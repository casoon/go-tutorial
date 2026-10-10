# Teil 13: Security

Nach diesem Kapitel weißt du, welche Sicherheitsentscheidungen in einem Go-Server explizit getroffen werden müssen, welche die Standardbibliothek dir abnimmt und warum ein URL-Monitor wie upwatch ein ganz eigenes Risiko mitbringt: Server-Side Request Forgery.

## Was Go dir abnimmt und was nicht

Go hat ein paar Klassen von Fehlern einfach nicht. Es gibt keine Pufferüberläufe im C-Sinn, weil Slices und Strings Grenzen prüfen und ein Zugriff außerhalb mit einem Panic endet statt mit fremdem Speicher. Es gibt keine Deserialisierung, die beliebige Klassen instanziiert, wie man sie aus Javas `ObjectInputStream` oder alten .NET-Formattern kennt: `encoding/json` füllt genau den Typ, den du übergibst. Und die Kryptografie liegt in der Standardbibliothek, gepflegt vom Go-Team, statt in einer Sammlung von Drittpaketen.

Was Go dir nicht abnimmt, sind Entscheidungen über Grenzen: Wie lange darf ein Client brauchen? Wie groß darf ein Request sein? Welche Adressen darf dein Server von sich aus kontaktieren? Diese Werte hängen von deiner Anwendung ab, deshalb sind die Defaults oft „unbegrenzt“. Das ist der Teil, um den es hier hauptsächlich geht.

## Timeouts auf der Serverseite

Der kürzeste HTTP-Server in Go ist auch ein unsicherer:

```go
// Do not do this in production.
http.ListenAndServe(":8080", handler)
```

`http.ListenAndServe` erzeugt intern einen `http.Server` mit Nullwerten, und null heißt bei den Timeouts: kein Timeout. Ein Client kann eine Verbindung öffnen und die Header byteweise im Minutentakt schicken. Jede solche Verbindung hält eine Goroutine und einen Dateideskriptor. Goroutines sind billig, Dateideskriptoren sind begrenzt, und irgendwann nimmt der Server keine Verbindungen mehr an. Das ist der Slowloris-Angriff, und er braucht keine Bandbreite, nur Geduld.

upwatch baut den Server deshalb explizit in [main.go](../cmd/upwatch/main.go):

```go
srv := &http.Server{
	Addr:    cfg.Addr,
	Handler: httpapi.New(store, logger),
	// Without timeouts a slow or malicious client can hold a
	// connection (and a goroutine) open forever.
	ReadHeaderTimeout: 5 * time.Second,
	ReadTimeout:       10 * time.Second,
	WriteTimeout:      30 * time.Second,
	IdleTimeout:       120 * time.Second,
	MaxHeaderBytes:    16 << 10,
	ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
}
```

Die vier Timeouts schützen verschiedene Phasen einer Verbindung:

- `ReadHeaderTimeout` begrenzt die Zeit bis zum Ende der Request-Header. Das ist der direkte Schutz gegen Slowloris.
- `ReadTimeout` begrenzt das Lesen des ganzen Requests inklusive Body. upwatch nimmt nur kleine JSON-Bodies an, zehn Sekunden sind großzügig.
- `WriteTimeout` läuft ab dem Ende der Request-Header bis zum Ende der Antwort. Ein Client, der die Antwort extrem langsam abholt, hält die Verbindung damit nicht beliebig lange.
- `IdleTimeout` gilt für Keep-alive-Verbindungen zwischen zwei Requests. Ohne ihn fällt Go auf `ReadTimeout` zurück.

`MaxHeaderBytes` begrenzt die Größe von Request-Zeile und Headern. Der Default (`http.DefaultMaxHeaderBytes`) ist 1 MiB; upwatch braucht keine großen Header und setzt 16 KiB.

Diese Timeouts gelten für alle Handler gleich. Für einzelne Endpunkte, die mehr Zeit brauchen (ein Upload, ein Server-Sent-Events-Stream), gibt es `http.NewResponseController(w).SetReadDeadline(...)` und `SetWriteDeadline(...)` im Handler. Das ist besser, als die globalen Werte für alle hochzudrehen.

In ASP.NET Core setzt Kestrel von sich aus Limits (`KestrelServerLimits`, etwa für Header-Timeout und Request-Größe), in Node.js hat `http.Server` seit einigen Versionen ebenfalls Defaults für `headersTimeout` und `requestTimeout`. Go ist hier zurückhaltender: Der `http.Server` als Struct macht die Werte sichtbar, aber du musst sie setzen. Ein Linter wie `gosec` meldet einen `http.Server` ohne `ReadHeaderTimeout`; `go vet` tut das nicht.

