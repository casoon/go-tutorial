# Teil 05: Fehlerbehandlung

Nach diesem Kapitel weißt du, warum Go Fehler als normale Rückgabewerte behandelt, wie du sie mit Kontext anreicherst, gezielt prüfst und an der Systemgrenze in Statuscodes übersetzt – und wann `panic` das richtige Werkzeug ist.

> **upwatch in diesem Teil:** die Fehlerwerte in [monitor.go](../internal/monitor/monitor.go), ihre Übersetzung in [store.go](../internal/sqlitestore/store.go) und `handleError` in [httpapi.go](../internal/httpapi/httpapi.go)
> **Voraussetzungen:** Teil 03 und 04

## Auf einen Blick

Die Tabelle ordnet die Begriffe dieses Kapitels dem zu, was du kennst. Die Go-Spalte wird in den folgenden Abschnitten erklärt.

| C# | Java | TypeScript | Go |
|---|---|---|---|
| `throw new X(…)` | `throw new X(…)` | `throw new Error(…)` | `return …, err` |
| `catch (X ex)` | `catch (X e)` | `catch (e: unknown)` plus `instanceof` | `errors.As(err, &target)` |
| `catch (…) when (ex == …)`, Exception-Filter | – | – | `errors.Is(err, ErrX)` |
| `finally`, `using` | `finally`, try-with-resources | `finally` | `defer` |
| `InnerException` | `getCause()` | `cause` | `%w` in `fmt.Errorf`, `Unwrap` |
| nicht in der Signatur | `throws X` | nicht in der Signatur | `error` als letzter Rückgabewert |

## Das Ausgangsbeispiel

```go
t, err := store.Target(ctx, id)
if err != nil {
	return fmt.Errorf("load target for check: %w", err)
}
```

Drei Zeilen, die in jedem Go-Programm hundertfach vorkommen. `Target` aus [store.go](../internal/sqlitestore/store.go) gibt zwei Werte zurück: das Ziel und einen Fehler (`ctx` kannst du bis Teil 07 ignorieren). Ist `err` nicht `nil`, ist `t` bedeutungslos (in der Regel der Zero Value), und die Funktion reicht den Fehler weiter – nicht unverändert, sondern mit einem Satz Kontext davor.

`error` ist dabei kein Sprachkonstrukt, sondern ein gewöhnliches, eingebautes Interface:

```go
type error interface {
	Error() string
}
```

Jeder Typ mit einer Methode `Error() string` ist ein Fehler. Es gibt keine Klassenhierarchie, kein `throws`, keinen Stack-Unwinding-Mechanismus. Fehler sind Werte, und mit Werten macht man, was man mit Werten eben macht: zurückgeben, vergleichen, in Variablen halten, in Slices sammeln.

## Warum keine Exceptions

In C# oder TypeScript sieht derselbe Code so aus:

```csharp
var target = store.Target(id); // may throw – or may not
```

Ob `Target` werfen kann, steht nirgends. Die Signatur verrät es nicht, der Aufrufer sieht es nicht, und der Kontrollfluss springt im Fehlerfall an eine Stelle, die womöglich drei Dateien entfernt liegt. Das ist bequem, solange alles gut geht, und unangenehm, wenn man verstehen will, was im Fehlerfall passiert: Jede Zeile ist ein potenzieller Ausgang. Java hat mit Checked Exceptions versucht, das sichtbar zu machen; in der Praxis hat das vor allem zu `throws Exception` und leeren `catch`-Blöcken geführt.

Go macht den Fehlerpfad zum Teil der Signatur und des Codes. `func (s *Store) Target(ctx context.Context, id int64) (monitor.Target, error)` sagt: Diese Funktion kann scheitern, und du musst dich entscheiden, was dann passiert. Beim Lesen siehst du jede Stelle, an der eine Funktion vorzeitig verlassen wird. Es gibt keine unsichtbaren Sprünge.

