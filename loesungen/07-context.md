# Lösungen zu Teil 07: Context richtig verstehen

Zu den Aufgaben: [Teil 07](../kapitel/07-context.md#übungen)

Alle vier Übungen haben einen Patch; die Patches für Übung 2 und 4 sind zusätzlich mit `go test -race` geprüft. Die Ausgaben stammen aus echten Läufen auf einem Apple-M-Rechner mit Go 1.27.1. Für Übung 3 lauschten upwatch und die langsame Website auf den Ports `18251` und `18250` statt auf `8090` und `18101`; bei dir gelten die Ports aus dem Kapitel.

## Übung 1: Timeout gegen Cancel (Aufwärmen)

Der Patch legt das Programm als neues Beispiel `beispiele/07-context/abbruch` an. Beide Varianten teilen sich die Warte-Funktion:

```go
// wait blocks until ctx ends or a second has passed and returns ctx.Err().
func wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
	}
	return ctx.Err()
}

func withTimeout() error {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	return wait(ctx)
}

func withCancel() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	return wait(ctx)
}
```

```bash
git apply loesungen/07-context/uebung-1.patch
go run ./beispiele/07-context/abbruch
go test ./beispiele/07-context/abbruch
```

```
WithTimeout: context deadline exceeded after 51ms
WithCancel:  context canceled after 11ms
```

Die beiden Fehlerwerte sind `context.DeadlineExceeded` („context deadline exceeded“) und `context.Canceled` („context canceled“). In beiden Fällen gewinnt `<-ctx.Done()` das `select`, die Sekunde aus `time.After` wird nie erreicht. Der Test im Patch prüft beide Werte mit `errors.Is` und läuft in einer `synctest`-Bubble, sodass die Dauern dort exakt 50 und 10 ms sind statt 51 und 11.

**Warum so:** Die Aufgabe verlangt kein Repo-Beispiel, aber als eigenes Paket unter `beispiele/` ist das Programm mit einem Befehl startbar und durch den Test gegen spätere Änderungen gesichert. `wait` gibt `ctx.Err()` auch dann zurück, wenn die Sekunde zuerst abläuft – dann ist der Wert `nil`, was genau die richtige Aussage ist. Wichtig ist das `defer cancel()` auch in der Cancel-Variante: Es ist dort der zweite Aufruf und harmlos, denn `cancel` darf beliebig oft aufgerufen werden, und es sorgt dafür, dass der Context auch dann endet, wenn die Goroutine einmal nicht feuert. Die Unterscheidung der beiden Werte ist die Grundlage für Übung 2: Ein abgelaufener Timeout und ein ausdrücklicher Abbruch sind zwei verschiedene Ereignisse, aber `DeadlineExceeded` allein sagt nicht, *wessen* Deadline abgelaufen ist.

## Übung 2: Die Deadline des Aufrufers (Kern)

`Check` leitet mit `WithTimeout(ctx, c.timeout)` einen Kind-Context ab. Hat der Aufrufer selbst eine frühere Deadline gesetzt, erbt das Kind sie, denn ein Kind hat nie mehr Zeit als sein Elternteil. Läuft sie ab, liefert `ctx.Err()` im Kind `DeadlineExceeded` – nicht `Canceled`. `Check` prüft aber nur auf `Canceled`, hält den Fall also für eine langsame Seite und gibt ein Ergebnis mit `"timeout"` und `err == nil` zurück. Der Worker würde es speichern, und die Seite stünde als „down“ in der Datenbank, obwohl sie womöglich in 200 ms geantwortet hätte.

Der Test zeigt das mit einem Server, der nie antwortet, einem Checker-Timeout von einer Sekunde und einer Aufrufer-Deadline von 20 ms:

```go
func TestCheckCallerDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	c := New(NewClient(true), time.Second)
	res, err := c.Check(ctx, monitor.Target{URL: srv.URL})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
	if res.Err != "" {
		t.Errorf("res.Err = %q, want it empty: there is no outcome to record", res.Err)
	}
}
```

Gegen den unveränderten Code:

```
=== RUN   TestCheckCallerDeadline
    check_test.go:149: err = <nil>, want context.DeadlineExceeded
    check_test.go:152: res.Err = "timeout", want it empty: there is no outcome to record
--- FAIL: TestCheckCallerDeadline (0.02s)
```

Für die Unterscheidung gibt es zwei Wege. Mit `ctx.Deadline()` könnte `Check` vor dem Ableiten nachsehen, ob der Aufrufer eine Deadline hat, die vor `time.Now().Add(c.timeout)` liegt, und sich das merken. Das ist ein Zeitvergleich mit Randfällen (gleiche Deadline, Uhr läuft zwischen Prüfung und Ablauf weiter). Präziser ist `WithTimeoutCause`: Der eigene Timeout bekommt eine eigene Ursache, und nach dem Fehler fragt `Check`, ob es *diese* Ursache war. Der Patch setzt das um:

```go
// errCheckTimeout is the cause of the checker's own timeout. It tells that
// timeout apart from a deadline the caller set on ctx.
var errCheckTimeout = errors.New("check timeout")

func (c *Checker) Check(ctx context.Context, target monitor.Target) (monitor.Result, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, c.timeout, errCheckTimeout)
	defer cancel()
	// ...
	if err != nil {
		// Distinguish "the caller gave up" from "the site is slow": only
		// our own timeout carries errCheckTimeout as its cause.
		if ctx.Err() != nil && !errors.Is(context.Cause(ctx), errCheckTimeout) {
			return res, fmt.Errorf("check %s: %w", target.URL, ctx.Err())
		}
		res.Err = describe(err)
		return res, nil
	}
```

Beim ersten Versuch schlug danach der bestehende Test „slow server times out“ fehl:

```
check_test.go:71: Err = "Get \"http://127.0.0.1:49292\": check timeout", want "timeout"
```

`net/http` meldet beim Abbruch die *Ursache* des Context, nicht `ctx.Err()`. Der Fehler von `client.Do` enthält jetzt `errCheckTimeout` statt `context.DeadlineExceeded`, und `describe` erkannte ihn nicht mehr. `describe` prüft deshalb beide:

```go
if errors.Is(err, errCheckTimeout) || errors.Is(err, context.DeadlineExceeded) {
	return "timeout"
}
```

```bash
git apply loesungen/07-context/uebung-2.patch
go test -race -v ./internal/check
```

```
--- PASS: TestCheck (0.07s)
--- PASS: TestCheckCanceledByCaller (0.02s)
--- PASS: TestForbiddenAddresses (0.00s)
--- PASS: TestIsPublic (0.00s)
--- PASS: TestCheckCallerDeadline (0.02s)
ok  	github.com/casoon/go-tutorial/internal/check	1.455s
```

**Warum so:** Die Frage „wer hat aufgegeben?“ lässt sich nur am Grund des Abbruchs beantworten, und den transportiert seit Go 1.21 `WithTimeoutCause`. Die Bedingung ist absichtlich negativ formuliert („alles, was nicht unser Timeout ist“): Damit fallen Abbruch, fremde Deadline und eine fremde Ursache aus `WithCancelCause` in denselben Zweig, und der Fehler ist weiterhin `ctx.Err()`, sodass `errors.Is(err, context.Canceled)` in `TestCheckCanceledByCaller` und im Scheduler unverändert funktioniert. In upwatch setzt heute kein Aufrufer eine Deadline – der Scheduler reicht den Signal-Context durch –, der Fehler ist also latent. Er würde auftreten, sobald jemand etwa einen „jetzt prüfen“-Endpunkt baut, der `Check` mit dem Request-Context eines ungeduldigen Clients aufruft. Der unerwartete Fehlschlag von `describe` ist die zweite Lehre: Wer Ursachen einführt, ändert, was Bibliotheken als Fehler melden, und sollte alle Stellen prüfen, die auf `DeadlineExceeded` vergleichen.

## Übung 3: `WithCancelCause` statt `NotifyContext` (Kern)

```go
// ctx is canceled on Ctrl+C or SIGTERM (sent by systemd, Docker, k8s).
// The cause names the signal, so the shutdown log says why.
ctx, cancel := context.WithCancelCause(context.Background())
defer cancel(nil)

sigs := make(chan os.Signal, 1)
signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
defer signal.Stop(sigs)
go func() {
	select {
	case sig := <-sigs:
		cancel(fmt.Errorf("received %v", sig))
	case <-ctx.Done(): // run returned for another reason
	}
}()
```

Dazu drei kleinere Änderungen: Im Zweig für einen Serverfehler wird aus `stop()` ein `cancel(fmt.Errorf("http server: %w", err))`, die Log-Zeile wird zu `logger.Info("shutting down", "cause", context.Cause(ctx))`, und das `cancel` des Shutdown-Timeouts heißt jetzt `cancelShutdown`, weil `cancel` schon vergeben ist.

```bash
git apply loesungen/07-context/uebung-3.patch
```

Ausprobiert mit dem Aufbau aus „Jetzt du“ (langsame Website mit 20 s Verzögerung, `UPWATCH_CHECK_TIMEOUT=30s`), einmal mit Ctrl+C (`SIGINT`), einmal mit `kill -TERM`:

```text
level=INFO msg=request method=POST path=/targets status=201 duration=491.5µs
level=INFO msg="shutting down" cause="received interrupt"
level=INFO msg=stopped
```

```text
level=INFO msg="shutting down" cause="received terminated"
level=INFO msg=stopped
```

Die langsame Website bestätigt beide Male den Abbruch:

```text
/slow canceled by client after 2.741s
/slow canceled by client after 1.007s
```

„terminated“ ist der Text, den Go für `SIGTERM` ausgibt (`syscall.SIGTERM.String()`), „interrupt“ der für `os.Interrupt`.

Ein Befund beim Vergleich: Die Umstellung ist in der aktuellen Go-Version gar nicht nötig, um die Ursache zu loggen. Laut Dokumentation von `signal.NotifyContext` (`go doc os/signal.NotifyContext` mit Go 1.27) liefert `context.Cause` auf dem Context bereits einen Fehler, der das auslösende Signal beschreibt. Mit dem unveränderten `NotifyContext` und nur der geänderten Log-Zeile ergibt sich:

```text
level=INFO msg="shutting down" cause="interrupt signal received"
level=INFO msg="shutting down" cause="terminated signal received"
```

**Warum so:** Die selbstgebaute Variante zeigt, was `NotifyContext` intern tut, und kann die Ursache frei formulieren – etwa um auch den Serverfehler als Ursache zu setzen, den der `serveErr`-Zweig jetzt mitgibt. Die Goroutine braucht einen zweiten Ausgang über `<-ctx.Done()`: Endet `run` aus einem anderen Grund, beendet `defer cancel(nil)` sie, sonst bliebe sie für immer im `select` hängen (siehe Leaks in Teil 06). Der Channel hat Puffer 1, weil `signal.Notify` nicht blockierend sendet und ein Signal sonst verloren ginge, falls die Goroutine gerade nicht im `select` steht. `signal.Stop` entspricht dem `stop` von `NotifyContext`; danach führt ein weiteres Ctrl+C wieder zum sofortigen Beenden. Für upwatch selbst ist `NotifyContext` plus `context.Cause` beim Loggen die bessere Wahl: weniger Code, gleiche Information.

## Übung 4: Ein Fake-Store, der seinen Context ernst nimmt (Profi)

Der Fake-Store bekommt eine Speicherdauer und gibt auf, wenn sein Context endet – wie eine echte Datenbank, deren `ExecContext` bei einem Abbruch abbricht:

```go
type fakeStore struct {
	mu        sync.Mutex
	pending   []monitor.Target
	saved     []monitor.Result
	saveDelay time.Duration // how long an INSERT takes
}

func (f *fakeStore) SaveResult(ctx context.Context, r monitor.Result) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-time.After(f.saveDelay):
	case <-ctx.Done():
		return ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, r)
	return nil
}
```

Der Test: Der Check ist nach 100 ms fertig, das Speichern dauert 10 ms, der Abbruch kommt bei 105 ms – mitten im `INSERT`.

```go
func TestRunSavesResultFinishedBeforeCancel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &fakeStore{
			pending:   []monitor.Target{{ID: 1}},
			saveDelay: 10 * time.Millisecond,
		}
		ctx, cancel := context.WithCancel(t.Context())
		s := New(store, fakeChecker{}, discardLogger(), 1, time.Second)

		done := make(chan struct{})
		go func() {
			s.Run(ctx)
			close(done)
		}()

		time.Sleep(105 * time.Millisecond)
		cancel()
		<-done

		if got := store.savedCount(); got != 1 {
			t.Fatalf("saved = %d, want 1", got)
		}
	})
}
```

```bash
git apply loesungen/07-context/uebung-4.patch
go test -race -v ./internal/scheduler
```

```
--- PASS: TestRunChecksAllTargets (0.00s)
--- PASS: TestRunStopsOnCancel (0.00s)
--- PASS: TestRunSavesResultFinishedBeforeCancel (0.00s)
ok  	github.com/casoon/go-tutorial/internal/scheduler	1.219s
```

Auch 50 Wiederholungen mit `-race -count=50` laufen grün; `synctest` macht den Ablauf deterministisch. Dann der Vergleich: In `Scheduler.write` `context.WithTimeout(ctx, 5*time.Second)` statt `saveCtx`. Weil `saveCtx` dann unbenutzt ist, meldet der Compiler „declared and not used: saveCtx“; für das Experiment wird die Zeile zu `_ = context.WithoutCancel(ctx)`. Ergebnis:

```
--- PASS: TestRunChecksAllTargets (0.00s)
--- PASS: TestRunStopsOnCancel (0.00s)
    scheduler_test.go:155: saved = 0, want 1
--- FAIL: TestRunSavesResultFinishedBeforeCancel (0.00s)
```

Im Fake-Store landet nichts: Das `INSERT` läuft mit dem abgebrochenen Context, bricht bei 105 ms mit `context.Canceled` ab, und der Writer verbucht das als Speicherfehler (Zähler `saveFailures`, Log-Eintrag „save result“). Eine fertige, gültige Messung ist verloren. Mit `saveCtx` landet sie, obwohl der Abbruch während des Speicherns kam.

**Warum so:** Die Speicherdauer ist der Kern des Tests. Mit einem Fake, der sofort speichert und nur am Anfang `ctx.Err()` prüft, wären Check-Ende, Senden und Speichern alle im selben virtuellen Augenblick bei 100 ms passiert; ob der Abbruch davor oder danach kommt, hinge dann von der Reihenfolge ab, in der die Laufzeit gleichzeitig geweckte Goroutines ausführt – ein flackernder Test. Mit 10 ms Speicherdauer liegt der Abbruch eindeutig im Speichervorgang. Die vorgezogene Prüfung `ctx.Err()` vor dem `select` ist nötig, weil `select` bei zwei bereiten Fällen zufällig wählt: Mit `saveDelay` 0 und bereits abgebrochenem Context würde sonst manchmal trotzdem gespeichert. Die bestehenden Tests bleiben unverändert grün, weil `saveDelay` dort 0 ist. Der Vergleich zeigt, warum `WithoutCancel` im Writer steht: Das Ende des Writers ist über `close(results)` geregelt, nicht über den Context, und der eigene 5-Sekunden-Timeout begrenzt jedes einzelne `INSERT`.