## Timeouts auf der Clientseite

Dasselbe Problem gibt es in die andere Richtung. `http.DefaultClient` und die Hilfsfunktionen `http.Get` und `http.Post` haben kein Gesamt-Timeout. Ein Server, der die Verbindung annimmt und dann nichts mehr sendet, blockiert den Aufrufer, bis das Betriebssystem irgendwann aufgibt. Für upwatch, das fremde URLs abruft, wäre das fatal: Ein einziges hängendes Ziel blockiert einen Worker dauerhaft.

[check.go](../internal/check/check.go) setzt deshalb Timeouts auf zwei Ebenen. Der Dialer bricht den Verbindungsaufbau nach fünf Sekunden ab, und jeder Check bekommt einen Context mit Deadline:

```go
func (c *Checker) Check(ctx context.Context, target monitor.Target) (monitor.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	// ...
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
```

Der Context-Timeout deckt alles ab: DNS, Verbindungsaufbau, TLS-Handshake, Warten auf die Antwort. `http.Client.Timeout` würde dasselbe leisten; der Context hat den Vorteil, dass er gleichzeitig auf den Shutdown reagiert (siehe [Teil 07](07-context.md)). Der Transport kommt aus `http.DefaultTransport.Clone()` und erbt dessen Timeouts für TLS-Handshake und `Expect: 100-continue`.

Danach liest `Check` höchstens 64 KiB vom Body:

```go
_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
```

Das ist ebenfalls eine Grenze gegen fremde Server: Ein Ziel, das einen endlosen Body liefert, kostet upwatch so höchstens 64 KiB Lesearbeit pro Check, nicht Bandbreite ohne Ende.

## Request-Größen begrenzen

Ohne Grenze liest `json.NewDecoder(r.Body).Decode(...)` so lange, wie der Client sendet. `ReadTimeout` hilft nur bedingt: In zehn Sekunden kann ein schneller Client sehr viele Bytes schicken, und der Decoder baut daraus Strings im Speicher.

Die Lösung ist `http.MaxBytesReader`. In [httpapi.go](../internal/httpapi/httpapi.go) kapselt `readJSON` den Body, bevor irgendetwas gelesen wird:

```go
const maxBodyBytes = 16 << 10

func readJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return fmt.Errorf("body must not exceed %d bytes", maxErr.Limit)
		}
		return fmt.Errorf("invalid JSON: %v", err)
	}
	// ...
}
```

Anders als `io.LimitReader`, der einfach still abschneidet, liefert `MaxBytesReader` beim Überschreiten einen Fehler vom Typ `*http.MaxBytesError` und signalisiert dem Server, die Verbindung nach der Antwort zu schließen. Der Rest des Bodys wird nicht mehr gelesen.

Das Beispiel [beispiele/13-security/maxbytes](../beispiele/13-security/maxbytes/) zeigt den Effekt mit einem 1-KiB-Limit. Es antwortet bei zu großem Body mit 413, upwatch selbst bleibt bei 400 mit Fehlertext:

```bash
go run ./beispiele/13-security/maxbytes
```

```
small body     -> 201 Created
1 MiB body     -> 413 Request Entity Too Large
unknown field  -> 400 Bad Request
```

Der zugehörige Test prüft dasselbe mit `httptest.NewRecorder`, ohne Netzwerk.

## Eingaben validieren

Validierung ist in Go keine Annotation, sondern Code. Es gibt kein `[Required]` wie in ASP.NET und kein `class-validator` wie in NestJS. Das klingt nach mehr Arbeit, hat aber einen Vorteil: Die Regeln stehen an einer Stelle, sind testbar und gelten unabhängig davon, ob die Eingabe aus HTTP, einer Datei oder einem Test kommt.

In upwatch liegt diese Stelle in der Domäne, in [monitor.go](../internal/monitor/monitor.go). `NormalizeURL` lässt nur absolute `http`- und `https`-URLs mit Host zu, lehnt Leerraum und Zugangsdaten in der URL ab, entfernt das Fragment und begrenzt die Länge auf `MaxURLLength` (2048 Bytes). Das schließt `file://`, `gopher://` und `javascript:` aus, bevor sie überhaupt gespeichert werden.

Bemerkenswert ist, wo die Längengrenze greift. Eine erste Prüfung am Rohwert ist nur ein billiger Abbruch vor dem Parsen. Die verbindliche Prüfung steht am Ende, am normalisierten Ergebnis von `u.String()`, denn das Normalisieren kann eine URL länger machen: Bytes, die nicht roh vorkommen dürfen, werden als `%XX` kodiert, aus einem Byte werden drei. Diesen Fall und den mit Leerraum hat `FuzzNormalizeURL` gefunden ([Teil 11](11-testing.md) erzählt die Geschichte). Die allgemeine Regel daraus: Validiere den Wert, den du speicherst und später benutzt, nicht nur den, den du bekommen hast. `NewTarget` erzwingt, dass das Intervall zwischen `MinInterval` (10 Sekunden) und `MaxInterval` (24 Stunden) liegt, sodass niemand upwatch dazu bringt, eine Seite im Sekundentakt abzufragen.