Der Preis ist Schreibarbeit, und darüber wird viel geklagt. Aber `if err != nil` ist kein Ritual, das man abarbeitet, sondern genau die Stelle, an der du eine Entscheidung triffst. Du kannst

- Kontext hinzufügen und weitergeben (der häufigste Fall),
- den Fehler in einen anderen übersetzen (etwa `sql.ErrNoRows` in `monitor.ErrNotFound`),
- ihn behandeln und weitermachen (ein Fallback, ein Default, ein Retry),
- ihn bewusst ignorieren – dann aber sichtbar mit `_ =`.

In C# passiert dieselbe Abwägung, nur stillschweigend: Wer keinen `try`-Block schreibt, hat entschieden, den Fehler unverändert durchzureichen. Go zwingt dich, die Entscheidung hinzuschreiben. Das macht Code länger, aber auch reviewbar: Ein fehlendes `if err != nil` fällt auf, ein fehlendes `try` nicht.

Ein zweiter, weniger offensichtlicher Grund: Exceptions sind teuer und für Ausnahmefälle gedacht. In einem Server sind Fehler aber normal. Eine Ziel-URL antwortet nicht, ein Datensatz existiert nicht, ein Client schickt kaputtes JSON. Wenn diese Fälle normale Werte sind, behandelt man sie auch wie normale Fälle.

## Kontext hinzufügen mit `fmt.Errorf` und `%w`

Ein nackt durchgereichter Fehler ist am Ende der Kette oft wertlos. „not found“ in einem Log – nicht gefunden, was? Deshalb fügt jede Schicht hinzu, was sie weiß. Im Beispiel [fehler/main.go](../beispiele/05-fehlerbehandlung/fehler/main.go) gibt es dafür ein Repository und einen Service:

```go
// repository
return Target{}, fmt.Errorf("target %d: %w", id, ErrNotFound)

// service
return "", fmt.Errorf("describe: %w", err)
```

Die Meldung, die oben ankommt, liest sich wie ein Pfad durch das Programm:

```
describe: target 2: not found
```

Das ersetzt den Stacktrace, den Go-Fehler nicht mitbringen, und ist meist nützlicher, weil er die fachlich relevanten Werte enthält statt Zeilennummern. Die Konventionen dafür: Fehlermeldungen klein geschrieben, ohne Punkt am Ende, ohne „failed to“ oder „error:“ als Präfix – sie werden ja aneinandergereiht. Und jede Schicht ergänzt nur, was die darüberliegende nicht ohnehin weiß. In upwatch nennt der Store die ID (`"target %d: %w"`), der Handler fügt nichts mehr hinzu, weil er den Fehler nur noch in einen Statuscode übersetzt.

Das Verb `%w` (wrap) unterscheidet sich von `%v` in einem Punkt: Der ursprüngliche Fehler bleibt als Objekt erhalten, nicht nur als Text. `fmt.Errorf` mit `%w` liefert einen Fehler mit einer `Unwrap()`-Methode, und darüber können `errors.Is` und `errors.As` in die Kette hineinschauen.

## Sentinel-Errors und `errors.Is`

Ein Sentinel-Error ist ein Fehlerwert auf Paketebene, gegen den Aufrufer vergleichen. Die Standardbibliothek ist voll davon: `io.EOF`, `sql.ErrNoRows`, `http.ErrServerClosed`. upwatch definiert zwei in [monitor.go](../internal/monitor/monitor.go):

```go
var (
	ErrNotFound  = errors.New("not found")
	ErrDuplicate = errors.New("already exists")
)
```

Weil der Fehler unterwegs eingepackt wurde, hilft `==` nicht mehr: `err == monitor.ErrNotFound` ist für `"target 2: not found"` falsch. `errors.Is` dagegen läuft die Kette über `Unwrap` ab und vergleicht jedes Glied:

```go
if errors.Is(err, monitor.ErrNotFound) {
	// 404
}
```

