# Teil 08: Eine HTTP-API mit net/http

Nach diesem Kapitel kannst du eine JSON-API allein mit der Standardbibliothek bauen: Routing mit Methoden und Pfadparametern, sauberes Lesen und Schreiben von JSON, Fehler-Mapping und Middleware. Du kennst die HTTP-API von upwatch Zeile für Zeile und weißt, ab wann sich ein Router oder Framework lohnt.

> **upwatch in diesem Teil:** die HTTP-Schicht in [httpapi.go](../internal/httpapi/httpapi.go) und [middleware.go](../internal/httpapi/middleware.go), dazu der `http.Server` in [main.go](../cmd/upwatch/main.go)
> **Voraussetzungen:** Teil 04 bis 07

## Die Endpunkte

upwatch bietet eine kleine REST-artige API:

| Methode | Pfad | Antwort |
|---|---|---|
| `GET` | `/targets` | alle Ziele, `200` (auch als leere Liste) |
| `POST` | `/targets` | neues Ziel, `201` mit `Location`-Header; `400` bei ungültiger Eingabe, `409` bei Duplikat |
| `GET` | `/targets/{id}` | ein Ziel, `200` oder `404` |
| `DELETE` | `/targets/{id}` | `204` oder `404` |
| `GET` | `/targets/{id}/results` | letzte Ergebnisse, `?limit=` 1–1000, Standard 50 |
| `GET` | `/healthz` | lebt der Prozess? |
| `GET` | `/readyz` | ist die Datenbank erreichbar? |

Ab hier gehen wir upwatch Paket für Paket durch, angefangen mit der HTTP-Schicht. Sie steckt vollständig in zwei Dateien: [httpapi.go](../internal/httpapi/httpapi.go) und [middleware.go](../internal/httpapi/middleware.go). Die beiden Health-Endpunkte erklärt Teil 12.

## Warum kein Framework

Wer aus ASP.NET Core, Spring oder Express kommt, sucht als Erstes das Web-Framework. In Go gibt es mehrere bekannte: Gin, Echo, Fiber, Chi. upwatch verwendet keines davon, weil `net/http` kein Low-Level-Baustein ist, auf dem man erst ein Framework aufsetzen muss. Es enthält einen produktionsreifen HTTP/1.1- und HTTP/2-Server mit TLS, Timeouts und Graceful Shutdown, einen Router mit Methoden und Pfadparametern (seit Go 1.22), einen HTTP-Client und mit `net/http/httptest` die Testinfrastruktur; `encoding/json` liefert die Serialisierung.

Was ein Framework zusätzlich bietet – Binding, Validierung per Struct-Tags, Middleware-Sammlungen –, kostet eine Abhängigkeit mit eigenem Release-Zyklus, eigene Typen in jeder Handler-Signatur und oft eine eigene Vorstellung davon, wie Fehler behandelt werden. Bei sieben Endpunkten überwiegt der Preis. Wann sich das ändert, steht am Ende des Kapitels.

## `http.Handler` und `http.HandlerFunc`

Das ganze HTTP-Paket ruht auf einem Interface mit einer Methode:

```go
type Handler interface {
	ServeHTTP(ResponseWriter, *Request)
}
```

Alles, was Requests beantwortet, ist ein `Handler`: der Router, jede Middleware, jeder Endpunkt, ein Dateiserver, der Server von `expvar`. Damit man für einen einzelnen Endpunkt keinen eigenen Typ definieren muss, macht der Adapter `http.HandlerFunc` aus jeder Funktion mit der Signatur `func(http.ResponseWriter, *http.Request)` einen `Handler` – das Muster „Funktionstyp mit Methode“ aus Teil 04. Es trägt die ganze Middleware-Architektur weiter unten.

`ResponseWriter` ist ebenfalls ein Interface. Darüber setzt der Handler Header, schreibt den Statuscode und den Body. Die Reihenfolge ist fest: erst Header, dann `WriteHeader(status)`, dann `Write`. Wer zuerst schreibt, bekommt implizit `200`, und danach gesetzte Header werden ignoriert.

## Routing mit `ServeMux`

Seit Go 1.22 versteht der eingebaute Router Methoden und Platzhalter. Die Routen von upwatch stehen in `New`:

