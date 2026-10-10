# Teil 08: Eine HTTP-API mit net/http

Nach diesem Kapitel kannst du eine JSON-API allein mit der Standardbibliothek bauen: Routing mit Methoden und Pfadparametern, sauberes Lesen und Schreiben von JSON, Fehler-Mapping und Middleware. Du kennst die HTTP-API von upwatch Zeile für Zeile und weißt, ab wann sich ein Router oder Framework lohnt.

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

Ab diesem Kapitel geht es um den echten upwatch-Code. Die HTTP-Schicht steckt vollständig in zwei Dateien: [httpapi.go](../internal/httpapi/httpapi.go) und [middleware.go](../internal/httpapi/middleware.go).

## Warum kein Framework

Wer aus ASP.NET Core oder Express kommt, sucht als Erstes das Web-Framework. In Go gibt es mehrere bekannte: Gin, Echo, Fiber, Chi. upwatch verwendet keines davon, und das ist keine Askese, sondern eine Abwägung.

Das Paket `net/http` ist kein Low-Level-Baustein, auf dem man erst ein Framework aufsetzen muss. Es enthält einen produktionsreifen HTTP/1.1- und HTTP/2-Server mit TLS, Timeouts, Graceful Shutdown und Keep-Alive, einen Router mit Methoden und Pfadparametern (seit Go 1.22), einen HTTP-Client und mit `net/http/httptest` die Testinfrastruktur. `encoding/json` liefert die Serialisierung. Damit ist alles da, was eine JSON-API braucht.

Was ein Framework zusätzlich bietet – Binding, Validierung per Struct-Tags, eigene Context-Typen, Middleware-Sammlungen –, kostet etwas: eine Abhängigkeit mit eigenem Release-Zyklus, eigene Typen in jeder Handler-Signatur und oft eine eigene Vorstellung davon, wie Fehler behandelt werden. Bei sieben Endpunkten überwiegt der Preis. Wann sich das ändert, steht am Ende des Kapitels.

## `http.Handler` und `http.HandlerFunc`

Das ganze HTTP-Paket ruht auf einem Interface mit einer Methode:

```go
type Handler interface {
	ServeHTTP(ResponseWriter, *Request)
}
```

Alles, was Requests beantwortet, ist ein `Handler`: der Router, jede Middleware, jeder Endpunkt, ein Dateiserver, der Server von `expvar`. Weil man für einen einzelnen Endpunkt keinen eigenen Typ definieren will, gibt es einen Adapter:

```go
type HandlerFunc func(ResponseWriter, *Request)

func (f HandlerFunc) ServeHTTP(w ResponseWriter, r *Request) { f(w, r) }
```

Ein Funktionstyp mit einer Methode: Jede Funktion mit der passenden Signatur wird durch die Konvertierung `http.HandlerFunc(f)` zum `Handler`. Das ist das Go-typische Muster aus Kapitel 4 – kleine Interfaces, implizit erfüllt –, und es trägt die ganze Middleware-Architektur weiter unten.

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

Der Mutex ist nicht optional. `net/http` bearbeitet jeden Request in einer eigenen Goroutine, und Go-Maps sind nicht für gleichzeitige Zugriffe gebaut (Kapitel 6). Ohne Mutex bricht die Laufzeit unter gleichzeitigen Schreibzugriffen früher oder später mit „concurrent map writes“ ab.

Die Handler sind hier als Closures direkt in `NewHandler` registriert, was für ein Beispiel dieser Größe gut lesbar ist:

```go
mux.HandleFunc("GET /targets/{id}", func(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "id must be an integer")
		return
	}
	t, err := store.Get(id)
	if errors.Is(err, errNotFound) {
		writeError(w, http.StatusNotFound, "target not found")
		return
	}
	writeJSON(w, http.StatusOK, toJSON(t))
})
```

Starten und ausprobieren:

```bash
go run ./beispiele/08-http-api/inmemory
```