Interessant ist, wo diese Sentinels entstehen. Der SQLite-Store übersetzt in [store.go](../internal/sqlitestore/store.go) den Treiberfehler in den Domänenfehler:

```go
t, err := scanTarget(row)
if errors.Is(err, sql.ErrNoRows) {
	return monitor.Target{}, fmt.Errorf("target %d: %w", id, monitor.ErrNotFound)
}
```

Hier steht kein `%w` auf `sql.ErrNoRows`. Würde der Store den SQL-Fehler durchreichen, müsste der HTTP-Handler `database/sql` kennen, um „nicht gefunden“ zu erkennen – und ein In-Memory-Store für Tests müsste `sql.ErrNoRows` zurückgeben, obwohl er gar keine Datenbank hat. Mit `monitor.ErrNotFound` als gemeinsamer Sprache sprechen alle Store-Implementierungen denselben Dialekt, und das `monitor`-Paket bleibt frei von Infrastruktur.

## Eigene Error-Typen und `errors.As`

Ein Sentinel trägt nur seine Identität. Brauchst du Details – welches Feld, welcher Grund –, definierst du einen eigenen Typ:

```go
// ValidationError describes invalid user input. Handlers map it to 400.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Reason)
}
```

`monitor.NewTarget` und `monitor.NormalizeURL` geben ihn zurück, etwa `&ValidationError{Field: "url", Reason: "scheme must be http or https"}`. Um ihn aus einer Kette herauszuholen, gibt es `errors.As`:

```go
var ve *monitor.ValidationError // 1. a pointer variable, still nil
if errors.As(err, &ve) {        // 2. pass the address of that variable
	// 3. As found a *ValidationError in the chain and stored it in ve
	writeError(w, http.StatusBadRequest, ve.Error())
}
```

Das sieht beim ersten Mal seltsam aus: `&ve` ist ein Zeiger auf einen Zeiger. Schritt für Schritt:

1. `Error()` ist auf `*ValidationError` definiert, also steckt in der Kette ein Wert vom Typ `*ValidationError`. Die Variable, die ihn aufnehmen soll, hat deshalb genau diesen Typ: `var ve *monitor.ValidationError`. Sie ist zunächst `nil`.
2. `errors.As` soll diese Variable füllen. Eine Funktion kann eine Variable des Aufrufers nur verändern, wenn sie deren Adresse bekommt – also `&ve`, Typ `**monitor.ValidationError`. Das ist dasselbe Prinzip wie `*dst = n` in der Closure aus Teil 03 oder wie ein `out`-Parameter in C#: `errors.As(err, &ve)` entspricht ungefähr `TryFind(err, out ValidationError? ve)`.
3. `As` läuft die Kette ab, findet den ersten Fehler, der sich `ve` zuweisen lässt, schreibt ihn hinein und gibt `true` zurück.

Wer stattdessen `var ve monitor.ValidationError` schreibt, übergibt einen Zeiger auf einen Typ, der `error` gar nicht implementiert: `go vet` meldet das, und zur Laufzeit panikt `errors.As` mit „target must be interface or implement error“.

Seit Go 1.26 gibt es mit `errors.AsType` eine generische Variante, die ohne die Hilfsvariable auskommt:

```go
if ve, ok := errors.AsType[*monitor.ValidationError](err); ok {
	writeError(w, http.StatusBadRequest, ve.Error())
}
```

upwatch nutzt die klassische Form, weil sie in jedem bestehenden Go-Code vorkommt; beide sind gleichwertig.

Ein zweites `errors.As` steckt im Store, diesmal auf einen fremden Typ, den Fehler des SQLite-Treibers:

```go
func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}
```

Daraus wird `monitor.ErrDuplicate` – wieder die Übersetzung an der Paketgrenze.

Sentinel oder Typ? Ein Sentinel reicht, wenn der Aufrufer nur wissen muss, *dass* etwas passiert ist. Ein Typ lohnt sich, wenn er Daten braucht. Ein eigener Error-Typ ist ungefähr eine eigene Exception-Klasse, und die Tabelle am Kapitelanfang ordnet `errors.As` und `errors.Is` den passenden `catch`-Formen zu. Der Unterschied: Du prüfst explizit, wo du es brauchst, statt dass die Laufzeit einen passenden Handler sucht.