```go
mux := http.NewServeMux()
mux.HandleFunc("GET /targets", a.listTargets)
mux.HandleFunc("POST /targets", a.createTarget)
mux.HandleFunc("GET /targets/{id}", a.getTarget)
mux.HandleFunc("DELETE /targets/{id}", a.deleteTarget)
mux.HandleFunc("GET /targets/{id}/results", a.listResults)
mux.HandleFunc("GET /healthz", a.healthz)
mux.HandleFunc("GET /readyz", a.readyz)
```

Ein Muster hat die Form `[METHODE ][HOST]/PFAD`. `{id}` passt auf genau ein Pfadsegment, `{rest...}` auf den Rest des Pfades. Den Wert holt der Handler mit `r.PathValue("id")` – immer als String, die Umwandlung ist Sache des Handlers.

Einige Regeln, die man kennen sollte:

- Ein Muster mit `GET` passt auch auf `HEAD`.
- Passt ein Pfad, aber nicht die Methode, antwortet der Mux mit `405 Method Not Allowed` und einem korrekten `Allow`-Header. Passt gar nichts, mit `404`.
- Passen mehrere Muster, gewinnt das spezifischere. Sind zwei gleich spezifisch, panikt `HandleFunc` schon bei der Registrierung – Konflikte fallen also beim Start auf, nicht im Betrieb.
- Ein Muster ohne abschließenden Slash (`/targets`) passt nur auf genau diesen Pfad. Mit Slash (`/static/`) passt es auf den ganzen Teilbaum.

Vor Go 1.22 brauchte man für Methoden-Routing und Pfadparameter fast zwingend einen externen Router. Viele Tutorials und Blogposts stammen aus dieser Zeit; wenn du irgendwo `if r.Method != http.MethodPost` am Anfang jedes Handlers siehst, ist das der Grund.

## Vorstufe: ein In-Memory-Store

Bevor wir uns die echte API mit SQLite anschauen, eine reduzierte Fassung zum Ausprobieren: [inmemory/main.go](../beispiele/08-http-api/inmemory/main.go). Sie hat dieselben Routen für Ziele, hält sie aber in einer Map:

```go
// Store keeps targets in memory. Many readers may hold the RWMutex at the
// same time, writers get it exclusively.
type Store struct {
	mu      sync.RWMutex
	nextID  int64
	targets map[int64]Target
}

func (s *Store) Get(id int64) (Target, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.targets[id]
	if !ok {
		return Target{}, fmt.Errorf("target %d: %w", id, errNotFound)
	}
	return t, nil
}
```

Der Mutex ist nicht optional. `net/http` bearbeitet jeden Request in einer eigenen Goroutine, und Go-Maps sind nicht für gleichzeitige Zugriffe gebaut (Teil 06). Ohne Mutex bricht die Laufzeit unter gleichzeitigen Schreibzugriffen früher oder später mit „concurrent map writes“ ab.

Die Handler sind als Closures direkt in `NewHandler` registriert und holen die ID mit `r.PathValue("id")` aus dem Pfad – für ein Beispiel dieser Größe gut lesbar.

Starten und in einem zweiten Terminal ausprobieren:

```bash
go run ./beispiele/08-http-api/inmemory
curl -s -X POST localhost:8081/targets -d '{"url":"https://example.com"}'
```

```text
{"id":1,"url":"https://example.com","interval_seconds":60}
```

Getestet wird ohne laufenden Prozess: [main_test.go](../beispiele/08-http-api/inmemory/main_test.go) startet den Handler mit `httptest.NewServer` auf einem freien Port im Testprozess und spielt eine Tabelle von Requests mit erwartetem Status und Body durch (`go test ./beispiele/08-http-api/...`). Wie `httptest` funktioniert, zeigt Teil 11.

Die Vorstufe validiert kaum, unterscheidet keine Fehlerarten und hat keine Middleware. Genau das ergänzt der echte Code.

## Der echte Code: `httpapi.go`

### Abhängigkeiten und Aufbau

Das Paket definiert, was es vom Speicher braucht, als Interface beim Verbraucher:

```go
// Store is the subset of persistence the API needs.
type Store interface {
	CreateTarget(ctx context.Context, t monitor.Target) (monitor.Target, error)
	Target(ctx context.Context, id int64) (monitor.Target, error)
	Targets(ctx context.Context) ([]monitor.Target, error)
	DeleteTarget(ctx context.Context, id int64) error
	Results(ctx context.Context, targetID int64, limit int) ([]monitor.Result, error)
	Ping(ctx context.Context) error
}
```

`*sqlitestore.Store` erfüllt es, ohne davon zu wissen. Die Handler sind Methoden auf `*API`, das Store und Logger hält; `New` baut daraus den fertigen `http.Handler` samt Middleware. Nach außen ist nur `New` sichtbar – `main` muss weder `API` noch die Handler kennen.

### DTOs getrennt vom Domänenmodell

Die JSON-Darstellung hat eigene Typen:

```go
type targetJSON struct {
	ID              int64     `json:"id"`
	URL             string    `json:"url"`
	IntervalSeconds int64     `json:"interval_seconds"`
	CreatedAt       time.Time `json:"created_at"`
}
```

Man könnte `monitor.Target` direkt mit JSON-Tags versehen. Das spart einen Typ, koppelt aber Domänenmodell und öffentliche API: `Interval` ist ein `time.Duration`, das `encoding/json` als Nanosekunden ausgäbe (`"interval":60000000000`), jedes neue interne Feld würde Teil der API, und jede Umbenennung bräche Clients. `resultJSON` enthält zusätzlich das Feld `up`, das im Domänenmodell eine Methode ist (`Result.Up()`), und rechnet die Dauer in Millisekunden um. Die Eingabe hat mit `createTargetRequest` ebenfalls einen eigenen Typ, der nur die Felder enthält, die ein Client setzen darf – `id` oder `created_at` lassen sich nicht einschmuggeln (in ASP.NET als „Overposting“ bekannt, in Spring als Mass Assignment).

### JSON lesen: `readJSON`

```go
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
	if dec.More() {
		return errors.New("body must contain a single JSON object")
	}
	return nil
}
```

Jede Zeile hat einen Grund:

| Zeile | ohne sie |
|---|---|
| `http.MaxBytesReader` (16 KiB) | Ein Client könnte beliebig viel Speicher belegen. Bei Überschreitung liefert der Reader `*http.MaxBytesError`, und der Server schließt die Verbindung nach der Antwort. |
| `json.NewDecoder` statt `io.ReadAll` + `json.Unmarshal` | Der Body würde erst komplett gepuffert, und die beiden folgenden Optionen gäbe es nicht. |
| `DisallowUnknownFields` | `encoding/json` ignoriert unbekannte Felder. Ein Tippfehler wie `"intervall": 30` würde still mit dem Standardintervall angelegt statt als `json: unknown field "intervall"` gemeldet. |
| `dec.More()` | `Decode` liest genau einen Wert; ein Body wie `{"url":"…"}{"url":"…"}` würde still zur Hälfte verarbeitet. |

Die Decoder-Fehlermeldung wird mit `%v` eingebettet, nicht mit `%w` – der Text geht an den Client, aber niemand soll auf `*json.SyntaxError` prüfen (Teil 05).

### JSON schreiben: `writeJSON`

```go
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
```

Die Reihenfolge Header – Status – Body ist zwingend. Den Fehler von `Encode` ignoriert die Funktion sichtbar mit `_ =`: Nach `WriteHeader` ist der Status bereits unterwegs, und ein Schreibfehler bedeutet fast immer, dass der Client die Verbindung geschlossen hat – es gibt niemanden mehr, dem man ihn melden könnte. Alle Fehlerantworten haben dieselbe Form `{"error": "…"}`, Clients brauchen also nur ein Format zu kennen.

### Pfadparameter: `pathID`

Drei Handler brauchen eine ID aus dem Pfad. Der Helfer `pathID` parst `r.PathValue("id")`, schreibt im Fehlerfall selbst die `400`-Antwort und meldet mit `false`, dass der Handler aufhören soll:

```go
id, ok := pathID(w, r)
if !ok {
	return
}
```

Ein Framework würde das per Binding und Struct-Tags lösen; hier sind es neun Zeilen, die man komplett lesen kann.

### Anlegen mit `Location`-Header

