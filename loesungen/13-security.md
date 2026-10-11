# Lösungen zu Teil 13: Security

Zu den Aufgaben: [Teil 13](../kapitel/13-security.md#übungen)

## Übung 1: Token-Middleware (Kern)

Die Lösung hat drei Teile: einen Typ für den Token in `config`, die Middleware in `httpapi` und eine Aufteilung der Routen, damit die Probes ohne Token durchkommen.

```go
// internal/config/config.go
type Secret string

const redacted = "[redacted]"

func (Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

func (Secret) String() string { return redacted }

func (Secret) MarshalText() ([]byte, error) { return []byte(redacted), nil }

// in Load:
if v := getenv("UPWATCH_API_TOKEN"); v != "" {
	// Unlike the other messages, this one must not echo the value.
	if len(v) < minTokenLength {
		errs = append(errs, fmt.Errorf("UPWATCH_API_TOKEN: want at least %d characters, got %d", minTokenLength, len(v)))
	} else {
		cfg.APIToken = Secret(v)
	}
}
```

```go
// internal/httpapi/middleware.go
func requireToken(token string, next http.Handler) http.Handler {
	want := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, got, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
			unauthorized(w)
			return
		}
		have := sha256.Sum256([]byte(got))
		if subtle.ConstantTimeCompare(have[:], want[:]) != 1 {
			unauthorized(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

```go
// internal/httpapi/httpapi.go, in New(store, logger, token)
api := http.NewServeMux()
api.HandleFunc("GET /targets", a.listTargets)
// ... the other /targets routes

var protected http.Handler = api
if token != "" {
	protected = requireToken(token, api)
}

mux := http.NewServeMux()
mux.HandleFunc("GET /healthz", a.healthz)
mux.HandleFunc("GET /readyz", a.readyz)
mux.Handle("/", protected)
```

`main` übergibt `string(cfg.APIToken)` und loggt eine Warnung, wenn kein Token gesetzt ist – nach demselben Muster wie bei `UPWATCH_ALLOW_PRIVATE_TARGETS`. `TestRequireToken` prüft die Angriffsfälle gegen den echten Handler: kein Header, falscher Token, ein Präfix des Tokens, der Token mit angehängtem Zeichen, `Bearer ` ohne Wert, `Basic` statt `Bearer`, der Token ohne Schema, Schreibzugriffe und unbekannte Pfade ohne Token. Alle liefern 401 mit `WWW-Authenticate`, nur der richtige Token und die beiden Probes liefern 200. Ein zweiter Test zeigt, dass `requireToken("")` jeden Request ablehnt, auch `Authorization: Bearer `.

Ein Lauf mit gesetztem Token:

```
$ curl -s -i localhost:18350/targets
HTTP/1.1 401 Unauthorized
...
{"error":"missing or invalid token"}
$ curl -s -H 'Authorization: Bearer s3cr3t-token-0123456789' localhost:18350/targets
[]
$ curl -s localhost:18350/healthz
{"status":"ok"}
```

```bash
git apply loesungen/13-security/uebung-1.patch
go test ./...
```

**Warum so:** `ConstantTimeCompare` ist nur bei gleich langen Eingaben konstant – bei unterschiedlicher Länge kehrt es sofort mit 0 zurück und verrät so die Länge des Tokens. Beide Seiten vorher mit SHA-256 zu hashen gibt immer 32 Byte und schließt das aus. Die Prüfung `token == ""` in der Middleware ist kein Zierrat: Ohne sie wäre `sha256("") == sha256("")`, und `Authorization: Bearer ` käme durch, falls jemand die Middleware mit leerem Token einhängt. Beim Schreiben der Lösung hat der Test außerdem eine echte Lücke gefunden: `LogValue` allein reicht nicht, weil der JSON-Handler von slog ein verschachteltes Struct wie `Config` mit `encoding/json` kodiert und dabei `LogValue` der Felder ignoriert – der Token stand im Klartext im Log, bis `MarshalText` dazukam. Ob ein leerer Token „Authentifizierung aus“ bedeuten soll oder upwatch den Start verweigern sollte, ist eine Abwägung: Die Lösung bleibt kompatibel zum bisherigen Verhalten und warnt; strenger wäre, ohne Token nur auf `127.0.0.1` zu lauschen.

## Übung 2: Weitere Adressbereiche in isPublic (Kern)

Mit den vier neuen Einträgen in `TestIsPublic` und dem unveränderten `isPublic` schlägt der Test so fehl:

```
--- FAIL: TestIsPublic (0.00s)
    check_test.go:133: isPublic(198.18.0.1) = true, want false
    check_test.go:133: isPublic(240.0.0.1) = true, want false
    check_test.go:133: isPublic(64:ff9b::7f00:1) = true, want false
```

`224.0.0.1` fängt schon `IsGlobalUnicast` ab, weil Multicast kein Unicast ist. Die anderen drei hält `isPublic` für öffentlich: `198.18.0.0/15` und `240.0.0.0/4` sind für `net/netip` gewöhnliche Unicast-Adressen, und `64:ff9b::7f00:1` ist für Go eine IPv6-Adresse wie jede andere – dass ein NAT64-Gateway sie auf `127.0.0.1` abbildet, weiß nur das Netz.

Die Lösung ergänzt eine Liste von Präfixen und behandelt NAT64 gesondert:

```go
var forbiddenPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation (TEST-NET-1)
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // documentation (TEST-NET-2)
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation (TEST-NET-3)
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, includes broadcast
	netip.MustParsePrefix("::/96"),           // deprecated IPv4-compatible
	netip.MustParsePrefix("64:ff9b:1::/48"),  // local-use NAT64
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("2002::/16"),       // 6to4, embeds any IPv4 address
}