## `%w` oder `%v`: die Kette bewusst abschneiden

Mit `%w` wird der eingepackte Fehler Teil deiner API. Aufrufer können `errors.Is(err, io.ErrUnexpectedEOF)` schreiben und verlassen sich dann darauf, dass deine Funktion diesen Fehler auch in der nächsten Version noch durchreicht. Manchmal willst du das nicht. Dann formatierst du mit `%v`: Der Text bleibt, die Verbindung zum Original ist weg.

upwatch macht das in [httpapi.go](../internal/httpapi/httpapi.go) beim JSON-Decoding:

```go
if err := dec.Decode(dst); err != nil {
	// ...
	return fmt.Errorf("invalid JSON: %v", err)
}
```

Der Handler braucht vom Decoder-Fehler nur den Text für die Antwort an den Client. Niemand soll später anfangen, auf `*json.SyntaxError` zu prüfen, denn dann wäre es nicht mehr möglich, den Decoder auszutauschen, ohne Aufrufer zu brechen.

Die Faustregel: Innerhalb eines Pakets und zu Fehlern, die du selbst dokumentierst, wrappst du mit `%w`. An Grenzen, hinter denen ein Implementierungsdetail verschwinden soll, schneidest du mit `%v` ab oder übersetzt in einen eigenen Fehler. Das Beispiel zeigt beide Varianten nebeneinander:

```bash
go run ./beispiele/05-fehlerbehandlung/fehler
```

```
found    status=200 desc="#1 https://example.com every 1m0s" err=<nil>
missing  status=404 desc="" err=describe: target 2: not found
broken   status=500 desc="" err=describe: query target 1: disk full
validate status=400
invalid url: scheme must be http or https
invalid interval: must be at least 10s
wrapped with w, Is finds cause: true
wrapped with v, Is finds cause: false
```

## Mehrere Fehler: `errors.Join`

Manchmal gibt es nicht *einen* Fehler. [config.go](../internal/config/config.go) liest neun Umgebungsvariablen, und wenn drei davon kaputt sind, wäre es lästig, sie in drei Neustarts nacheinander zu entdecken. `config.Load` sammelt deshalb:

```go
var errs []error
// ...
errs = append(errs, fmt.Errorf("%s: want a positive integer, got %q", key, v))
// ...
// Report every problem at once instead of one per restart.
return cfg, errors.Join(errs...)
```

`errors.Join` liefert `nil`, wenn alle Argumente `nil` sind oder die Liste leer ist – deshalb kann `Load` es ohne Fallunterscheidung zurückgeben. Sonst entsteht ein Fehler, dessen Text die einzelnen Meldungen zeilenweise enthält und der über `Unwrap() []error` alle Teilfehler herausgibt. `errors.Is` und `errors.As` durchsuchen diesen Baum vollständig, eine Validierung mit mehreren `ValidationError`s wird also weiterhin als 400 erkannt.

## Fehler an der Grenze übersetzen

Innen sprechen die Pakete in Go-Fehlern, außen spricht die API HTTP. Die Übersetzung passiert an genau einer Stelle, `handleError` in [httpapi.go](../internal/httpapi/httpapi.go):

```go
// handleError maps domain errors to HTTP status codes. This is the only
// place where the API needs to know which errors exist.
func (a *API) handleError(w http.ResponseWriter, r *http.Request, err error) {
	var ve *monitor.ValidationError
	switch {
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, ve.Error())
	case errors.Is(err, monitor.ErrNotFound):
		writeError(w, http.StatusNotFound, "target not found")
	case errors.Is(err, monitor.ErrDuplicate):
		writeError(w, http.StatusConflict, "target already exists")
	default:
		a.serverError(w, r, err)
	}
}
```