```go
func (a *API) createTarget(w http.ResponseWriter, r *http.Request) {
	var req createTargetRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	interval, err := secondsToDuration(req.IntervalSeconds)
	if err != nil {
		a.handleError(w, r, err)
		return
	}
	target, err := monitor.NewTarget(req.URL, interval)
	if err != nil {
		a.handleError(w, r, err)
		return
	}
	created, err := a.store.CreateTarget(r.Context(), target)
	if err != nil {
		a.handleError(w, r, err)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/targets/%d", created.ID))
	writeJSON(w, http.StatusCreated, toTargetJSON(created))
}
```

`secondsToDuration` gab es in der ersten Fassung nicht; dort stand `time.Duration(req.IntervalSeconds)*time.Second` direkt im Aufruf. Beim Schreiben der Musterlösungen fiel auf, dass das überlaufen kann: `time.Duration` zählt Nanosekunden in einem `int64`, und `{"interval_seconds":18446744084}` ergab nach der Multiplikation genau 10 Sekunden – ein gültiges Intervall. `NewTarget` sieht nur das Ergebnis und kann den Überlauf nicht mehr erkennen. Deshalb prüft `secondsToDuration` die Obergrenze, bevor es multipliziert. Allgemein gilt: Eingaben im Wertebereich des Eingabetyps prüfen, nicht erst nach einer Umrechnung.

Der Ablauf ist der Kern jeder schreibenden API: Eingabe dekodieren, in ein gültiges Domänenobjekt überführen (`monitor.NewTarget` validiert und normalisiert die URL), speichern, antworten. Die Validierung liegt im Domänenpaket, nicht im Handler – derselbe Code gilt damit auch, wenn Ziele später über ein CLI oder einen Import angelegt werden. `201 Created` mit `Location` ist die HTTP-Konvention für „neue Ressource unter dieser URL“. Fehler aus `NewTarget` und `CreateTarget` laufen beide durch `handleError`, das aus Teil 05 bekannt ist: `ValidationError` wird `400`, `ErrDuplicate` wird `409`, alles andere `500` ohne interne Details.

### `404` oder leere Liste

`listResults` enthält eine Abfrage, die auf den ersten Blick überflüssig wirkt:

```go
// Distinguish "unknown target" (404) from "no results yet" (empty list).
if _, err := a.store.Target(r.Context(), id); err != nil {
	a.handleError(w, r, err)
	return
}
results, err := a.store.Results(r.Context(), id, limit)
```

`Results` liefert für ein unbekanntes Ziel einfach keine Zeilen – genau wie für ein Ziel, das gerade erst angelegt wurde. Für den Client ist das ein wichtiger Unterschied: „Dieses Ziel gibt es nicht“ (Tippfehler in der ID, Ziel gelöscht) ist etwas anderes als „noch keine Messungen“. Die zusätzliche Abfrage kostet einen Primärschlüsselzugriff.

Dazu passt ein Detail bei der Ausgabe:

```go
out := make([]resultJSON, 0, len(results))
```

Ein `nil`-Slice kodiert `encoding/json` als `null`, ein leeres Slice als `[]`. Clients, die über das Ergebnis iterieren, stolpern über `null`. Deshalb wird das Ausgabe-Slice immer mit `make` angelegt.

## Middleware als Funktionen

Middleware kennst du von jeder Plattform. Nur die Form unterscheidet sich:

| Plattform | Middleware | Reihenfolge ergibt sich aus |
|---|---|---|
| ASP.NET Core | Delegate oder Klasse, `app.Use(async (ctx, next) => { …; await next(); … })` | Reihenfolge der `app.Use…`-Aufrufe |
| Java | Servlet-`Filter` mit `chain.doFilter(req, res)`, in Spring zusätzlich `HandlerInterceptor` mit `preHandle`/`postHandle` | Filter-Registrierung bzw. `@Order` |
| Express | `app.use((req, res, next) => { …; next(); })` | Reihenfolge der `app.use`-Aufrufe |
| Go | Funktion `func(next http.Handler) http.Handler` | Verschachtelung im Code |

In Go ist Middleware also kein eigenes Konzept, sondern eine Funktion, die einen `Handler` nimmt und einen `Handler` zurückgibt ([middleware.go](../internal/httpapi/middleware.go)):

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