```bash
curl -i -X POST localhost:8081/targets -d '{"url":"https://example.com"}'
```

```
HTTP/1.1 201 Created
Content-Type: application/json
Location: /targets/1
Date: Sat, 10 Oct 2026 20:30:54 GMT
Content-Length: 59

{"id":1,"url":"https://example.com","interval_seconds":60}
```

Getestet wird ohne laufenden Prozess. `httptest.NewServer` startet den Handler auf einem freien Port im Testprozess, und der Test spricht ihn über echtes HTTP an ([main_test.go](../beispiele/08-http-api/inmemory/main_test.go)):

```go
srv := httptest.NewServer(NewHandler(NewStore()))
defer srv.Close()

steps := []struct {
	method, path, body string
	wantStatus         int
	wantBody           string
}{
	{"GET", "/targets", "", 200, "[]"},
	{"POST", "/targets", `{"url":"https://example.com","interval_seconds":30}`, 201, `"id":1`},
	{"POST", "/targets", `{"url":"https://example.com","color":"red"}`, 400, "unknown field"},
	{"GET", "/targets/1", "", 200, `"interval_seconds":30`},
	{"GET", "/targets/abc", "", 400, "integer"},
	{"PUT", "/targets/1", "", 405, ""},
	{"DELETE", "/targets/1", "", 204, ""},
	{"GET", "/targets/1", "", 404, "not found"},
	{"GET", "/healthz", "", 200, "ok"},
}
```

Für einzelne Handler ohne Netzwerk gibt es `httptest.NewRecorder`, ein `ResponseWriter`, der alles mitschreibt; `TestLocationHeader` im selben File nutzt ihn.

```bash
go test ./beispiele/08-http-api/...
```

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

Man könnte `monitor.Target` direkt mit JSON-Tags versehen und ausgeben. Das spart einen Typ und eine Konvertierungsfunktion, koppelt aber drei Dinge aneinander, die sich unabhängig ändern sollten. `monitor.Target.Interval` ist ein `time.Duration`, das `encoding/json` als Nanosekunden-Zahl ausgeben würde – kein Client will `"interval":60000000000`. Ein neues internes Feld würde ohne Zutun Teil der öffentlichen API. Und eine Umbenennung im Domänenmodell bräche alle Clients. `resultJSON` zeigt den Nutzen noch deutlicher: Es enthält das Feld `up`, das im Domänenmodell eine Methode ist (`Result.Up()`), und rechnet die Dauer in Millisekunden um.

Ebenso hat die Eingabe einen eigenen Typ, `createTargetRequest`. Er enthält nur die Felder, die ein Client setzen darf. Ein Client kann also weder `id` noch `created_at` mitschicken und damit etwas überschreiben – ein typisches Problem, wenn Request-Bodies direkt in Entitäten gebunden werden (in ASP.NET als „Overposting“ bekannt).

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

Jede Zeile hat einen Grund.

`http.MaxBytesReader` begrenzt den Body auf 16 KiB (`maxBodyBytes`). Ohne Grenze könnte ein Client beliebig viel Speicher belegen. Wird die Grenze überschritten, liefert der Reader einen `*http.MaxBytesError`, und der Server schließt die Verbindung nach der Antwort.

`json.NewDecoder` liest direkt aus dem Stream, statt erst alles mit `io.ReadAll` in einen Puffer zu laden und dann `json.Unmarshal` aufzurufen. Für kleine Bodies ist der Unterschied gering, aber der Decoder bietet zwei Optionen, die `Unmarshal` nicht hat.

`DisallowUnknownFields` macht aus einem Tippfehler einen Fehler. Standardmäßig ignoriert `encoding/json` unbekannte Felder. Schickt ein Client `"intervall": 30`, würde das Ziel stillschweigend mit dem Standardintervall angelegt. So bekommt er stattdessen `json: unknown field "intervall"`.