Die HTTP-Schicht ergänzt Prüfungen, die nur dort Sinn ergeben:

- `dec.DisallowUnknownFields()` lehnt JSON mit unbekannten Feldern ab. Ein `{"url":"…","admin":true}` wird nicht still ignoriert, sondern ist ein 400. Das schützt vor Tippfehlern und davor, dass ein später hinzugefügtes Feld plötzlich von alten Clients mit Unsinn befüllt wird.
- `dec.More()` nach dem Decode stellt sicher, dass nur ein JSON-Objekt im Body steht.
- `pathID` akzeptiert nur positive Ganzzahlen als `{id}`.
- `listResults` lässt `limit` nur zwischen 1 und 1000 zu. Ohne Obergrenze könnte ein einzelner Request die ganze Ergebnistabelle in den Speicher holen.

Die Faustregel: Jede Zahl, die ein Client wählt und die Arbeit auf dem Server bestimmt, braucht eine Obergrenze.

## SQL parametrisieren

SQL-Injection ist in Go so leicht zu vermeiden wie in jeder Sprache mit Platzhaltern, und so leicht einzubauen wie überall, wo jemand `fmt.Sprintf` für SQL benutzt. [store.go](../internal/sqlitestore/store.go) übergibt Werte ausschließlich als Parameter:

```go
row := s.db.QueryRowContext(ctx,
	`SELECT id, url, interval_seconds, created_at FROM targets WHERE id = ?`, id)
```

Der Treiber schickt SQL-Text und Werte getrennt an die Datenbank; ein Wert kann den SQL-Text nicht verändern. Das gilt auch für `LIMIT ?` in `Results`. Was sich nicht parametrisieren lässt, sind Bezeichner wie Tabellen- oder Spaltennamen. Wenn du die dynamisch brauchst (etwa für eine Sortierung), dann über eine feste Zuordnung (`map[string]string{"created": "created_at"}`), nie durch Einsetzen der Benutzereingabe.

## Secrets gehören nicht ins Binary

Ein Go-Binary ist eine einzelne Datei, die man leicht kopiert, in Images packt und in Artefakt-Speichern ablegt. Alles, was im Code steht, steht auch im Binary – `strings upwatch | grep` findet es. Konstanten mit API-Keys, Passwörter in `-ldflags "-X …"` oder eingebettete Konfigurationsdateien mit Zugangsdaten sind deshalb tabu.

upwatch liest seine Konfiguration aus Umgebungsvariablen ([config.go](../internal/config/config.go)). Selbst hat es keine Secrets, aber die Unit-Datei [upwatch.service](../deploy/upwatch.service) zeigt, wo sie hingehören würden:

```ini
# Secrets would go into a root-only file instead:
# EnvironmentFile=/etc/upwatch/env
```

`Environment=` steht in der Unit, die oft im Repository liegt und von `systemctl show` für jeden lesbar ausgegeben wird. Eine `EnvironmentFile` mit Rechten `0600`, die root gehört, hält den Wert aus beidem heraus. In Kubernetes übernimmt ein Secret diese Rolle, in Docker Compose `secrets:` oder eine `.env`-Datei außerhalb des Repositorys.

Die zweite Stelle, an der Secrets auslaufen, sind Logs. upwatch loggt beim Start Adresse und Datenbankpfad, nicht die komplette Konfiguration, und die Request-Middleware loggt `r.URL.Path`, nicht die volle URL mit Query-String, in dem Tokens stehen könnten. Wenn du einen Wert hast, der nie im Log landen darf, gib ihm einen eigenen Typ, der `slog.LogValuer` implementiert:

```go
// Token never shows up in logs, even if someone logs the whole config.
type Token string

func (Token) LogValue() slog.Value { return slog.StringValue("REDACTED") }
```

Für das Vergleichen solcher Werte nimmst du `crypto/subtle.ConstantTimeCompare` statt `==`, damit die Laufzeit des Vergleichs nichts über die Anzahl übereinstimmender Bytes verrät.

## TLS