Code vor `next.ServeHTTP` läuft auf dem Hinweg, Code danach auf dem Rückweg – wie Code vor und nach `await next()` in ASP.NET oder vor und nach `chain.doFilter` in einem Servlet-Filter. Verkettet wird durch Verschachtelung, am Ende von `New`:

```go
return a.logRequests(a.recoverPanics(securityHeaders(mux)))
```

Die äußerste Funktion ist die erste, die den Request sieht, und die letzte, die die Antwort sieht:

```text
           ┌─ logRequests ──────────────────────────────────┐
Request ──►│  ┌─ recoverPanics ──────────────────────────┐  │
           │  │  ┌─ securityHeaders ──────────────────┐  │  │
           │  │  │  ┌─ mux ────────────────────────┐  │  │  │
           │  │  │  │  Handler, z. B. getTarget    │  │  │  │
           │  │  │  └──────────────────────────────┘  │  │  │
           │  │  └────────────────────────────────────┘  │  │
Antwort ◄──│  └──────────────────────────────────────────┘  │
           └────────────────────────────────────────────────┘
```

Ein Request läuft von außen nach innen durch `logRequests`, `recoverPanics` und `securityHeaders` in den Mux, der den passenden Handler aufruft. Die Antwort nimmt denselben Weg zurück. Die Reihenfolge ist nicht beliebig. Stünde `recoverPanics` außen, würde ein Panic den Code nach `next.ServeHTTP` in `logRequests` überspringen – genau der Request, der schiefging, fehlte im Log. So wandelt `recoverPanics` den Panic in eine 500 um, und `logRequests` protokolliert sie wie jede andere Antwort. Die Security-Header stehen vor dem Mux und gelten damit auch für dessen eigene `404`- und `405`-Antworten. Bei drei Middlewares ist die Verschachtelung gut lesbar; bei zehn würde man sich eine kleine Hilfsfunktion `chain(h, mws...)` schreiben – oder einen Router wie chi in Betracht ziehen.

Middlewares, die Abhängigkeiten brauchen, sind Methoden auf `*API` (`logRequests` braucht den Logger), die anderen sind freie Funktionen. Für Middleware mit Konfiguration schreibt man eine Funktion, die die Middleware zurückgibt: `func timeout(d time.Duration) func(http.Handler) http.Handler`. `recoverPanics` ist in Teil 05 beschrieben.

<details><summary>Vertiefung: Wie <code>logRequests</code> an den Statuscode kommt</summary>

`ResponseWriter` bietet keine Methode, den geschriebenen Statuscode abzufragen. `logRequests` umhüllt den Writer deshalb mit einem eigenen Typ, der das Interface einbettet und nur `WriteHeader` abfängt:

```go
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
```

`Header()` und `Write()` kommen über die Einbettung vom eingebetteten Writer. Der Startwert `http.StatusOK` in `logRequests` deckt Handler ab, die nie `WriteHeader` aufrufen. `Unwrap` löst ein Problem solcher Wrapper: Der echte Writer von `net/http` kann mehr als die drei Interface-Methoden, etwa Flushen für Streaming. `http.ResponseController` (seit Go 1.20) findet diese Fähigkeiten über `Unwrap`; ohne die Methode scheiterte `http.NewResponseController(w).Flush()` hinter der Logging-Middleware.

</details>

## `http.Server`

`main` startet die API nicht mit der Bequemlichkeitsfunktion `http.ListenAndServe(addr, handler)`, sondern konfiguriert einen eigenen `http.Server` mit `ReadHeaderTimeout`, `ReadTimeout`, `WriteTimeout`, `IdleTimeout` und `MaxHeaderBytes` ([main.go](../cmd/upwatch/main.go)). `ListenAndServe` setzt keine Timeouts; ein Client, der Header im Schneckentempo sendet, könnte dann eine Verbindung und ihre Goroutine beliebig lange belegen. Was die einzelnen Werte begrenzen, zeigt Teil 13, das Herunterfahren mit `srv.Shutdown` Teil 15.

## Jetzt du: der curl-Rundgang

Mit einer Datenbank in einem temporären Verzeichnis lässt sich upwatch gefahrlos starten. Spiel den Rundgang selbst durch und vergleiche mit den Ausgaben hier:

```bash
export UPWATCH_DB="$(mktemp -d)/upwatch.db"
UPWATCH_ADDR=localhost:8090 go run ./cmd/upwatch
```

In einem zweiten Terminal:

```bash
curl -s localhost:8090/healthz; curl -s localhost:8090/readyz; curl -s localhost:8090/targets
```

```text
{"status":"ok"}
{"status":"ready"}
[]
```

Ein Ziel anlegen:

```bash
curl -i -X POST localhost:8090/targets \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com","interval_seconds":30}'
```

```
HTTP/1.1 201 Created
Cache-Control: no-store
Content-Type: application/json
Location: /targets/1
X-Content-Type-Options: nosniff
Date: Sat, 10 Oct 2026 20:31:06 GMT
Content-Length: 99

{"id":1,"url":"https://example.com","interval_seconds":30,"created_at":"2026-10-10T20:31:06.492Z"}
```

Die Fehlerfälle, jeweils mit `-s` und nur dem Body:

```bash
curl -s -X POST localhost:8090/targets -d '{"url":"https://example.com"}'
curl -s -X POST localhost:8090/targets -d '{"url":"ftp://example.com"}'
curl -s -X POST localhost:8090/targets -d '{"url":"https://example.org","intervall":30}'
curl -s -X POST localhost:8090/targets -d '{"url":"https://example.org"}{"url":"https://example.net"}'
curl -s localhost:8090/targets/99
curl -s localhost:8090/targets/abc
```

```
{"error":"target already exists"}
{"error":"invalid url: scheme must be http or https"}
{"error":"invalid JSON: json: unknown field \"intervall\""}
{"error":"body must contain a single JSON object"}
{"error":"target not found"}
{"error":"id must be a positive integer"}
```

Die Statuscodes dazu sind `409`, `400`, `400`, `400`, `404` und `400`. Eine falsche Methode beantwortet der Mux selbst:

```bash
curl -i -X PUT localhost:8090/targets/1
```

```
HTTP/1.1 405 Method Not Allowed
Allow: DELETE, GET, HEAD
Content-Type: text/plain; charset=utf-8
...
Method Not Allowed
```

Hier sieht man zweierlei: Der `Allow`-Header listet die erlaubten Methoden einschließlich des automatisch unterstützten `HEAD`. Und der Body ist Text, kein JSON – der Mux kennt das Fehlerformat der API nicht. Für die meisten Clients ist der Statuscode ausschlaggebend; wer auch hier JSON will, muss `404` und `405` selbst behandeln (siehe Übungen).

Nach ein paar Sekunden hat der Scheduler das Ziel geprüft. Zum Schluss Ergebnisse abfragen und das Ziel zweimal löschen:

```bash
curl -s 'localhost:8090/targets/1/results?limit=5'
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE localhost:8090/targets/1
curl -s -X DELETE localhost:8090/targets/1
```

```text
[{"id":1,"status_code":200,"duration_ms":525,"up":true,"checked_at":"2026-10-10T20:31:07.233Z"}]
204
{"error":"target not found"}
```

Im Terminal des Servers protokolliert `logRequests` jeden dieser Requests mit Methode, Pfad, Status und Dauer, und Ctrl+C beendet den Prozess sauber mit `shutting down` und `stopped` – was dabei im Hintergrund passiert, zeigt Teil 07.

## Die andere Seite: `http.Client`

`net/http` enthält auch den Client, und upwatch nutzt ihn für die Checks in [check.go](../internal/check/check.go). Ein `http.Client` ist für gleichzeitige Nutzung gebaut und hält einen Verbindungspool; man erzeugt ihn einmal und verwendet ihn überall. Ohne Context oder Konfiguration hat er keinen Timeout – `http.Get(url)` wartet im Zweifel ewig. upwatch erzeugt jeden Request deshalb mit `http.NewRequestWithContext` und einem Context mit Timeout (Teil 07) und schließt den Body immer mit `defer resp.Body.Close()`.

Den Client selbst baut `check.NewClient` mit einem geklonten `http.DefaultTransport`, einem Dialer, der private Adressen ablehnt, und ohne Redirect-Verfolgung. Das ist vor allem eine Sicherheitsfrage und steht deshalb in Teil 13.

## Wann ein Router oder Framework sinnvoll wird