`dec.More()` prüft, ob nach dem ersten JSON-Wert noch etwas kommt. `Decode` liest genau einen Wert und hört dann auf; ein Body wie `{"url":"…"}{"url":"…"}` würde sonst still zur Hälfte verarbeitet.

Die Decoder-Fehlermeldung wird mit `%v` eingebettet, nicht mit `%w` – der Text geht an den Client, aber niemand soll auf `*json.SyntaxError` prüfen (Kapitel 5).

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

Die Reihenfolge Header – Status – Body ist zwingend. Den Fehler von `Encode` ignoriert die Funktion sichtbar mit `_ =`: Nach `WriteHeader` ist der Status bereits unterwegs, ein Fehler beim Schreiben bedeutet fast immer, dass der Client die Verbindung geschlossen hat, und es gibt niemanden mehr, dem man ihn melden könnte. Ein Encoding-Fehler im engeren Sinn kann bei den hier verwendeten Typen nicht auftreten.

Alle Fehlerantworten haben dieselbe Form `{"error": "…"}`. Clients brauchen nur ein Format zu kennen.

### Pfadparameter: `pathID`

```go
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return 0, false
	}
	return id, true
}
```

Drei Handler brauchen eine ID aus dem Pfad. `pathID` schreibt im Fehlerfall die Antwort selbst und meldet mit `false`, dass der Handler aufhören soll:

```go
id, ok := pathID(w, r)
if !ok {
	return
}
```

Das ist ein bewusst kleiner Helfer, keine Abstraktion. Ein Framework würde das per Binding und Struct-Tags lösen; hier sind es neun Zeilen, die man komplett lesen kann.

### Anlegen mit `Location`-Header

```go
func (a *API) createTarget(w http.ResponseWriter, r *http.Request) {
	var req createTargetRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	target, err := monitor.NewTarget(req.URL, time.Duration(req.IntervalSeconds)*time.Second)
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

Der Ablauf ist der Kern jeder schreibenden API: Eingabe dekodieren, in ein gültiges Domänenobjekt überführen (`monitor.NewTarget` validiert und normalisiert die URL), speichern, antworten. Die Validierung liegt im Domänenpaket, nicht im Handler – derselbe Code gilt damit auch, wenn Ziele später über ein CLI oder einen Import angelegt werden. `201 Created` mit `Location` ist die HTTP-Konvention für „neue Ressource unter dieser URL“. Fehler aus `NewTarget` und `CreateTarget` laufen beide durch `handleError`, das aus Kapitel 5 bekannt ist: `ValidationError` wird `400`, `ErrDuplicate` wird `409`, alles andere `500` ohne interne Details.

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

### `/healthz` und `/readyz`

```go
// healthz answers "is the process alive?" and never touches dependencies.
func (a *API) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

Zwei Endpunkte, zwei Fragen. `/healthz` fragt nur, ob der Prozess Requests beantwortet; ein Orchestrator startet den Prozess neu, wenn das scheitert. `/readyz` prüft zusätzlich die Datenbank mit einem Timeout von einer Sekunde und liefert `503`, wenn sie nicht erreichbar ist; ein Load Balancer schickt dann vorübergehend keinen Traffic. Würde `/healthz` die Datenbank prüfen, würde ein kurzer Datenbankausfall zu Neustarts aller Instanzen führen, die das Problem nicht lösen.

## Middleware als Funktionen

In ASP.NET Core ist Middleware eine Klasse oder ein Delegate in einer Pipeline, die `app.Use…` aufbaut; in Express eine Funktion mit `next`. In Go ist Middleware eine Funktion, die einen `Handler` nimmt und einen `Handler` zurückgibt ([middleware.go](../internal/httpapi/middleware.go)):

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

Code vor `next.ServeHTTP` läuft auf dem Hinweg, Code danach auf dem Rückweg. Verkettet wird durch Verschachtelung, am Ende von `New`:

```go
return a.logRequests(a.recoverPanics(securityHeaders(mux)))
```

