# Teil 07: Context richtig verstehen

Nach diesem Kapitel weißt du, wofür `context.Context` da ist, wie er durch alle Schichten einer Anwendung gereicht wird, welche Regeln dabei gelten und wie du Abbruch, Timeout und deren Ursache sauber auseinanderhältst.

## Das Ausgangsbeispiel

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
```

`context.Background()` ist der leere Wurzel-Context: kein Timeout, kein Abbruch, keine Werte. `WithTimeout` leitet davon einen neuen Context ab, der nach zwei Sekunden automatisch abgebrochen wird, und liefert zusätzlich eine Funktion `cancel`, mit der du ihn früher abbrechen kannst. `defer cancel()` sorgt dafür, dass das spätestens beim Verlassen der Funktion passiert.

Ein Context ist ein Interface mit vier Methoden:

```go
type Context interface {
	Deadline() (deadline time.Time, ok bool)
	Done() <-chan struct{}
	Err() error
	Value(key any) any
}
```

`Done()` liefert einen Channel, der geschlossen wird, wenn der Context endet – das ist das Signal aus Kapitel 6, auf das man mit `select` warten kann. `Err()` ist danach nicht mehr `nil`, sondern `context.Canceled` oder `context.DeadlineExceeded`. `Deadline()` verrät, bis wann noch Zeit ist, und `Value()` transportiert request-bezogene Metadaten.

Contexts bilden einen Baum. Jede `With…`-Funktion erzeugt ein Kind. Wird ein Context abgebrochen, sind alle seine Nachfahren ebenfalls abgebrochen; umgekehrt beeinflusst der Abbruch eines Kindes den Elternteil nicht. Ein Kind kann außerdem nie *mehr* Zeit haben als sein Elternteil: `WithTimeout(parent, time.Hour)` endet trotzdem, sobald `parent` endet.

## Warum es Context gibt

In Kapitel 6 hast du gesehen, dass man eine Goroutine nicht von außen beenden kann. Sie muss selbst merken, dass ihre Arbeit nicht mehr gebraucht wird, und aussteigen. Das ist in einem Server ständig der Fall: Der Client bricht den Request ab, der Prozess soll herunterfahren, eine Datenbankabfrage dauert länger als erlaubt. Context ist die standardisierte Form dieses Signals. Weil alle Bibliotheken – `net/http`, `database/sql`, gRPC, Cloud-SDKs – denselben Typ akzeptieren, kann ein Abbruch oben in `main` bis in einen Socket ganz unten durchgereicht werden, ohne dass eine Schicht dazwischen etwas Besonderes tun muss.

Wer aus C# kommt, kennt das Konzept als `CancellationToken`:

```csharp
using var cts = new CancellationTokenSource(TimeSpan.FromSeconds(2));
var resp = await client.GetAsync(url, cts.Token);
```

`CancellationTokenSource` entspricht dem Paar aus Context und `cancel`-Funktion, `Token` dem Context selbst: Wer den Token bekommt, kann auf Abbruch reagieren, aber ihn nicht auslösen. In TypeScript übernimmt `AbortController` mit `AbortSignal` dieselbe Rolle, und `AbortSignal.timeout(2000)` ist das Gegenstück zu `WithTimeout`.

Die Unterschiede liegen in der Konsequenz. In C# ist `CancellationToken` ein optionaler Parameter mit `default`, und viele Codebasen reichen ihn nur halbherzig durch. In Go hat sich eine Konvention durchgesetzt, die praktisch jede Bibliothek befolgt: Jede Funktion, die I/O macht oder länger laufen kann, nimmt `ctx context.Context` als ersten Parameter. Außerdem bündelt Go Abbruch, Deadline und Metadaten in einem Objekt, wo .NET dafür getrennte Mechanismen hat (Token, Timeout-Properties an Clients, `AsyncLocal` bzw. `Activity` für Metadaten).

## Der Weg durch upwatch

In upwatch gibt es zwei Ketten, über die ein Context von oben nach unten wandert.

### HTTP-Request bis zur Datenbank

Jeder eingehende Request bringt seinen eigenen Context mit. `net/http` bricht ihn ab, wenn die Verbindung zum Client abreißt oder `ServeHTTP` zurückkehrt. Der Handler reicht ihn an den Store weiter ([httpapi.go](../internal/httpapi/httpapi.go)):

```go
func (a *API) getTarget(w http.ResponseWriter, r *http.Request) {
	// ...
	t, err := a.store.Target(r.Context(), id)
```

Der Store gibt ihn an `database/sql` ([store.go](../internal/sqlitestore/store.go)):

```go
func (s *Store) Target(ctx context.Context, id int64) (monitor.Target, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, url, interval_seconds, created_at FROM targets WHERE id = ?`, id)
```

Schließt der Client die Verbindung, während die Abfrage läuft, wird sie abgebrochen und die Verbindung geht zurück in den Pool. Niemand arbeitet für eine Antwort, die keiner mehr liest.

Ein Handler kann den Context auch enger fassen. `readyz` gibt der Datenbank höchstens eine Sekunde, egal wie lange der Client zu warten bereit wäre:

```go
func (a *API) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := a.store.Ping(ctx); err != nil {
```

Ein Load Balancer, der `/readyz` abfragt, soll eine schnelle Antwort bekommen, auch wenn die Datenbank hängt.

### Signal bis zum HTTP-Check

Die zweite Kette beginnt in [main.go](../cmd/upwatch/main.go):

```go
// ctx is canceled on Ctrl+C or SIGTERM (sent by systemd, Docker, k8s).
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
```

`signal.NotifyContext` übersetzt Betriebssystemsignale in einen Context-Abbruch. Dieser Context geht an den Scheduler (`sched.Run(ctx)`), von dort an jeden Worker und an `checker.Check`. Der Checker leitet einen eigenen Timeout ab und übergibt das Ergebnis an den HTTP-Request ([check.go](../internal/check/check.go)):

```go
func (c *Checker) Check(ctx context.Context, target monitor.Target) (monitor.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	// ...
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
```

Als Baum gezeichnet:

```
Background
└── NotifyContext (SIGTERM)          main
    ├── ClaimDue(ctx)                scheduler.dispatch
    ├── WithTimeout(10s)             check.Check, one per check
    │   └── http request             DNS, connect, TLS, response
    └── WithoutCancel                scheduler.write
        └── WithTimeout(5s)          one per SaveResult
```

Ein `SIGTERM` schließt den `Done`-Channel ganz oben. Der Dispatcher steigt aus seinem `select` aus, laufende HTTP-Checks werden abgebrochen – mitten im TLS-Handshake, wenn es sein muss –, und die Worker kehren zurück. Kein Teil des Codes muss dafür wissen, dass es Signale gibt. Der Zweig unter `WithoutCancel` bleibt bewusst unberührt; dazu gleich mehr.

## Die Regeln

**`ctx` ist der erste Parameter und heißt `ctx`.** Das ist reine Konvention, aber sie ist so verbreitet, dass Abweichungen in Reviews auffallen. Alle Interfaces in upwatch folgen ihr, etwa `scheduler.Store`:

```go
type Store interface {
	ClaimDue(ctx context.Context, now time.Time, limit int) ([]monitor.Target, error)
	SaveResult(ctx context.Context, r monitor.Result) error
}
```

**Context nicht in Structs speichern.** Ein Context gehört zu einem Aufruf, nicht zu einem Objekt. Der `Scheduler` hat Felder für Store, Checker, Logger und Takt, aber keinen Context; der kommt über `Run(ctx)`. Ein gespeicherter Context verschleiert, welcher Aufruf eigentlich abgebrochen werden kann, und führt leicht dazu, dass der Context einer längst beendeten Operation weiterverwendet wird. Die bekannte Ausnahme ist `http.Request`, der seinen Context als Feld trägt – weil der Request selbst ein kurzlebiger Wert ist, der genau einen Aufruf repräsentiert.

**`cancel` immer aufrufen.** Jede `With…`-Funktion, die ein `cancel` zurückgibt, registriert das Kind beim Elternteil und startet bei `WithTimeout` einen Timer. Beides wird erst freigegeben, wenn `cancel` aufgerufen wird oder der Elternteil endet. Wird `cancel` vergessen und lebt der Elternteil lange – etwa der Context aus `main` –, sammeln sich Kinder an. `go vet` meldet vergessene `cancel`-Aufrufe (Analyzer `lostcancel`). Die Regel gilt auch, wenn der Timeout ohnehin gleich abläuft: `defer cancel()` direkt nach der Erzeugung, ohne Ausnahme. Im Writer des Schedulers steht `cancel()` ausnahmsweise ohne `defer` am Ende des Schleifenkörpers, weil `defer` erst am Funktionsende liefe und sich in einer langen Schleife stapeln würde.

**`context.Background()` nur ganz oben.** In `main`, in `init` und in Tests. Überall sonst bekommt eine Funktion ihren Context vom Aufrufer. `context.TODO()` ist dasselbe wie `Background()`, signalisiert aber: Hier fehlt noch ein Context, der durchgereicht werden müsste. In Tests liefert `t.Context()` seit Go 1.24 einen Context, der kurz vor dem Ende des Tests abgebrochen wird; die Tests in upwatch nutzen ihn durchgehend.

**Nie `nil` übergeben.** Auch wenn eine Funktion den Context scheinbar nicht braucht. Wenn du keinen hast, nimm `context.TODO()`.

**Werte nur für request-bezogene Metadaten.** `context.WithValue` ist verführerisch, weil man damit beliebige Dinge durch Schichten tunneln kann, ohne Signaturen zu ändern. Genau das ist das Problem: Abhängigkeiten werden unsichtbar und nicht mehr vom Compiler geprüft. Ein Logger, eine Datenbankverbindung oder eine Konfiguration gehören in Struct-Felder oder Parameter. In den Context gehören Daten, die zu genau diesem Request gehören und quer durch alle Schichten mitlaufen: Request-ID, Trace-ID, der authentifizierte Benutzer. Als Schlüssel verwendest du einen eigenen, nicht exportierten Typ, damit kein anderes Paket kollidieren kann ([kette/main.go](../beispiele/07-context/kette/main.go)):

```go
// requestIDKey is unexported, so no other package can collide with it.
type requestIDKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return "-"
}
```

Die beiden Funktionen sind die einzige Stelle, die den Schlüssel kennt. Der Rest des Codes ruft `RequestID(ctx)` auf und bekommt einen typisierten Wert.

## `Canceled` oder `DeadlineExceeded`

Ein abgebrochener Context kann zwei Gründe haben, und für upwatch macht das einen großen Unterschied. Läuft der Timeout des Checks ab, hat die überwachte Seite zu langsam geantwortet – ein gültiges Messergebnis, das gespeichert werden soll. Wird dagegen der Context des Aufrufers abgebrochen, fährt upwatch herunter, und das halbfertige Ergebnis sagt nichts über die Seite aus. `Check` unterscheidet das so:

```go
resp, err := c.client.Do(req)
res.Duration = time.Since(start)
if err != nil {
	// Distinguish "the caller gave up" from "the site is slow".
	if errors.Is(ctx.Err(), context.Canceled) {
		return res, fmt.Errorf("check %s: %w", target.URL, ctx.Err())
	}
	res.Err = describe(err)
	return res, nil
}
```

`ctx` ist hier der abgeleitete Context mit Timeout. Ist sein Elternteil abgebrochen worden, liefert `ctx.Err()` `context.Canceled`; ist der eigene Timeout abgelaufen, `context.DeadlineExceeded`. Im ersten Fall gibt `Check` einen Fehler zurück, und der Worker im Scheduler verwirft das Ergebnis. Im zweiten Fall erkennt `describe` per `errors.Is(err, context.DeadlineExceeded)`, dass der HTTP-Client wegen der Deadline aufgegeben hat, und schreibt „timeout“ ins Ergebnis. `client.Do` liefert dabei einen `*url.Error`, der den Context-Fehler einpackt – deshalb `errors.Is` und kein `==`.

Das Beispiel spielt alle Fälle gegen einen lokalen Testserver durch, der 200 ms für eine Antwort braucht:

```bash
go run ./beispiele/07-context/kette
```

```
[req-42] generous timeout: "status 200", err=<nil>
[req-42] tight timeout:    "timeout", err=<nil>
AfterFunc: parent canceled, cause: shutting down
[req-42] caller canceled:  "", err=check http://127.0.0.1:55431: context canceled
Err: context canceled | Cause: shutting down
WithoutCancel: Err=<nil>, request ID still "req-42"
```

Port und die Position der `AfterFunc`-Zeile können bei dir abweichen; die Funktion läuft in einer eigenen Goroutine.

## Die Ursache mitgeben: `WithCancelCause`

`ctx.Err()` kennt nur zwei Werte. Wer wissen will, *warum* abgebrochen wurde, kann seit Go 1.20 eine Ursache mitgeben:

```go
var ErrShutdown = errors.New("shutting down")

ctx, cancel := context.WithCancelCause(parent)
// ...
cancel(ErrShutdown)

ctx.Err()           // context.Canceled – unchanged, for compatibility
context.Cause(ctx)  // ErrShutdown
```

`Err()` bleibt bewusst bei `context.Canceled`, damit bestehender Code, der darauf prüft, weiter funktioniert. `context.Cause` liefert die eigentliche Ursache; ohne angegebene Ursache ist sie gleich `Err()`. Für Timeouts gibt es seit Go 1.21 `WithTimeoutCause` und `WithDeadlineCause`. Nützlich ist das vor allem fürs Logging: „context canceled“ in einem Log sagt wenig, „shutting down“ oder „client disconnected“ sehr viel.

## Weiterarbeiten nach dem Abbruch: `WithoutCancel`

Manchmal muss Arbeit zu Ende geführt werden, obwohl der auslösende Context abgebrochen ist. Im Scheduler betrifft das den Writer: Beim Shutdown liegen womöglich noch Ergebnisse von Checks im Channel, die vor dem Signal fertig geworden sind. Würde der Writer `ctx` direkt an `SaveResult` geben, schlüge jedes dieser `INSERT`s sofort mit „context canceled“ fehl, und die Messungen gingen verloren. Deshalb ([scheduler.go](../internal/scheduler/scheduler.go)):

```go
func (s *Scheduler) write(ctx context.Context, results <-chan monitor.Result) {
	// Results that arrive during shutdown should still be saved, so the
	// writer uses a context that is not canceled together with ctx.
	saveCtx := context.WithoutCancel(ctx)

	for r := range results {
		// ...
		ctx, cancel := context.WithTimeout(saveCtx, 5*time.Second)
		if err := s.store.SaveResult(ctx, r); err != nil {
			// ...
		}
		cancel()
	}
}
```

`WithoutCancel` (seit Go 1.21) erzeugt einen Context, der die Werte des Elternteils behält, aber dessen Abbruch und Deadline nicht erbt. Gegenüber einem frischen `context.Background()` hat das einen Vorteil: Request-IDs, Trace-Informationen und andere Metadaten bleiben erhalten, Logs und Traces der Nacharbeit lassen sich also noch dem auslösenden Vorgang zuordnen. Das zeigt die letzte Zeile der Beispielausgabe.

Ein Context ohne Abbruch ist allerdings ein Context ohne Grenze. Deshalb leitet der Writer für jeden Speichervorgang wieder einen Timeout von fünf Sekunden ab. Und `WithoutCancel` ist kein Freibrief: Der Writer endet trotzdem, nämlich wenn `results` geschlossen wird – das Ende ist über die Channel-Struktur geregelt, nicht über den Context.

`main` geht beim HTTP-Server einen ähnlichen Weg auf andere Art: `srv.Shutdown` bekommt einen frischen Context mit 15 Sekunden Timeout, abgeleitet von `context.Background()`, weil der Signal-Context zu diesem Zeitpunkt bereits abgebrochen ist und Shutdown sonst sofort aufgeben würde. Werte gibt es dort keine zu erhalten.

## Auf Abbruch reagieren: `AfterFunc`

`context.AfterFunc` (seit Go 1.21) registriert eine Funktion, die in einer eigenen Goroutine läuft, sobald der Context endet:

```go
stop := context.AfterFunc(ctx, func() {
	conn.Close() // unblocks a Read that knows nothing about contexts
})
defer stop()
```

Das ist die Brücke zu APIs, die keinen Context kennen, aber sich durch Schließen oder ein Signal unterbrechen lassen. `stop` hebt die Registrierung auf, wenn die Arbeit regulär fertig wurde. Im Beispiel gibt die Funktion nur die Ursache aus; in upwatch wird `AfterFunc` nicht gebraucht, weil alle verwendeten APIs Contexts direkt akzeptieren.

## Context in eigenem Code respektieren

Bibliotheken wie `net/http` und `database/sql` reagieren selbstständig auf den Context. In eigenem Code, der wartet oder lange rechnet, musst du das selbst tun. Wartest du auf einen Channel oder Timer, gehört `ctx.Done()` in dasselbe `select` – wie im Dispatcher des Schedulers oder im Fake-Checker der Tests:

```go
func (fakeChecker) Check(ctx context.Context, t monitor.Target) (monitor.Result, error) {
	select {
	case <-time.After(100 * time.Millisecond):
		return monitor.Result{TargetID: t.ID, StatusCode: 200}, nil
	case <-ctx.Done():
		return monitor.Result{}, ctx.Err()
	}
}
```

In einer Schleife, die rechnet statt wartet, prüfst du in sinnvollen Abständen `if err := ctx.Err(); err != nil { return err }`. Context ist kooperativ: Er sagt nur Bescheid. Aufhören muss der Code selbst.

## Im Projekt

- [main.go](../cmd/upwatch/main.go): `signal.NotifyContext` als Wurzel für den Hintergrundbetrieb, eigener Context für `srv.Shutdown`.
- [scheduler.go](../internal/scheduler/scheduler.go): `ctx` als Parameter von `Run`, nicht als Feld; `select` auf `ctx.Done()` im Dispatcher; `WithoutCancel` plus Timeout im Writer.
- [check.go](../internal/check/check.go): `WithTimeout` pro Check, `NewRequestWithContext`, Unterscheidung von `Canceled` und `DeadlineExceeded`.
- [httpapi.go](../internal/httpapi/httpapi.go): `r.Context()` in jedem Handler, eigener kurzer Timeout in `readyz`.
- [store.go](../internal/sqlitestore/store.go): ausschließlich die `…Context`-Varianten von `database/sql`, auch in Transaktionen.
- [scheduler_test.go](../internal/scheduler/scheduler_test.go): `t.Context()` und ein Fake, der auf `ctx.Done()` reagiert.

## Übungen

1. Der `fakeStore` in `scheduler_test.go` ignoriert seinen Context. Lass `SaveResult` dort `ctx.Err()` zurückgeben, wenn der Context abgebrochen ist, und schreibe einen `synctest`-Test, in dem ein Check kurz vor dem Abbruch fertig wird. Gib dann in `Scheduler.write` testweise `ctx` statt `saveCtx` an `WithTimeout` und vergleiche, was im Fake-Store landet.
2. `Check` behandelt jedes `DeadlineExceeded` als langsame Seite. Was passiert, wenn der Aufrufer selbst eine Deadline setzt, die vor `c.timeout` abläuft? Schreibe einen Test mit `httptest.Server`, der das zeigt, und überlege, wie man den Fall mit `ctx.Deadline()` oder `WithTimeoutCause` unterscheiden könnte.
3. Füge upwatch eine Middleware hinzu, die jedem Request eine Request-ID in den Context legt (aus dem Header `X-Request-ID` oder zufällig erzeugt) und sie in `logRequests` mitloggt. Halte dich an die Regeln zu Schlüsseltyp und Zugriffsfunktionen.
4. Ersetze in `main.go` `signal.NotifyContext` testweise durch `context.WithCancelCause` und einen eigenen `signal.Notify`-Channel, der beim Signal `cancel(fmt.Errorf("received %v", sig))` aufruft. Logge beim Shutdown `context.Cause(ctx)`.

---

← [Teil 06: Concurrency](06-concurrency.md) · [Übersicht](../README.md) · [Teil 08: Eine HTTP-API mit net/http](08-http-api.md) →