Die Standardbibliothek reicht für upwatch, und sie reicht für mehr, als viele erwarten. Wann sich die Rechnung ändert:

| Situation | Empfehlung |
|---|---|
| wenige Routen, gemeinsame Middleware für alle | `net/http` mit `ServeMux`, wie upwatch |
| Routengruppen mit unterschiedlicher Middleware (`/admin/…` mit Auth, `/public/…` ohne) | [chi](https://github.com/go-chi/chi): `r.Route`, `r.Group`, voll kompatibel zu `http.Handler` – jede Middleware aus diesem Kapitel läuft unverändert weiter |
| fertige Middleware für CORS, Rate Limiting, Kompression, JWT/OIDC | Pakete für `net/http`-kompatible Router, eingehängt wie `securityHeaders` |
| API als Vertrag mit anderen Teams oder Kunden | OpenAPI zuerst, Server-Stubs und DTOs mit einem Generator wie oapi-codegen erzeugen (für `net/http` oder chi) |
| Team schätzt die Konventionen von Gin oder Echo | möglich, aber eine größere Bindung: eigene Context-Typen in jeder Handler-Signatur; Fiber baut auf fasthttp und ist nicht mit `http.Handler` kompatibel |

Eine pragmatische Reihenfolge: mit `net/http` anfangen, chi einführen, wenn Routengruppen fehlen, und OpenAPI-Generierung, wenn die API zum Vertrag mit Dritten wird.

## Im Projekt

- [httpapi.go](../internal/httpapi/httpapi.go): Routen, DTOs, `readJSON`, `writeJSON`, `pathID`, `handleError`, `/healthz` und `/readyz`.
- [middleware.go](../internal/httpapi/middleware.go): `statusRecorder` mit `Unwrap`, `logRequests`, `recoverPanics`, `securityHeaders`.
- [httpapi_test.go](../internal/httpapi/httpapi_test.go): Tests mit `httptest.NewServer` gegen eine echte SQLite-Datenbank im Temp-Verzeichnis, ohne Mocks.
- [main.go](../cmd/upwatch/main.go): `http.Server` mit Timeouts, Graceful Shutdown, ein zweiter `ServeMux` für den Debug-Listener.
- [check.go](../internal/check/check.go): `http.Client` mit eigenem Transport, `NewRequestWithContext`, Body schließen und begrenzt auslesen.

## Übungen

Musterlösungen mit Begründung stehen im Branch [`loesungen`](https://github.com/casoon/go-tutorial/blob/loesungen/loesungen/08-http-api.md) – versuch es zuerst selbst.

1. **Aufwärmen:** Hänge in der In-Memory-Vorstufe eine eigene Middleware ein, die jeder Antwort den Header `X-Upwatch-Example: 1` mitgibt, und prüfe mit `curl -i`, dass er auch bei `404` und `405` des Mux ankommt. Was ändert sich, wenn du sie stattdessen nur um einen einzelnen Handler legst?
2. **Kern:** `readJSON` prüft den `Content-Type` nicht; `curl -d` schickt standardmäßig `application/x-www-form-urlencoded`, und die API akzeptiert das trotzdem. Lass `createTarget` bei einem anderen Typ als `application/json` mit `415 Unsupported Media Type` antworten (Tipp: `mime.ParseMediaType`) und ergänze einen Fall in `httpapi_test.go`.
3. **Kern:** Ergänze `PATCH /targets/{id}`, mit dem sich das Intervall ändern lässt. Du brauchst eine neue Store-Methode, ein Request-DTO und die Validierung aus `monitor`. Was soll passieren, wenn der Body `"url"` enthält?
4. **Profi:** Sorge dafür, dass auch unbekannte Pfade und falsche Methoden mit JSON im Format `{"error": "…"}` beantwortet werden. Eine Möglichkeit: eine Middleware, die den `ResponseWriter` wie `statusRecorder` umhüllt, `404`- und `405`-Antworten des Mux erkennt und ersetzt. Achte darauf, dass der `Allow`-Header erhalten bleibt.

---

← [Teil 07: Context richtig verstehen](07-context.md) · [Übersicht](../README.md) · [Teil 09: Projektstruktur und Architektur](09-projektstruktur.md) →