Von außen nach innen gelesen: Ein Request durchläuft zuerst `logRequests`, dann `recoverPanics`, dann `securityHeaders` und landet schließlich im Mux. Die Reihenfolge ist nicht beliebig. Stünde `recoverPanics` außen, würde ein Panic den Code nach `next.ServeHTTP` in `logRequests` überspringen – genau der Request, der schiefging, fehlte im Log. So wandelt `recoverPanics` den Panic in eine 500 um, und `logRequests` protokolliert sie wie jede andere Antwort. Die Security-Header stehen vor dem Mux und gelten damit auch für dessen eigene `404`- und `405`-Antworten. Bei drei Middlewares ist die Verschachtelung gut lesbar; bei zehn würde man sich eine kleine Hilfsfunktion `chain(h, mws...)` schreiben – oder hier einen Router wie chi in Betracht ziehen.

Middlewares, die Abhängigkeiten brauchen, sind Methoden auf `*API` (`logRequests` braucht den Logger), die anderen sind freie Funktionen. Für Middleware mit Konfiguration schreibt man eine Funktion, die die Middleware zurückgibt: `func timeout(d time.Duration) func(http.Handler) http.Handler`.

### Den Statuscode mitlesen: `statusRecorder`

Die Logging-Middleware will den Statuscode loggen, den der Handler geschrieben hat. `ResponseWriter` bietet keine Methode, ihn abzufragen. Die Lösung ist ein Wrapper, der das `ResponseWriter`-Interface einbettet und eine Methode überschreibt:

```go
// statusRecorder remembers the status code written by the wrapped handler.
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

Durch die Einbettung hat `statusRecorder` automatisch `Header()` und `Write()` des eingebetteten Writers; nur `WriteHeader` wird abgefangen. Der Startwert `http.StatusOK` in `logRequests` deckt Handler ab, die direkt `Write` aufrufen, ohne `WriteHeader` – dann sendet `net/http` implizit `200`.

`Unwrap` löst ein Problem, das solche Wrapper früher hatten. Der echte `ResponseWriter` von `net/http` unterstützt optionale Fähigkeiten wie Flushen (für Streaming) oder das Setzen von Deadlines pro Request. Ein Wrapper verdeckt diese, weil er nur die drei Methoden des Interfaces hat. `http.ResponseController` (seit Go 1.20) sucht über `Unwrap` die Kette entlang nach dem Writer, der die Fähigkeit tatsächlich hat. Ohne `Unwrap` würde `http.NewResponseController(w).Flush()` hinter der Logging-Middleware mit einem Fehler scheitern.

Die dritte Middleware, `recoverPanics`, ist in Kapitel 5 beschrieben.

## `http.Server`

`main` startet die API nicht mit `http.ListenAndServe(addr, handler)`, sondern konfiguriert einen `http.Server` ([main.go](../cmd/upwatch/main.go)):

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

Die Bequemlichkeitsfunktion `http.ListenAndServe` setzt keine Timeouts. Ein Client, der Header im Schneckentempo sendet, könnte dann eine Verbindung beliebig lange offen halten. Was die einzelnen Timeouts genau begrenzen und wie sie mit Graceful Shutdown zusammenspielen, behandelt Kapitel 13. Hier reicht: Für einen öffentlich erreichbaren Server immer einen eigenen `http.Server` mit Timeouts verwenden.

## Ausprobieren mit `curl`

Mit einer Datenbank in einem temporären Verzeichnis lässt sich upwatch gefahrlos starten:

```bash
export UPWATCH_DB="$(mktemp -d)/upwatch.db"
UPWATCH_ADDR=localhost:8090 go run ./cmd/upwatch
```

In einem zweiten Terminal:

```bash
curl -s localhost:8090/healthz
curl -s localhost:8090/readyz
curl -s localhost:8090/targets
```

```
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
Cache-Control: no-store
Content-Type: text/plain; charset=utf-8
X-Content-Type-Options: nosniff
...