Das `switch` ohne Ausdruck prüft die Fälle der Reihe nach; das ist Gos idiomatische Form für eine `if`-`else`-Kaskade. Jeder Handler ruft bei einem Fehler nur noch `a.handleError(w, r, err)` auf. Kommt ein neuer Domänenfehler hinzu, ändert sich eine Funktion, nicht zehn Handler.

Beachte, was zum Client geht: Bei `ValidationError` die Meldung selbst, denn sie ist für den Nutzer gedacht und enthält nur, was er geschickt hat. Bei `ErrNotFound` ein fester Text, nicht `err.Error()` – der lautete „target 99: not found“ und würde zwar nichts verraten, aber die Antwort hinge dann an Formulierungen im Store. Und bei allem anderen:

```go
// serverError logs the details and returns a generic message: internal
// error texts can leak SQL, file paths or hostnames.
func (a *API) serverError(w http.ResponseWriter, r *http.Request, err error) {
	a.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal server error")
}
```

Ein unerwarteter Fehler enthält dank sauberem Wrapping viel Kontext – Tabellennamen, Dateipfade, Hostnamen der Datenbank. Genau dieser Kontext gehört ins Log und nicht in die Antwort an einen beliebigen Client. Das ist dieselbe Regel wie „keine Stacktraces in Produktions-Fehlerseiten“ in ASP.NET, nur dass sie hier im Code steht statt in einer Konfiguration.

## Jetzt du: Fehler über die API auslösen

Starte upwatch auf einem freien Port mit einer Wegwerf-Datenbank und provoziere jeden Zweig von `handleError` einmal:

```bash
UPWATCH_ADDR=127.0.0.1:8081 UPWATCH_DB=/tmp/upwatch-errors.db go run ./cmd/upwatch
```

```bash
curl -s localhost:8081/targets/99                                        # ErrNotFound
curl -s -X POST localhost:8081/targets -d '{"url":"ftp://example.com"}'  # ValidationError
curl -s -X POST localhost:8081/targets -d '{"url":"https://example.com"}'
curl -s -X POST localhost:8081/targets -d '{"url":"https://example.com"}' # ErrDuplicate
curl -s -X POST localhost:8081/targets -d '{"url":"https://example.com","foo":1}'
```

```
{"error":"target not found"}
{"error":"invalid url: scheme must be http or https"}
{"id":1,"url":"https://example.com","interval_seconds":60,"created_at":"…"}
{"error":"target already exists"}
{"error":"invalid JSON: json: unknown field \"foo\""}
```

Achte auf zwei Dinge. Bei 404 und 409 steht in der Antwort ein fester Text, nicht die Meldung aus dem Store. Und die letzte Antwort enthält den Text des JSON-Decoders, weil `readJSON` ihn mit `%v` übernommen hat – als Text, ohne die Kette. Im Log des Servers erscheint jede Anfrage einmal mit ihrem Statuscode, keine doppelt. Beende den Server mit `Ctrl+C`.

## Einmal behandeln: loggen oder zurückgeben

Ein verbreitetes Muster aus Exception-Welten, das in Go besonders stört:

```go
// Don't: every layer logs and returns.
t, err := s.store.Target(ctx, id)
if err != nil {
	log.Printf("load target failed: %v", err)
	return fmt.Errorf("load target: %w", err)
}
```

Wenn jede Schicht so arbeitet, steht ein einziger Fehler fünfmal im Log, jedes Mal mit etwas weniger Kontext, und niemand weiß, welche Zeile die maßgebliche ist. Die Regel lautet deshalb: Ein Fehler wird genau einmal *behandelt*. Loggen ist eine Behandlung. Zurückgeben ist keine, sondern das Weiterreichen an jemanden, der mehr weiß.

