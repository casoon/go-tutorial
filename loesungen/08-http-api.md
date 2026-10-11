# Lösungen zu Teil 08: Eine HTTP-API mit net/http

Zu den Aufgaben: [Teil 08](../kapitel/08-http-api.md#übungen)

Alle vier Übungen haben einen Patch. Die Ausgaben stammen aus echten Läufen auf einem Apple-M-Rechner mit Go 1.27.1; die Zeile `Date:` ist jeweils weggelassen. Für die Läufe lauschten die Server auf Ports zwischen `18260` und `18273` statt auf `8081` bzw. `8090` – bei dir gelten die Ports aus dem Kapitel.

## Übung 1: Eine eigene Middleware in der Vorstufe (Aufwärmen)

```go
// exampleHeader sets X-Upwatch-Example on every response. Headers must be
// set before the wrapped handler writes the status, so this happens on
// the way in.
func exampleHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upwatch-Example", "1")
		next.ServeHTTP(w, r)
	})
}
```

Am Ende von `NewHandler` wird aus `return mux` ein `return exampleHeader(mux)`. Der Patch ergänzt einen Test, der den Header bei 200, bei den `404` und `405` des Mux und beim `404` des eigenen Handlers prüft.

```bash
git apply loesungen/08-http-api/uebung-1.patch
go test ./beispiele/08-http-api/...
go run ./beispiele/08-http-api/inmemory
```

```
$ curl -i localhost:8081/nope
HTTP/1.1 404 Not Found
Content-Type: text/plain; charset=utf-8
X-Content-Type-Options: nosniff
X-Upwatch-Example: 1
Content-Length: 19

404 page not found

$ curl -i -X PUT localhost:8081/targets/1
HTTP/1.1 405 Method Not Allowed
Allow: DELETE, GET, HEAD
Content-Type: text/plain; charset=utf-8
X-Content-Type-Options: nosniff
X-Upwatch-Example: 1
Content-Length: 19

Method Not Allowed
```

Liegt die Middleware dagegen nur um einen einzelnen Handler, etwa `mux.Handle("GET /healthz", exampleHeader(http.HandlerFunc(…)))`, trägt nur dessen Antwort den Header. Die Antworten des Mux bekommen ihn nicht mehr – auch nicht die auf denselben Pfad mit falscher Methode, denn die `405` schreibt der Mux, nicht der Handler:

```
$ curl -i localhost:8081/healthz
HTTP/1.1 200 OK
Content-Type: application/json
X-Upwatch-Example: 1
Content-Length: 16

{"status":"ok"}

$ curl -i -X PUT localhost:8081/healthz
HTTP/1.1 405 Method Not Allowed
Allow: GET, HEAD
Content-Type: text/plain; charset=utf-8
X-Content-Type-Options: nosniff
Content-Length: 19

Method Not Allowed
```

**Warum so:** Der Header muss *vor* `next.ServeHTTP` gesetzt werden. Danach hat der Handler den Status schon geschrieben, und spätere Header werden ignoriert – deshalb wäre die Middleware auf dem Rückweg wirkungslos. Die Unterscheidung „um den Mux“ oder „um einen Handler“ ist die Go-Form von globaler Middleware und Middleware pro Route: Alles außerhalb des Mux sieht jeden Request, auch die, für die es keine Route gibt; alles innerhalb nur die Requests seiner Route. In upwatch steht `securityHeaders` aus genau diesem Grund außen um den Mux. Für Header, die wirklich nur eine Route betreffen (etwa `Cache-Control` für eine einzelne, cachebare Ressource), ist die Variante pro Handler richtig.

## Übung 2: `415 Unsupported Media Type` (Kern)

```go
// isJSON reports whether the request declares a JSON body. Parameters
// such as charset are allowed, the media type itself must match.
func isJSON(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/json"
}
```

`createTarget` prüft das vor dem Lesen des Body:

```go
if !isJSON(r) {
	writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
	return
}
```

Im Test war mehr zu tun als ein neuer Fall: Der Helfer `do` in [httpapi_test.go](../internal/httpapi/httpapi_test.go) schickte bisher gar keinen `Content-Type`, alle bestehenden POST-Fälle hätten nun 415 bekommen. `do` setzt deshalb `application/json`, sobald es einen Body gibt. Der neue Test deckt die Grenzfälle ab:

```go
{"application/x-www-form-urlencoded", http.StatusUnsupportedMediaType}, // curl -d
{"text/plain", http.StatusUnsupportedMediaType},
{"", http.StatusUnsupportedMediaType},
{"application/json; charset=utf-8", http.StatusCreated},
{"Application/JSON", http.StatusCreated}, // media types are case-insensitive
```

```bash
git apply loesungen/08-http-api/uebung-2.patch
go test -race ./internal/httpapi
```

Gegen den laufenden Server:

```
$ curl -i -X POST localhost:8090/targets -d '{"url":"https://example.com"}'
HTTP/1.1 415 Unsupported Media Type
Cache-Control: no-store
Content-Type: application/json
X-Content-Type-Options: nosniff
Content-Length: 50

{"error":"Content-Type must be application/json"}

$ curl -s -X POST localhost:8090/targets -H 'Content-Type: application/json' -d '{"url":"https://example.com"}'
{"id":1,"url":"https://example.com","interval_seconds":60,"created_at":"2026-10-11T09:45:04.799Z"}
```

**Warum so:** `mime.ParseMediaType` statt eines String-Vergleichs, weil der Header Parameter (`; charset=utf-8`), Großschreibung und Leerzeichen enthalten darf; die Funktion normalisiert den Typ auf Kleinbuchstaben. Ein fehlender Header ist hier ebenfalls 415 – das ist die strenge Lesart; manche APIs akzeptieren einen fehlenden Typ und lehnen nur falsche ab, um einfache Clients nicht zu brechen. Die Prüfung steht in `createTarget` und nicht in `readJSON`, weil die Aufgabe es so verlangt und weil `readJSON` dann seinen Rückgabefehler um einen zweiten Statuscode erweitern müsste; bei mehreren schreibenden Endpunkten wäre eine kleine Middleware für `POST`/`PATCH` die sauberere Stelle. Eine Folge solltest du kennen: Alle `curl -d`-Aufrufe ohne `-H 'Content-Type: application/json'` in den Teilen 05 bis 08 bekämen mit diesem Patch eine 415. Strenge an der Eingangstür hat ihren Preis bei der Bequemlichkeit – deshalb fehlt die Prüfung im Tutorial-Code.

## Übung 3: `PATCH /targets/{id}` (Kern)

Drei Schichten ändern sich. In [monitor.go](../internal/monitor/monitor.go) wandert die Intervallprüfung aus `NewTarget` in eine eigene Funktion, die `NewTarget` weiter benutzt:

```go
// ValidateInterval reports whether d lies between MinInterval and
// MaxInterval. Unlike NewTarget it has no default for zero.
func ValidateInterval(d time.Duration) error {
	if d < MinInterval || d > MaxInterval {
		return &ValidationError{
			Field:  "interval",
			Reason: fmt.Sprintf("must be between %s and %s", MinInterval, MaxInterval),
		}
	}
	return nil
}
```

Der Store bekommt `UpdateInterval`, das mit `RETURNING` das geänderte Ziel in einer einzigen Anweisung zurückliefert:

```go
func (s *Store) UpdateInterval(ctx context.Context, id int64, interval time.Duration) (monitor.Target, error) {
	next := time.Now().Add(interval).UnixMilli()
	row := s.db.QueryRowContext(ctx,
		`UPDATE targets SET interval_seconds = ?, next_check_at = MIN(next_check_at, ?)
		 WHERE id = ? RETURNING id, url, interval_seconds, created_at`,
		int64(interval/time.Second), next, id)
	t, err := scanTarget(row)
	if errors.Is(err, sql.ErrNoRows) {
		return monitor.Target{}, fmt.Errorf("target %d: %w", id, monitor.ErrNotFound)
	}
	// ...
}
```

Und die API ein DTO, eine Route und einen Handler:

```go
// updateTargetRequest lists the only field a client may change. The URL is
// the identity of a target; a different URL is a different target.
// A pointer tells "missing" apart from 0.
type updateTargetRequest struct {
	IntervalSeconds *int64 `json:"interval_seconds"`
}

func (a *API) updateTarget(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req updateTargetRequest
	if err := readJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.IntervalSeconds == nil {
		writeError(w, http.StatusBadRequest, "interval_seconds is required")
		return
	}

	interval, err := secondsToDuration(*req.IntervalSeconds)
	if err != nil {
		a.handleError(w, r, err)
		return
	}
	if err := monitor.ValidateInterval(interval); err != nil {
		a.handleError(w, r, err)
		return
	}
	t, err := a.store.UpdateInterval(r.Context(), id, interval)
	if err != nil {
		a.handleError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toTargetJSON(t))
}
```

```bash
git apply loesungen/08-http-api/uebung-3.patch
go test -race ./internal/...
```

```
$ curl -s -X PATCH localhost:8090/targets/1 -d '{"interval_seconds":30}'
{"id":1,"url":"https://example.com","interval_seconds":30,"created_at":"2026-10-11T09:46:10.154Z"}
$ curl -s -X PATCH localhost:8090/targets/1 -d '{"url":"https://example.org"}'
{"error":"invalid JSON: json: unknown field \"url\""}
$ curl -s -X PATCH localhost:8090/targets/1 -d '{"interval_seconds":5}'
{"error":"invalid interval: must be between 10s and 24h0m0s"}
$ curl -s -X PATCH localhost:8090/targets/99 -d '{"interval_seconds":30}'
{"error":"target not found"}
$ curl -si -X PUT localhost:8090/targets/1 | grep Allow
Allow: DELETE, GET, HEAD, PATCH
```

Die letzte Zeile zeigt nebenbei, dass der Mux die neue Methode sofort im `Allow`-Header der `405` meldet.

Was passiert mit `"url"` im Body? Hier gibt es keine einzig richtige Antwort, aber eine gute: ablehnen, und zwar mit `400`. Das DTO hat kein Feld `url`, und `DisallowUnknownFields` in `readJSON` erledigt den Rest ohne eine Zeile zusätzlichen Code. Die URL ist die Identität eines Ziels (`UNIQUE` in der Datenbank); ein Ziel mit anderer URL ist ein anderes Ziel, dessen Messreihe nicht mit der alten vermischt werden sollte. Wer die URL ändern will, legt ein neues Ziel an und löscht das alte. Stillschweigend ignorieren wäre die schlechteste Wahl: Der Client bekäme `200` und glaubte, die URL sei geändert.

**Warum so:** `ValidateInterval` als eigene Funktion hält die Regel an einem Ort; ein Aufruf von `NewTarget(alteURL, neuesIntervall)` hätte auch validiert, aber die URL ein zweites Mal normalisiert und den Default für `0` angewendet – bei PATCH ist `0` ein Fehler, kein „Standard bitte“. Der Zeiger im DTO unterscheidet „Feld fehlt“ von `0`; ohne ihn würde `{}` als `0` und damit als ungültiges Intervall gemeldet, was den Client auf die falsche Fährte setzt. `next_check_at = MIN(…)` sorgt dafür, dass ein von 24 h auf 10 s verkürztes Intervall sofort greift statt nach dem Rest der alten 24 h; ein verlängertes Intervall lässt den bereits geplanten, früheren Check stehen. Der Store-Test im Patch prüft genau das über `ClaimDue`. `secondsToDuration` prüft den Wert, bevor er mit `time.Second` multipliziert wird: Ohne die Prüfung läuft `time.Duration(18446744084) * time.Second` über und landet bei gut 10 Sekunden, also mitten im gültigen Bereich. Der Testfall „patch overflow“ belegt das. Dieser Überlauf ist beim Schreiben genau dieser Lösung aufgefallen – er steckte auch in `createTarget`, wo `POST` mit `"interval_seconds":18446744084` ein Ziel mit 10 Sekunden anlegte. Auf `main` ist er inzwischen behoben (Teil 08 erzählt das), und die Lösung nutzt dieselbe Funktion.

## Übung 4: JSON auch für `404` und `405` des Mux (Profi)

Eine Middleware direkt um den Mux, die `WriteHeader` abfängt:

```go
func jsonErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&errorRewriter{ResponseWriter: w}, r)
	})
}

// errorRewriter intercepts WriteHeader. Our handlers answer 404 with JSON
// themselves; only the mux sends text/plain (via http.Error), so the
// Content-Type tells the two apart.
type errorRewriter struct {
	http.ResponseWriter
	replaced bool
}

func (e *errorRewriter) WriteHeader(code int) {
	isText := strings.HasPrefix(e.Header().Get("Content-Type"), "text/plain")
	if isText && (code == http.StatusNotFound || code == http.StatusMethodNotAllowed) {
		e.replaced = true
		// The Allow header of a 405 is already set and stays untouched.
		writeError(e.ResponseWriter, code, strings.ToLower(http.StatusText(code)))
		return
	}
	e.ResponseWriter.WriteHeader(code)
}

// Write drops the text body of a replaced response.
func (e *errorRewriter) Write(b []byte) (int, error) {
	if e.replaced {
		return len(b), nil
	}
	return e.ResponseWriter.Write(b)
}

func (e *errorRewriter) Unwrap() http.ResponseWriter { return e.ResponseWriter }
```

Eingehängt als innerste Middleware: `a.logRequests(a.recoverPanics(securityHeaders(jsonErrors(mux))))`.

```bash
git apply loesungen/08-http-api/uebung-4.patch
go test -race ./internal/httpapi
```

```
$ curl -i localhost:8090/nope
HTTP/1.1 404 Not Found
Cache-Control: no-store
Content-Type: application/json
X-Content-Type-Options: nosniff
Content-Length: 22

{"error":"not found"}

$ curl -i -X PUT localhost:8090/targets/1
HTTP/1.1 405 Method Not Allowed
Allow: DELETE, GET, HEAD
Cache-Control: no-store
Content-Type: application/json
X-Content-Type-Options: nosniff
Content-Length: 31

{"error":"method not allowed"}
```

Im Log stehen beide Requests weiterhin mit `status=404` und `status=405`, weil `jsonErrors` den Statuscode unverändert durchreicht. Der Test im Patch prüft Status, `Content-Type`, Body und `Allow` für beide Fälle und zusätzlich, dass das `404` von `getTarget` (`{"error":"target not found"}`) unangetastet bleibt.

**Warum so:** Die Schwierigkeit ist nicht das Ersetzen, sondern das Erkennen. Auch die eigenen Handler antworten mit 404, und deren Antwort soll bleiben, wie sie ist. Der Statuscode allein reicht also nicht; das Merkmal ist der `Content-Type`: Der Mux schreibt seine Fehler mit `http.Error`, das `text/plain; charset=utf-8` setzt, die Handler schreiben über `writeJSON` immer `application/json`. Weil `http.Error` zuerst die Header setzt und dann `WriteHeader` aufruft, ist der `Allow`-Header beim Abfangen schon in der Header-Map – er bleibt erhalten, ohne dass die Middleware etwas dafür tun muss. Die Middleware steht innen, direkt um den Mux, damit sie nur Antworten des Mux und der Handler sieht; `recoverPanics` außen schreibt sein 500 ohnehin als JSON. Eine naheliegende Alternative funktioniert nicht: Eine Catch-all-Route `mux.HandleFunc("/", notFound)` beantwortet zwar unbekannte Pfade, schluckt aber auch die 405. Ein kurzer Versuch mit `GET /targets/{id}` und `/` ergibt für `PUT /targets/1` ein `404` ohne `Allow`-Header, weil `/` auf jede Methode passt und der Mux deshalb keinen Methodenfehler mehr feststellt. Wer das Problem grundsätzlich lösen will, greift zu einem Router mit konfigurierbaren `NotFound`- und `MethodNotAllowed`-Handlern wie chi; für zwei Statuscodes reicht der Wrapper.