Method Not Allowed
```

Hier sieht man zweierlei: Der `Allow`-Header listet die erlaubten Methoden einschließlich des automatisch unterstützten `HEAD`. Und der Body ist Text, kein JSON – der Mux kennt das Fehlerformat der API nicht. Für die meisten Clients ist der Statuscode ausschlaggebend; wer auch hier JSON will, muss `404` und `405` selbst behandeln (siehe Übungen).

Nach ein paar Sekunden hat der Scheduler das Ziel geprüft:

```bash
curl -s 'localhost:8090/targets/1/results?limit=5'
```

```
[{"id":1,"status_code":200,"duration_ms":525,"up":true,"checked_at":"2026-10-10T20:31:07.233Z"}]
```

Und zum Schluss löschen, zweimal:

```bash
curl -s -o /dev/null -w '%{http_code}\n' -X DELETE localhost:8090/targets/1
curl -s -X DELETE localhost:8090/targets/1
```

```
204
{"error":"target not found"}
```

Im Terminal des Servers protokolliert `logRequests` jeden Request, und Ctrl+C beendet den Prozess sauber:

```
time=... level=INFO msg=request method=POST path=/targets status=201 duration=461µs
time=... level=INFO msg=request method=POST path=/targets status=409 duration=144.834µs
...
time=... level=INFO msg="shutting down"
time=... level=INFO msg=stopped
```

## Die andere Seite: `http.Client`

`net/http` enthält auch den Client, und upwatch nutzt ihn für die Checks in [check.go](../internal/check/check.go). Drei Dinge unterscheiden die Verwendung dort vom naiven `http.Get(url)`.

Erstens gibt es keinen Timeout ohne Context oder Konfiguration. `http.DefaultClient`, den `http.Get` verwendet, wartet im Zweifel ewig. upwatch erzeugt jeden Request mit `http.NewRequestWithContext` und einem Context mit Timeout (Kapitel 7).

Zweitens baut `check.NewClient` einen eigenen Client mit eigenem Transport:

```go
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
```

`Clone` übernimmt die sinnvollen Standardwerte (HTTP/2, Keep-Alive, Timeouts für TLS-Handshake) und ändert nur, was nötig ist. Der eigene Dialer prüft die Zieladresse gegen private Netze; warum, erklärt Kapitel 13. `CheckRedirect` sorgt dafür, dass ein Redirect als Ergebnis gemeldet und nicht verfolgt wird. Ein `http.Client` ist für gleichzeitige Nutzung gebaut und hält einen Verbindungspool. Man erzeugt ihn einmal und verwendet ihn überall – nicht einen pro Request.

Drittens muss der Body immer geschlossen werden, sonst bleibt die Verbindung belegt:

```go
defer resp.Body.Close()