`crypto/tls` ist eine vollständige TLS-Implementierung in Go, keine Bindung an OpenSSL. Die Defaults sind vernünftig: Mindestversion ist TLS 1.2, Höchstversion TLS 1.3, die Cipher-Suites sind auf sichere Kombinationen beschränkt und für TLS 1.3 nicht einmal konfigurierbar. Seit Go 1.24 ist der hybride Post-Quanten-Schlüsselaustausch `X25519MLKEM768` standardmäßig aktiv. Ein HTTPS-Server ist in Go eine Zeile mehr:

```go
err := srv.ListenAndServeTLS("/etc/upwatch/cert.pem", "/etc/upwatch/key.pem")
```

Trotzdem terminiert upwatch kein TLS selbst. Die Unit bindet an `127.0.0.1:8080`, davor steht ein Reverse Proxy wie Caddy oder nginx. Das ist in Go-Projekten der übliche Weg, nicht weil Go TLS schlecht kann, sondern weil Zertifikatserneuerung, HTTP/3, Zugriffslogs und mehrere Dienste hinter einer IP Aufgaben sind, die ein Proxy zentral erledigt. Teil 15 zeigt die Konfiguration.

Wenn du doch direkt aus Go heraus TLS mit Let’s-Encrypt-Zertifikaten anbieten willst, gibt es `golang.org/x/crypto/acme/autocert`. Das Paket holt und erneuert Zertifikate selbst; es liegt im erweiterten Ökosystem des Go-Teams, nicht in der Standardbibliothek.

Auf der Clientseite prüft Go Zertifikate gegen den System-Trust-Store. `InsecureSkipVerify: true` in einer `tls.Config` schaltet das ab. Es gibt fast keinen legitimen Grund dafür im Produktivcode, und wenn ein Check gegen ein internes Zertifikat nötig ist, gehört dessen CA in `RootCAs`.

## SSRF: Wenn Nutzer bestimmen, wohin dein Server sich verbindet

Jetzt zum Thema, das upwatch von einer gewöhnlichen CRUD-API unterscheidet. Der Kern der Anwendung ist: Ein Nutzer gibt eine URL ein, und der Server ruft sie ab. Damit kann jeder, der Ziele anlegen darf, Requests aus dem Netz des Servers heraus auslösen. Das heißt Server-Side Request Forgery (SSRF).

Was man damit anrichten kann, hängt davon ab, was neben upwatch erreichbar ist:

- `http://127.0.0.1:6060/debug/pprof/` – der eigene Debug-Listener.
- `http://localhost:5432` oder ein Admin-Interface auf einem anderen lokalen Port.
- `http://10.0.0.12/` – Dienste im internen Netz, die keine Authentifizierung haben, weil „da kommt ja keiner hin“.
- `http://169.254.169.254/` – der Metadaten-Endpunkt bei AWS, GCP, Azure und anderen Cloud-Anbietern. Je nach Konfiguration liefert er temporäre Zugangsdaten der Instanz.

upwatch gibt nur Statuscode und Dauer zurück, nicht den Body. Das schwächt den Angriff, verhindert ihn aber nicht: Schon „Port offen, Antwort 200“ gegen „Verbindung abgelehnt“ ist ein Portscanner fürs interne Netz, und manche internen Endpunkte lösen bei einem einfachen GET eine Aktion aus.

### Warum die URL zu prüfen nicht reicht

Der naheliegende Ansatz ist, beim Anlegen des Ziels den Host aus der URL zu nehmen und zu schauen, ob er eine private IP ist. Das scheitert sofort an Hostnamen. `http://localhost/` enthält keine IP, `http://intern.example.com/` auch nicht, und beide können auf `127.0.0.1` oder `10.0.0.12` zeigen.

Den Hostnamen beim Anlegen aufzulösen hilft ebenfalls nicht. Erstens kann sich der DNS-Eintrag später ändern, und upwatch ruft das Ziel noch tagelang ab. Zweitens gibt es DNS-Rebinding: Ein Angreifer kontrolliert einen DNS-Server, der mit sehr kurzer TTL beim ersten Abruf eine öffentliche IP liefert (Prüfung bestanden) und beim zweiten `127.0.0.1` (Verbindung geht nach innen). Jede Prüfung, die vor dem eigentlichen Verbindungsaufbau stattfindet, hat dieses Zeitfenster.

### Prüfen, wohin tatsächlich verbunden wird

`check.NewClient` prüft deshalb an der einzigen Stelle, an der die echte IP feststeht: unmittelbar vor dem `connect`. `net.Dialer` hat dafür das Feld `Control`, eine Funktion, die nach der DNS-Auflösung und nach dem Anlegen des Sockets, aber vor dem Verbindungsaufbau aufgerufen wird. Ihr zweites Argument ist die Zieladresse als `ip:port`:

```go
func NewClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if !allowPrivate {
		// Control runs after DNS resolution, right before connect, so it
		// sees the real IP. Checking the hostname in the URL would not
		// be enough: evil.example could resolve to 127.0.0.1.
		dialer.Control = func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return err
			}
			if !isPublic(ap.Addr()) {
				return fmt.Errorf("%s: %w", ap.Addr(), ErrForbiddenAddress)
			}
			return nil
		}
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	transport.Proxy = nil // a proxy would bypass the address check
	transport.MaxIdleConnsPerHost = 2

	return &http.Client{
		Transport: transport,
		// Do not follow redirects: a 301 is an answer, not an error.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
```

Weil die Prüfung für jede neue Verbindung läuft, ist es egal, was DNS beim Anlegen geantwortet hat oder ob der Eintrag zwischendurch wechselt. Löst ein Name auf mehrere Adressen auf und probiert Go sie nacheinander (IPv6 und IPv4, „Happy Eyeballs“), wird jede einzeln geprüft. Verbindungen, die der Transport per Keep-alive wiederverwendet, sind über denselben Dialer entstanden und wurden beim Aufbau geprüft.

Zwei weitere Zeilen gehören zum Schutz, auch wenn man es ihnen nicht ansieht:

- `transport.Proxy = nil`: `DefaultTransport` nutzt `http.ProxyFromEnvironment`, liest also `HTTP_PROXY` und `HTTPS_PROXY`. Mit Proxy verbindet sich upwatch nur noch mit dem Proxy, und `Control` sieht dessen Adresse, nicht die des Ziels. Die Prüfung liefe ins Leere.
- `CheckRedirect` mit `http.ErrUseLastResponse`: Für einen Monitor ist ein 301 ein gültiges Ergebnis, kein Umweg. Nebenbei schließt das eine zweite Klasse von Tricks aus, bei der eine öffentliche URL auf `http://169.254.169.254/` umleitet. Der Dialer würde auch das abfangen, aber jede Schicht, die gar nicht erst nach innen zeigt, ist eine weniger, auf die man sich verlassen muss.

### Was „öffentlich“ heißt

```go
func isPublic(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() &&
		!netip.MustParsePrefix("100.64.0.0/10").Contains(ip) // carrier-grade NAT
}
```

`net/netip` (seit Go 1.18) hat für die meisten Kategorien fertige Methoden. `IsGlobalUnicast` schließt unter anderem `0.0.0.0`, Multicast und den Broadcast `255.255.255.255` aus, hält aber private Adressen für global – deshalb die weiteren Bedingungen. `IsPrivate` deckt `10/8`, `172.16/12`, `192.168/16` und IPv6 `fc00::/7` ab. `IsLinkLocalUnicast` erfasst `169.254/16` – und damit den Cloud-Metadaten-Endpunkt `169.254.169.254` – sowie `fe80::/10`. `100.64.0.0/10` ist der Bereich für Carrier-grade NAT, den manche Cloud-Umgebungen intern nutzen; dafür gibt es keine eigene Methode.

`Unmap()` am Anfang ist wichtig: `::ffff:127.0.0.1` ist eine IPv4-Adresse in IPv6-Schreibweise. Ohne `Unmap` wäre sie für `IsLoopback` keine Loopback-Adresse. [check_test.go](../internal/check/check_test.go) prüft genau diese Fälle in `TestIsPublic`, und `TestForbiddenAddresses` zeigt, dass ein Request an einen `httptest.Server` auf `127.0.0.1` den Server nie erreicht.

Das Beispiel [beispiele/13-security/ssrfguard](../beispiele/13-security/ssrfguard/) stellt die naive URL-Prüfung und den Dialer-Ansatz nebeneinander:

```bash
go run ./beispiele/13-security/ssrfguard
```

```
http://127.0.0.1:57396
  naive check allows: false
  guarded client:     Get "http://127.0.0.1:57396": dial tcp 127.0.0.1:57396: 127.0.0.1: forbidden address
http://localhost:57396
  naive check allows: true
  guarded client:     Get "http://localhost:57396": dial tcp [::1]:57396: ::1: forbidden address
```

Die Portnummer wechselt bei jedem Lauf. Interessant ist die zweite Zeile: Die naive Prüfung lässt `localhost` durch, der Dialer sieht, dass der Name auf `::1` auflöst, und blockiert.

### UPWATCH_ALLOW_PRIVATE_TARGETS

Für lokale Tests will man genau das Gegenteil: ein Ziel auf `localhost` überwachen. Dafür gibt es `UPWATCH_ALLOW_PRIVATE_TARGETS=true`. Dann setzt `NewClient` kein `Control`, und `main` loggt beim Start eine Warnung:

```go
if cfg.AllowPrivate {
	logger.Warn("checks may reach private and loopback addresses")
}
```

Der Schalter ist bewusst ein expliziter Opt-in mit einem Namen, den niemand versehentlich setzt, und der Default ist sicher. In Produktion hat er nichts verloren. Die Tests in `check_test.go` nutzen `NewClient(true)`, weil `httptest.Server` immer auf Loopback lauscht.

Der Schutz hat Grenzen, die du kennen solltest. Er verhindert nicht, dass upwatch öffentliche Ziele mit hoher Frequenz abfragt – die Intervallgrenze und eine Begrenzung der Zielanzahl pro Nutzer wären dafür zuständig. Und es gibt exotischere Adressbereiche (etwa NAT64-Präfixe, die auf IPv4-Adressen abbilden), die `isPublic` nicht kennt. Netzwerkseitige Regeln (eine Firewall, die ausgehenden Verkehr des Dienstes ins interne Netz sperrt) sind die zweite Verteidigungslinie, die ein Code-Check nicht ersetzt.

## Den HTTP-Server absichern

### Security-Header

[middleware.go](../internal/httpapi/middleware.go) setzt zwei Header auf jede Antwort:

```go
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
```

`nosniff` verhindert, dass ein Browser eine JSON-Antwort als HTML oder Script interpretiert. `no-store` hält API-Antworten aus geteilten Caches. Für eine reine JSON-API reicht das. Sobald upwatch HTML ausliefert, kämen eine `Content-Security-Policy` und `frame-ancestors` dazu. `Strict-Transport-Security` setzt sinnvollerweise der Reverse Proxy, der TLS terminiert.

Middleware ist in Go eine Funktion `func(http.Handler) http.Handler` – kein Framework, keine Registrierung. Wer aus ASP.NET kommt, erkennt das Prinzip von `app.Use(...)`; wer aus Express kommt, `app.use(...)`, nur ohne `next()`-Callback, weil der nächste Handler einfach aufgerufen wird.

### Keine internen Fehler nach außen

Ein Datenbankfehler enthält oft SQL, Dateipfade oder Hostnamen. `serverError` in [httpapi.go](../internal/httpapi/httpapi.go) loggt die Details und gibt dem Client nur einen generischen Text:

```go
func (a *API) serverError(w http.ResponseWriter, r *http.Request, err error) {
	a.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}
```

`TestInternalErrorsAreNotLeaked` in [httpapi_test.go](../internal/httpapi/httpapi_test.go) prüft das mit einem Store, dessen Fehlermeldung das Wort „secret“ enthält. Validierungsfehler (`*monitor.ValidationError`) dürfen dagegen nach außen, sie beschreiben ja die Eingabe des Clients. Diese Trennung ist der Grund, warum `handleError` mit `errors.As` und `errors.Is` arbeitet, statt jeden Fehlertext durchzureichen.

`recoverPanics` gehört in dieselbe Kategorie: Ein Panic in einem Handler wird zu einer 500 mit generischem Text und einem Log-Eintrag, statt einem abgebrochenen Response.

### Debug-Endpunkte nur auf localhost

pprof-Profile verraten Funktionsnamen und interne Struktur, `/debug/pprof/cmdline` zeigt die Kommandozeile, und ein CPU-Profil-Request bindet den Prozess für die angefragte Dauer. Deshalb hängen diese Endpunkte nicht am API-Router, sondern an einem zweiten Listener, der nur startet, wenn `UPWATCH_DEBUG_ADDR` gesetzt ist:

```go
if cfg.DebugAddr != "" {
	go serveDebug(cfg.DebugAddr, logger)
}
```

Gesetzt wird er auf `127.0.0.1:6060`, nie auf `:6060`. Ein häufiger Fehler ist ein blankes `import _ "net/http/pprof"`: Dieses Package registriert seine Handler beim Import im `http.DefaultServeMux`. Wer dann irgendwo `http.ListenAndServe(addr, nil)` nutzt, veröffentlicht pprof auf dem öffentlichen Port. upwatch registriert die pprof-Handler deshalb explizit an einem eigenen Mux. Wie du den Debug-Listener benutzt, zeigt Teil 14.

### Ohne Rechte laufen

Das Container-Image nutzt `distroless/static` mit dem Benutzer `nonroot`, die systemd-Unit `DynamicUser=yes` und eine Reihe von Hardening-Optionen (`NoNewPrivileges`, `ProtectSystem=strict`, `RestrictAddressFamilies`, …). Beides ist Thema von Teil 15. Für dieses Kapitel zählt der Gedanke dahinter: Wenn trotz allem jemand Code im Prozess ausführen kann, soll er dort möglichst wenig vorfinden – keine Shell, keine Schreibrechte außerhalb des Datenverzeichnisses, keine Root-Rechte.