In upwatch loggt der Store nie. Er gibt zurück, und erst ganz oben wird entschieden: im HTTP-Pfad von `serverError`, im Hintergrundbetrieb vom Scheduler. Der loggt einen Fehler beim Speichern in [scheduler.go](../internal/scheduler/scheduler.go) und macht mit dem nächsten Ergebnis weiter – denn dort gibt es niemanden mehr, an den er zurückgeben könnte:

```go
if err := s.store.SaveResult(ctx, r); err != nil {
	s.logger.Error("save result", "target_id", r.TargetID, "err", err)
}
```

Ganz am Ende der Kette steht `main`. In [main.go](../cmd/upwatch/main.go) gibt `run` einen Fehler zurück, `main` druckt ihn einmal und beendet den Prozess mit Exit-Code 1. `run` selbst ruft nie `os.Exit` auf, damit die `defer`-Aufrufe (Datenbank schließen) laufen.

## `panic` und `recover`

Go hat einen Mechanismus, der Exceptions ähnelt: `panic` bricht die aktuelle Goroutine ab, führt auf dem Weg nach oben alle `defer`-Funktionen aus und beendet das Programm, wenn niemand die Panik mit `recover` auffängt. Dass es ihn gibt, heißt nicht, dass man ihn für Fehler nutzt.

`panic` ist für Programmierfehler da – Zustände, die bei korrektem Code nicht eintreten können: ein Index außerhalb des Slices, ein Nil-Pointer, ein unmöglicher `switch`-Zweig. Ein nicht erreichbarer Server oder eine ungültige Benutzereingabe sind keine Programmierfehler, sondern erwartbare Situationen und damit `error`.

Der zweite legitime Fall sind `Must`-Funktionen für Werte, die im Quelltext feststehen. In [check.go](../internal/check/check.go):

```go
!netip.MustParsePrefix("100.64.0.0/10").Contains(ip) // carrier-grade NAT
```

Der Präfix ist ein Literal. Wenn er nicht parsebar wäre, hätte der Entwickler sich vertippt, und der erste Testlauf würde es zeigen. Ein `if err != nil` an dieser Stelle wäre Code für einen Fall, der in Produktion nicht auftreten kann. Dasselbe gilt für `regexp.MustCompile` oder `template.Must` auf Paketebene: Ein Fehler dort lässt das Programm beim Start scheitern, nicht irgendwann im Betrieb. Sobald der Eingabewert aus einer Datei, einer Variablen oder einem Request kommt, ist `Must` falsch.

`recover` gehört in sehr wenige Stellen, typischerweise an den Rand eines Systems, das viele unabhängige Aufgaben ausführt. Der HTTP-Server ist so ein Fall: Ein Bug in einem Handler soll nicht die anderen Requests mitnehmen. upwatch hat dafür eine Middleware in [middleware.go](../internal/httpapi/middleware.go):

```go
func (a *API) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				a.logger.Error("panic in handler", "path", r.URL.Path, "panic", v)
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
```

`defer func() { … }()` ist ein `defer` mit einer anonymen Funktion: Sie wird an dieser Stelle registriert – die Klammern `()` am Ende sind der Aufruf, der aufgeschoben wird – und läuft, wenn der Handler verlassen wird, also auch dann, wenn `next.ServeHTTP` panikt. `recover` wirkt nur innerhalb einer solchen `defer`-Funktion und nur in der Goroutine, in der die Panik auftritt. `http.ErrAbortHandler` ist ein Sonderfall: Mit diesem Wert bricht ein Handler absichtlich die Antwort ab, und `net/http` erwartet, ihn selbst zu sehen – deshalb wird er weitergeworfen.

Eine wichtige Grenze: Eine Panik in einer *anderen* Goroutine fängt diese Middleware nicht. Startet ein Handler eine Goroutine und die panikt, stürzt der ganze Prozess ab. Das ist Absicht – Go hat keinen globalen Exception-Handler, der Fehler in Hintergrund-Threads verschluckt. Fehler aus Goroutinen meldet man deshalb als `error`-Werte zurück, etwa über einen Channel. Mehr dazu im nächsten Kapitel.

## Fehler testen