var nat64 = netip.MustParsePrefix("64:ff9b::/96")

func isPublic(ip netip.Addr) bool {
	ip = ip.Unmap()
	if nat64.Contains(ip) {
		b := ip.As16()
		return isPublic(netip.AddrFrom4([4]byte(b[12:])))
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range forbiddenPrefixes {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}
```

Der Test enthält neben den vier Adressen aus der Aufgabe weitere Angriffsfälle: den Metadaten-Endpunkt und `10.0.0.1` hinter NAT64 (`64:ff9b::a9fe:a9fe`, `64:ff9b::a00:1`), `::7f00:1` und `2002:7f00:1::` als andere Verpackungen von `127.0.0.1` – und als Gegenprobe `64:ff9b::5db8:d70e`, die NAT64-Form einer öffentlichen Adresse, die erlaubt bleiben muss.

```bash
git apply loesungen/13-security/uebung-2.patch
go test ./...
```

**Warum so:** Die Liste ist bewusst eine Liste und kein langer `&&`-Ausdruck mehr: Neue Bereiche kommen als eine Zeile mit Kommentar dazu, und der Kommentar sagt, warum. Beim well-known NAT64-Präfix wäre es einfacher, ihn komplett zu sperren. Auf einem IPv6-only-Host mit DNS64 löst aber jede reine IPv4-Website in genau diesen Bereich auf, und upwatch könnte dort nichts mehr prüfen; deshalb wird die eingebettete IPv4-Adresse ausgepackt und mit denselben Regeln bewertet. Für `64:ff9b:1::/48` (lokal vergebene NAT64-Präfixe, RFC 8215) gibt es keine feste Position der IPv4-Adresse, der Bereich wird ganz gesperrt. Vollständig wird eine solche Liste nie – die Firewall-Regel für ausgehenden Verkehr bleibt die zweite Verteidigungslinie, wie im Kapitel beschrieben.

## Übung 3: Höchstens 500 Ziele (Profi)

Es gibt hier keine einzig richtige Antwort, aber eine Platzierung, die einen echten Fehler vermeidet. Die naheliegende Lösung in der API – erst zählen, dann anlegen – hat eine Race Condition: Zwei gleichzeitige Requests sehen beide 499 Ziele und legen beide an. Die Lösung verteilt die Grenze deshalb nach Zuständigkeit:

- Die **Domäne** besitzt die Regel: `monitor.MaxTargets = 500` und den Sentinel-Fehler `monitor.ErrLimitReached`, neben `ErrNotFound` und `ErrDuplicate`.
- Der **Store** setzt sie atomar durch, weil nur er die Anzahl ohne Wettlauf kennt.
- Die **API** übersetzt den Fehler in `handleError` in einen Statuscode, wie alle anderen Domänenfehler.

```go
// internal/sqlitestore/store.go, CreateTarget
// Count and insert in one statement: SQLite runs it atomically, so
// concurrent requests cannot both see 499 and end up at 501.
res, err := s.db.ExecContext(ctx,
	`INSERT INTO targets (url, interval_seconds, created_at, next_check_at)
	 SELECT ?, ?, ?, ? WHERE (SELECT COUNT(*) FROM targets) < ?`,
	t.URL, int64(t.Interval/time.Second), now.UnixMilli(), now.UnixMilli(), s.maxTargets)
// ... unique violation as before
n, err := res.RowsAffected()
// ...
if n == 0 {
	return monitor.Target{}, fmt.Errorf("%d targets: %w", s.maxTargets, monitor.ErrLimitReached)
}
```

```go
// internal/httpapi/httpapi.go, handleError
case errors.Is(err, monitor.ErrLimitReached):
	writeError(w, http.StatusConflict, fmt.Sprintf("at most %d targets allowed", monitor.MaxTargets))
```

`Store` bekommt ein unexportiertes Feld `maxTargets`, das `Open` auf `monitor.MaxTargets` setzt und Tests im selben Package herabsetzen können, damit kein Test 500 Ziele anlegen muss. `TestTargetLimitConcurrent` ist der Test für den Angriffsfall: 30 Goroutines legen gleichzeitig Ziele an, die Grenze steht auf 5, genau 5 dürfen gelingen. Gegenprobe mit der naiven Variante aus `SELECT COUNT(*)` und anschließendem `INSERT` in zwei Statements (drei Läufe):

```
--- FAIL: TestTargetLimitConcurrent (0.01s)
    store_test.go:212: created 18, stored 18, want 5 and 5
--- FAIL: TestTargetLimitConcurrent (0.01s)
    store_test.go:212: created 21, stored 21, want 5 and 5
--- FAIL: TestTargetLimitConcurrent (0.01s)
    store_test.go:212: created 14, stored 14, want 5 and 5
```

Mit dem einen `INSERT … SELECT` besteht der Test, auch mit `-race` und `-count=3`. `TestTargetLimitIsConflict` in `httpapi` prüft die Abbildung auf 409 mit einem eingebetteten Fake-Store.

```bash
git apply loesungen/13-security/uebung-3.patch
go test ./...
```

**Warum so:** Eine Grenze, die nur in der API steht, lässt sich mit parallelen Requests überlisten; eine Grenze nur im SQL-Schema (etwa per Trigger) versteckt eine Geschäftsregel in der Datenbank, wo sie niemand sucht. Die Domäne definiert Wert und Fehler, der Store garantiert sie, die API übersetzt – so bleibt `handleError` die einzige Stelle, an der Fehler zu Statuscodes werden. 409 statt 422, weil der Request selbst gültig ist und nur am aktuellen Zustand scheitert: Nach dem Löschen eines Ziels gelingt derselbe Request. Mit Benutzerkonten würde aus der globalen Grenze eine pro Nutzer, und dann gehört der Wert eher in die Konfiguration als in eine Konstante.

## Übung 4: govulncheck auswerten (Aufwärmen)

Ausgeführt am 11. Oktober 2026 mit govulncheck v1.9.0, Datenbankstand 8. Oktober 2026 und Go 1.27.1 im Repository (gekürzt):

```
$ go run golang.org/x/vuln/cmd/govulncheck@latest -show verbose ./...
...
=== Symbol Results ===

Vulnerability #1: GO-2026-6617
    HTTP/2 server crash due to HPACK encoder race in net/http
    Found in: net/http/internal/http2@go1.27.1
    Fixed in: net/http/internal/http2@go1.27.2
...
Vulnerability #9: GO-2026-6603
    HTTP/2 server memory exhaustion due to Trailer headers in net/http

=== Package Results ===

Vulnerability #1: GO-2026-6609
    Lack of limit on size of parsed Range headers in net/http
Vulnerability #2: GO-2026-6604
    Root.Mkdir(All) can follow junctions out of the root on Windows in os

=== Module Results ===

Vulnerability #1: GO-2026-6600
    Recognize yield as regexp preceder keyword in html/template
Vulnerability #2: GO-2026-6599
    Reset context tracking on consecutive template expressions in html/template

Your code is affected by 9 vulnerabilities from the Go standard library.
This scan also found 2 vulnerabilities in packages you import and 2
vulnerabilities in modules you require, but your code doesn't appear to call
these vulnerabilities.
exit status 3
```

Die Zuordnung zu den drei Stufen steht in der Ausgabe selbst:

| Stufe | Meldungen | Bedeutung |
|---|---|---|
| Symbol Results | GO-2026-6617, -6613, -6612, -6611, -6610, -6608, -6607, -6605, -6603 (HTTP/1 und HTTP/2 in `net/http`, `crypto/tls`, `net/textproto`) | betroffene Funktion ist erreichbar, mit Aufrufkette, etwa `check.Checker.Check calls http.Client.Do` |
| Package Results | GO-2026-6609 (`Range`-Header), GO-2026-6604 (`os.Root`, nur Windows) | Package wird importiert, die betroffene Funktion aber nicht aufgerufen |
| Module Results | GO-2026-6600, -6599 (`html/template`) | nur im Modul bzw. der Standardbibliothek vorhanden, nicht einmal importiert |

Ohne `-show verbose` erscheinen nur die Symbol-Treffer und die Zusammenfassung. Der Exit-Code von govulncheck ist 3; `go run` meldet ihn als `exit status 3` und endet selbst mit 1 – in CI schlägt der Schritt in beiden Fällen fehl. Zwei Details fallen auf: `./...` umfasst auch `beispiele/`, deshalb tauchen Aufrufketten aus den Beispielen auf (`inmemory.main calls http.Server.ListenAndServe …`). Für die Release-Frage zählt `./cmd/upwatch`; auch dieser Lauf meldet alle neun Symbol-Treffer, mit Aufrufketten etwa ab `check.Checker.Check calls http.Client.Do` und `upwatch.run calls http.Server.ListenAndServe`. Und `GOTOOLCHAIN=go1.27.2 go run golang.org/x/vuln/cmd/govulncheck@latest ./...` meldet `No vulnerabilities found.`

**Warum so:** Ein Release blockieren sollten die Symbol-Treffer – hier alle neun, weil sie im HTTP-Server und im Check-Client von upwatch erreichbar sind und mehrere davon Abstürze oder Speichererschöpfung durch fremde Clients beschreiben. Die Abhilfe kostet nur ein Toolchain-Update und einen neuen Build. Package- und Module-Treffer blockieren nicht, sollten aber beim nächsten Update mitgehen: Sobald jemand `html/template` für eine Weboberfläche einbaut (siehe Teil 16), werden aus Module-Treffern Symbol-Treffer. Eine Zuordnung, die nur auf den Paketnamen schaut wie `npm audit`, hätte alle dreizehn gleich behandelt.