### Was upwatch nicht hat

upwatch hat keine Authentifizierung. Wer die API erreicht, kann Ziele anlegen und löschen. Das ist für ein Tutorial vertretbar, weil die Unit nur an `127.0.0.1` bindet, aber vor einem Einsatz im Netz musst du das ändern: entweder per Authentifizierung im Reverse Proxy oder per Middleware, die einen Bearer-Token aus der Umgebung mit `subtle.ConstantTimeCompare` vergleicht. Eine der Übungen unten baut das.

## Sicherheitsfunktionen der Standardbibliothek

Einiges, was in anderen Ökosystemen ein Drittpaket braucht, ist in Go eingebaut:

- `html/template` escapt kontextabhängig: in HTML-Text anders als in Attributen, in `<script>` anders als in URLs. Wer `text/template` für HTML benutzt, verliert das. Der Unterschied ist ein Import – der häufigste Fehler in diesem Bereich.
- `crypto/rand` liefert kryptografisch sichere Zufallszahlen. `crypto/rand.Text()` (seit Go 1.24) erzeugt einen zufälligen Token als String, etwa für API-Keys. `math/rand/v2` ist dafür nicht gedacht.
- `crypto/subtle.ConstantTimeCompare` für Vergleiche von Tokens und Signaturen, `crypto/hmac` für signierte Werte, `crypto/sha256` und Co. für Hashes. Für Passwort-Hashing gibt es `golang.org/x/crypto/bcrypt` und `argon2` im erweiterten Ökosystem.
- `net/netip` stellt IP-Adressen als kleine, vergleichbare Werttypen dar, mit den Klassifizierungsmethoden, die `isPublic` nutzt.
- `os.Root` (seit Go 1.24) beschränkt Dateizugriffe auf einen Verzeichnisbaum. `root.Open("../../etc/passwd")` scheitert, auch über Symlinks. Das ersetzt die fehleranfällige Prüfung mit `filepath.Clean` und `strings.HasPrefix`, wenn ein Pfad aus einer Benutzereingabe stammt.
- `http.CrossOriginProtection` (seit Go 1.25) ist eine CSRF-Middleware. Sie lehnt nicht-sichere Cross-Origin-Requests von Browsern anhand der Header `Sec-Fetch-Site` und `Origin` ab, ohne Tokens in Formularen. Für upwatch als JSON-API ohne Cookies ist sie nicht nötig; sobald eine Web-Oberfläche mit Session-Cookie dazukommt, schon.

```go
handler := http.NewCrossOriginProtection().Handler(mux)
```

## Abhängigkeiten prüfen: govulncheck

Die Go-Sicherheitsdatenbank unter vuln.go.dev sammelt bekannte Schwachstellen in Go-Modulen und in der Standardbibliothek. `govulncheck` gleicht dein Projekt damit ab, und zwar genauer als die meisten Scanner aus anderen Ökosystemen: Es analysiert den Aufrufgraph und meldet in erster Linie Schwachstellen in Funktionen, die dein Code tatsächlich erreichen kann. Ein `npm audit` oder `dotnet list package --vulnerable` meldet ein verwundbares Paket, sobald es im Abhängigkeitsbaum steckt; govulncheck unterscheidet, ob die betroffene Funktion überhaupt aufgerufen wird.

Ausführen lässt es sich ohne Installation:

```bash
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Am 10. Oktober 2026 mit govulncheck v1.8.0 und dem lokal installierten Go 1.27.1 lieferte das für upwatch (gekürzt):

```
=== Symbol Results ===

Vulnerability #1: GO-2026-6617
    HTTP/2 server crash due to HPACK encoder race in net/http
  More info: https://pkg.go.dev/vuln/GO-2026-6617
  Standard library
    Found in: net/http/internal/http2@go1.27.1
    Fixed in: net/http/internal/http2@go1.27.2
    Example traces found:
      #1: internal/check/check.go:101:26: check.Checker.Check calls http.Client.Do, which eventually calls http2.NetHTTPClientConn.Close
      ...
      #7: cmd/upwatch/main.go:89:31: upwatch.run calls http.Server.ListenAndServe, which eventually calls http2.Server.ServeConn
      ...