// Drain a bounded part of the body so the connection can be reused.
_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
```

Das begrenzte Auslesen erlaubt dem Transport, die Verbindung für den nächsten Check wiederzuverwenden, ohne dass eine riesige Seite komplett heruntergeladen wird.

## Wann ein Router oder Framework sinnvoll wird

Die Standardbibliothek reicht für upwatch, und sie reicht für mehr, als viele erwarten. Es gibt aber Punkte, an denen sich die Rechnung ändert.

**Viele Routen mit gemeinsamer Middleware.** `ServeMux` kennt keine Routengruppen. Wenn `/admin/…` Authentifizierung braucht, `/public/…` nicht und `/api/v2/…` einen anderen Fehlerformatierer, wird die Verschachtelung unübersichtlich. [chi](https://github.com/go-chi/chi) löst das mit `r.Route` und `r.Group`, bleibt dabei aber vollständig kompatibel zu `net/http`: Handler sind `http.Handler`, Middleware hat die Signatur `func(http.Handler) http.Handler` Der Umstieg ist deshalb klein und umkehrbar, und jede Middleware aus diesem Kapitel liefe unverändert weiter.

**Middleware-Ökosystem.** CORS, Rate Limiting, Kompression, Request-IDs, Authentifizierung mit JWT oder OIDC – jedes davon ist in wenigen Dutzend Zeilen selbst geschrieben, aber nicht jedes sollte man selbst schreiben. Für `net/http`-kompatible Router wie chi gibt es fertige Pakete, die sich genauso wie `securityHeaders` einhängen lassen.

**OpenAPI.** Wenn eine API von mehreren Teams oder externen Kunden genutzt wird, ist eine maschinenlesbare Spezifikation wertvoll. Dann lohnt es sich, die Richtung umzukehren: Spezifikation zuerst schreiben und Server-Stubs, DTOs und Client daraus generieren lassen. Generatoren wie oapi-codegen können Code für reines `net/http` und für chi erzeugen. Handgeschriebene DTOs wie `targetJSON` entfallen dann.

**Gin, Echo, Fiber.** Diese Frameworks bringen eigene Context-Typen (`*gin.Context`, `echo.Context`, `*fiber.Ctx`) mit, die in jeder Handler-Signatur stehen. Gin und Echo bauen intern auf `net/http` auf; Fiber dagegen auf fasthttp, einer eigenen HTTP-Implementierung, die nicht mit `http.Handler` kompatibel ist. Wer sich für eines davon entscheidet, bindet die HTTP-Schicht stärker an das Framework. Das kann sich lohnen, wenn ein Team dessen Konventionen schätzt – es ist aber eine größere Entscheidung als die für chi.

Eine pragmatische Reihenfolge: mit `net/http` anfangen, chi einführen, wenn Routengruppen fehlen, und OpenAPI-Generierung, wenn die API zum Vertrag mit Dritten wird.

## Im Projekt

- [httpapi.go](../internal/httpapi/httpapi.go): Routen, DTOs, `readJSON`, `writeJSON`, `pathID`, `handleError`, `/healthz` und `/readyz`.
- [middleware.go](../internal/httpapi/middleware.go): `statusRecorder` mit `Unwrap`, `logRequests`, `recoverPanics`, `securityHeaders`.
- [httpapi_test.go](../internal/httpapi/httpapi_test.go): Tests mit `httptest.NewServer` gegen eine echte SQLite-Datenbank im Temp-Verzeichnis, ohne Mocks.
- [main.go](../cmd/upwatch/main.go): `http.Server` mit Timeouts, Graceful Shutdown, ein zweiter `ServeMux` für den Debug-Listener.
- [check.go](../internal/check/check.go): `http.Client` mit eigenem Transport, `NewRequestWithContext`, Body schließen und begrenzt auslesen.

## Übungen

1. `readJSON` prüft den `Content-Type` nicht; `curl -d` schickt standardmäßig `application/x-www-form-urlencoded`, und die API akzeptiert das trotzdem. Lass `createTarget` bei einem anderen Typ als `application/json` mit `415 Unsupported Media Type` antworten (Tipp: `mime.ParseMediaType`) und ergänze einen Fall in `httpapi_test.go`.
2. Ergänze `PATCH /targets/{id}`, mit dem sich das Intervall ändern lässt. Du brauchst eine neue Store-Methode, ein Request-DTO und die Validierung aus `monitor`. Was soll passieren, wenn der Body `"url"` enthält?
3. Sorge dafür, dass auch unbekannte Pfade und falsche Methoden mit JSON im Format `{"error": "…"}` beantwortet werden. Eine Möglichkeit: eine Middleware, die den `ResponseWriter` so umhüllt, dass sie `404`- und `405`-Antworten des Mux erkennt und ersetzt. Achte darauf, dass der `Allow`-Header erhalten bleibt.
4. Portiere die In-Memory-Vorstufe auf chi in einem separaten Testmodul außerhalb des Repos und vergleiche: Welche Zeilen ändern sich, welche Middleware bleibt identisch?

---

← [Teil 07: Context richtig verstehen](07-context.md) · [Übersicht](../README.md) · [Teil 09: Projektstruktur und Architektur](09-projektstruktur.md) →