Weil Fehler Werte sind, testest du sie wie Werte. Statt Strings zu vergleichen, prüfst du mit `errors.Is` und `errors.As`, denn der Text darf sich ändern, die Bedeutung nicht. Aus [main_test.go](../beispiele/05-fehlerbehandlung/fehler/main_test.go):

```go
tests := []struct {
	name string
	err  error
	want int
}{
	{"nil", nil, http.StatusOK},
	{"not found through two wraps", errMissing, http.StatusNotFound},
	{"validation", &ValidationError{Field: "url", Reason: "empty"}, http.StatusBadRequest},
	{"joined validation", Validate("example.com", time.Minute), http.StatusBadRequest},
	{"unknown", errDB, http.StatusInternalServerError},
}
```

Einen Textvergleich gibt es dort nur in einem Test, der genau die Meldungskette prüft – denn die *ist* in diesem Fall das, was getestet werden soll.

```bash
go test ./beispiele/05-fehlerbehandlung/...
```

## Im Projekt

- [monitor.go](../internal/monitor/monitor.go): die Sentinels `ErrNotFound` und `ErrDuplicate`, der Typ `ValidationError`, `NewTarget` und `NormalizeURL` als Quellen von Validierungsfehlern.
- [store.go](../internal/sqlitestore/store.go): Übersetzung von `sql.ErrNoRows` und `*sqlite.Error` in Domänenfehler, Wrapping mit Kontext in jeder Methode.
- [config.go](../internal/config/config.go): `errors.Join` für alle Konfigurationsfehler auf einmal.
- [httpapi.go](../internal/httpapi/httpapi.go): `handleError` als einzige Mapping-Stelle, `serverError` gegen Informationslecks, `%v` in `readJSON`.
- [middleware.go](../internal/httpapi/middleware.go): `recoverPanics`.
- [check.go](../internal/check/check.go): `Check` zeichnet Netzwerkfehler im Ergebnis auf, statt sie zurückzugeben – ein nicht erreichbarer Server ist ein gültiges Ergebnis, kein Fehler des Checkers.
- [main.go](../cmd/upwatch/main.go): `run() error` statt `os.Exit` mitten im Code.

## Übungen

Musterlösungen mit Begründung stehen im Branch [`loesungen`](https://github.com/casoon/go-tutorial/blob/loesungen/loesungen/05-fehlerbehandlung.md) – versuch es zuerst selbst.

1. **Aufwärmen:** Ersetze in `Describe` in [fehler/main.go](../beispiele/05-fehlerbehandlung/fehler/main.go) das `%w` durch `%v` und führe `go test ./beispiele/05-fehlerbehandlung/...` aus. Welcher Test schlägt fehl und warum? Mach die Änderung danach rückgängig.
2. **Aufwärmen:** Ersetze in `handleError` das `errors.As` durch `errors.AsType`. Laufen die Tests in `internal/httpapi` weiterhin durch?
3. **Kern:** Ändere in `sqlitestore.Store.Target` das Wrapping so, dass zusätzlich `sql.ErrNoRows` mit `%w` eingepackt wird (Tipp: `fmt.Errorf` akzeptiert seit Go 1.20 mehrere `%w`). Welcher Code außerhalb des Stores könnte sich jetzt darauf verlassen, und warum ist das unerwünscht?
4. **Profi:** `config.Load` meldet „want a positive integer“ als formatierten String. Schreibe einen eigenen Typ `*FieldError` mit Variablenname und Wert, gib ihn in `num` zurück und schreibe einen Test, der mit `errors.As` alle fehlerhaften Variablennamen aus dem `Join`-Fehler einsammelt. (Tipp: `errors.As` liefert nur den ersten Treffer; für alle brauchst du `Unwrap() []error`.)

---

← [Teil 04: Go-Denkweise statt OOP-Denkweise](04-go-denkweise.md) · [Übersicht](../README.md) · [Teil 06: Concurrency](06-concurrency.md) →