Your code is affected by 9 vulnerabilities from the Go standard library.
This scan also found 2 vulnerabilities in packages you import and 2
vulnerabilities in modules you require, but your code doesn't appear to call
these vulnerabilities.
```

Die Ausgabe hat drei Stufen. „Symbol Results“ sind Schwachstellen, deren betroffene Funktion von deinem Code aus erreichbar ist, mit einer Aufrufkette als Beleg – hier vom `Checker.Check` über `http.Client.Do` in die HTTP/2-Implementierung. Darunter stehen Schwachstellen in importierten Packages und in benötigten Modulen, deren betroffene Funktionen nicht erreicht werden. Die zeigt govulncheck nur mit `-show verbose`; in diesem Lauf waren das unter anderem zwei Fehler in `html/template`, das upwatch nicht benutzt. Der Exit-Code ist 3, sobald es Symbol-Treffer gibt, sodass ein CI-Schritt fehlschlägt.

Alle neun Treffer lagen in der Standardbibliothek, nicht in `modernc.org/sqlite` oder dessen Abhängigkeiten, und alle waren in Go 1.27.2 behoben. Die Abhilfe ist also kein Code-Change, sondern ein Toolchain-Update. Zum Gegencheck mit der neueren Toolchain:

```bash
GOTOOLCHAIN=go1.27.2 go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

```
No vulnerabilities found.
```

Das zeigt zwei Dinge. Erstens: Bei Go gehört die Standardbibliothek zu deinen Abhängigkeiten, und weil sie ins Binary einkompiliert wird, hilft nur neu bauen mit einer aktuellen Toolchain – ein Update des Servers genügt nicht. Zweitens: Für die Ergebnisse ist entscheidend, mit welcher Go-Version govulncheck läuft. In CI sollte das dieselbe Version sein, mit der du das Release baust.

In upwatch läuft govulncheck als Schritt in [.github/workflows/ci.yml](../.github/workflows/ci.yml). Für Modulabhängigkeiten aktualisierst du mit `go get modul@version` und `go mod tidy`; `go list -m -u all` zeigt, wofür es neuere Versionen gibt.

## Im Projekt

- [cmd/upwatch/main.go](../cmd/upwatch/main.go): `http.Server` mit allen Timeouts und `MaxHeaderBytes`, Debug-Listener nur bei gesetztem `UPWATCH_DEBUG_ADDR`, Warnung bei `AllowPrivate`.
- [internal/check/check.go](../internal/check/check.go): `NewClient` mit `Dialer.Control`, `Proxy = nil`, keine Redirects; `isPublic`; Context-Timeout pro Check; begrenztes Lesen des Bodys.
- [internal/check/check_test.go](../internal/check/check_test.go): `TestForbiddenAddresses`, `TestIsPublic`.
- [internal/monitor/monitor.go](../internal/monitor/monitor.go): `NormalizeURL`, Intervallgrenzen.
- [internal/httpapi/httpapi.go](../internal/httpapi/httpapi.go): `readJSON` mit `MaxBytesReader` und `DisallowUnknownFields`, `limit`-Grenzen, `serverError`.
- [internal/httpapi/middleware.go](../internal/httpapi/middleware.go): `securityHeaders`, `recoverPanics`.
- [internal/sqlitestore/store.go](../internal/sqlitestore/store.go): ausschließlich parametrisiertes SQL.
- [deploy/upwatch.service](../deploy/upwatch.service), [Dockerfile](../Dockerfile): Ausführung ohne Root-Rechte.

## Übungen

1. Schreib eine Middleware `requireToken(token string, next http.Handler) http.Handler`, die `Authorization: Bearer <token>` mit `subtle.ConstantTimeCompare` prüft und sonst 401 antwortet. Lies den Token in `config.Load` aus `UPWATCH_API_TOKEN` und gib ihm einen Typ mit `LogValue`, der ihn im Log verbirgt. `/healthz` und `/readyz` sollen ohne Token erreichbar bleiben.
2. Ergänze `TestIsPublic` um weitere Adressen: `224.0.0.1` (Multicast), `198.18.0.1` (Benchmark-Netz), `240.0.0.1` (reserviert) und `64:ff9b::7f00:1` (NAT64-Abbildung von `127.0.0.1`). Welche hält `isPublic` für öffentlich? Erweitere die Funktion um eine Liste zusätzlich verbotener Präfixe.
3. Begrenze die Anzahl der Ziele: `POST /targets` soll mit 409 oder 422 antworten, wenn bereits 500 Ziele existieren. Wo gehört die Grenze hin – in die Domäne, die API oder den Store? Begründe deine Wahl.
4. Führe `govulncheck` in deinem Projekt aus und dann mit `-show verbose`. Ordne jede Meldung einer der drei Stufen zu und überlege, welche davon ein Release blockieren sollte.

---

← [Teil 12: Logging, Konfiguration, Observability](12-logging-konfiguration-observability.md) · [Übersicht](../README.md) · [Teil 14: Performance und Speicher](14-performance.md) →
