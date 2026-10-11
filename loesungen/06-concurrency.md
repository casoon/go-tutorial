# Lösungen zu Teil 06: Concurrency

Zu den Aufgaben: [Teil 06](../kapitel/06-concurrency.md#übungen)

Übung 1 ist ein Experiment ohne Patch, die anderen drei haben je einen Patch. Weil es um nebenläufigen Code geht, sind alle Patches zusätzlich mit `go test -race` geprüft. Die Ausgaben stammen aus echten Läufen auf einem Apple-M-Rechner mit Go 1.27.1; Laufzeiten und Reihenfolgen sind bei dir andere, weil `fakeCheck` zufällige Dauern würfelt.

## Übung 1: 1, 12 und 24 Worker (Aufwärmen)

Geändert wird in `main` der Aufruf `Run(context.Background(), urls, 4, fakeCheck)`. Achtung: Die letzte `Printf`-Zeile nennt die Zahl der Worker fest im Text („with 4 workers“); die musst du mit ändern, sonst lügt die Ausgabe. Je drei Läufe, nur die letzte Zeile:

```
12 checks with 4 workers in 330ms
12 checks with 4 workers in 370ms
12 checks with 4 workers in 310ms
12 checks with 1 workers in 1s
12 checks with 1 workers in 1.2s
12 checks with 1 workers in 1.14s
12 checks with 12 workers in 130ms
12 checks with 12 workers in 150ms
12 checks with 12 workers in 150ms
12 checks with 24 workers in 140ms
12 checks with 24 workers in 140ms
12 checks with 24 workers in 150ms
```

**Ein Worker** arbeitet die zwölf Checks nacheinander ab. Die Laufzeit ist die Summe aller Einzeldauern; jede liegt zwischen 50 und 150 ms, im Mittel bei 100 ms, also landet die Summe um 1,2 s und schwankt mit dem Zufall. Ein Lauf mit aufsummierten `Took`-Werten zeigt es direkt: 1285 ms Summe, „in 1.29s“ gemessen. Die Ausgabe kommt dabei in Eingabereihenfolge, weil es nichts zu überholen gibt.

**Zwölf Worker** starten alle Checks gleichzeitig. Die Laufzeit ist jetzt das *Maximum* der Einzeldauern, nicht die Summe:

```
https://site01.example 200 51ms
https://site11.example 200 58ms
...
https://site06.example 200 127ms
https://site07.example 200 145ms
12 checks with 12 workers in 150ms
```

Der langsamste Check (145 ms) bestimmt das Ende. Bei zwölf Zufallswerten zwischen 50 und 150 ms liegt das Maximum fast immer knapp unter 150 ms, deshalb sind die drei Läufe so ähnlich. Die Ausgabe ist jetzt nach Dauer sortiert, denn jedes Ergebnis kommt an, sobald sein Check fertig ist.

**Vier Worker** liegen dazwischen: drei Runden, in denen jeweils der langsamste von vier Checks die Runde bestimmt – genauer gesagt nimmt sich jeder freie Worker sofort das nächste Ziel, sodass sich die Runden überlappen. Die Laufzeit liegt deshalb bei etwa einem Viertel der Summe plus einem Rest.

**24 Worker** bringen nichts mehr, weil es nur zwölf Jobs gibt. Zwölf Worker bekommen je eine URL, die anderen zwölf warten in `for u := range jobs`, bis `close(jobs)` sie beendet. Die Laufzeit ist wieder das Maximum der Einzeldauern. Mehr Parallelität als unabhängige Arbeit gibt es nicht.

**Warum so:** Das ist das Gesetz hinter jedem Worker-Pool: Die Laufzeit liegt zwischen „Summe geteilt durch Worker“ und „längster Einzeljob“, und mehr Worker verschieben sie nur bis zur unteren Grenze. Danach kosten zusätzliche Worker nur Goroutines (billig) und belasten die Gegenseite (nicht billig): Ein echter Pool mit 1000 Workern würde 1000 Verbindungen gleichzeitig öffnen. Genau deshalb hat upwatch eine konfigurierbare, kleine Zahl Worker (`UPWATCH_WORKERS`, Standard 8) und kein „eine Goroutine pro Ziel“. Dass die Messwerte auf 10 ms gerundet und verrauscht sind, spielt für die Größenordnungen keine Rolle; genaue Zahlen liefert der `synctest`-Test im Paket.

## Übung 2: `atomic.Int64` statt Mutex und Map (Kern)

```go
// Stats counts successful and failed results. It is safe for concurrent
// use, and its zero value is ready to use. Like a mutex, it must not be
// copied after first use; go vet reports copies.
type Stats struct {
	ok     atomic.Int64
	failed atomic.Int64
}

// Record adds one result. Status codes from 200 to 399 count as success,
// as in monitor.Result.Up.
func (s *Stats) Record(status int) {
	if status >= 200 && status < 400 {
		s.ok.Add(1)
		return
	}
	s.failed.Add(1)
}

func (s *Stats) Successes() int64 { return s.ok.Load() }
func (s *Stats) Failures() int64  { return s.failed.Load() }
```

`NewStats` entfällt, `main` und der Test benutzen `var stats Stats`. Statt `Count(status)` gibt es `Successes()` und `Failures()`, denn nach einem einzelnen Statuscode kann man nicht mehr fragen.

```bash
git apply loesungen/06-concurrency/uebung-2.patch
go test -race -v ./beispiele/06-concurrency/race
go run ./beispiele/06-concurrency/race
```

```
=== RUN   TestStatsConcurrent
--- PASS: TestStatsConcurrent (0.00s)
PASS
ok  	github.com/casoon/go-tutorial/beispiele/06-concurrency/race	1.261s
ok: 900 failed: 100
```

Zur Gegenprobe: Mit zwei gewöhnlichen `int64`-Feldern und `s.ok++` meldet derselbe Test sofort `WARNING: DATA RACE` in `(*Stats).Record`.

Was du gewinnst: Kein Lock, also kein Warten, wenn viele Goroutines gleichzeitig zählen; eine atomare Addition ist eine einzelne CPU-Instruktion statt Lock, Map-Zugriff, Unlock. Der Nullwert ist sofort benutzbar, ein Konstruktor ist überflüssig. Und der Typ ist kleiner und offensichtlicher – es gibt keine Map, die man vergessen kann zu initialisieren.

Was du verlierst: Die Auflösung nach Statuscode. Ob die Fehler 503 oder 404 waren, ist weg; für jeden weiteren Zähler brauchst du ein weiteres Feld, beliebige Schlüssel gehen nicht. Und die beiden Zähler sind nur *einzeln* atomar. Wer während des Zählens `Successes()` und dann `Failures()` liest, kann zwischen den beiden Aufrufen ein weiteres `Record` verpassen oder doppelt sehen; eine Fehlerquote aus beiden Werten ist also nur eine Näherung. Mit dem Mutex konnte eine Methode beide Werte unter demselben Lock lesen und bekam einen konsistenten Schnappschuss.

**Warum so:** Atomics sind die richtige Wahl für einzelne, voneinander unabhängige Zähler, die oft geschrieben und selten gelesen werden – genau so arbeiten die expvar-Metriken des Schedulers (`expvar.Int` benutzt intern ebenfalls atomare Operationen). Sobald mehrere Werte gemeinsam konsistent sein müssen oder die Schlüssel erst zur Laufzeit feststehen, ist der Mutex einfacher und richtig. Die Grenze 200–399 übernimmt bewusst die Definition aus `monitor.Result.Up`, damit „Erfolg“ im ganzen Projekt dasselbe heißt. `atomic.Int64` statt `atomic.AddInt64(&s.ok, 1)` auf einem nackten `int64` ist die modernere Form: Der Typ erzwingt, dass niemand versehentlich ohne Atomic darauf zugreift, und `go vet` meldet Kopien.

## Übung 3: `First` mit `done`-Channel (Kern)

```go
// First returns the first answer as well. Once it has received, it closes
// done; every other goroutine then takes the done case of its select
// instead of blocking on the send, and exits.
func First(urls []string, fetch FetchFunc) string {
	ch := make(chan string)
	done := make(chan struct{})
	defer close(done)
	for _, u := range urls {
		go func() {
			res := fetch(u)
			select {
			case ch <- res:
			case <-done:
			}
		}()
	}
	return <-ch
}
```

```bash
git apply loesungen/06-concurrency/uebung-3.patch
go test -race -v ./beispiele/06-concurrency/leak
go run ./beispiele/06-concurrency/leak
```

```
=== RUN   TestFirstDoesNotLeak
--- PASS: TestFirstDoesNotLeak (0.00s)
PASS
ok  	github.com/casoon/go-tutorial/beispiele/06-concurrency/leak	1.180s
FirstLeaky: goroutines left over: 200
First:      goroutines left over: 0
```

Der Ablauf: Die schnellste Goroutine findet einen Empfänger und liefert ab. `First` kehrt zurück, und das `defer` schließt `done`. Die übrigen Goroutines stehen entweder schon im `select` und werden durch das Schließen geweckt, oder sie kommen später dort an und finden `done` bereits geschlossen. In beiden Fällen ist `<-done` bereit, `ch <- res` nie mehr – sie steigen aus.

Die Gegenprobe zeigt, dass der Test wirklich etwas prüft. Ohne die Zeile `defer close(done)` schlägt er mit dem Leak-Befund aus dem Kapitel fehl, nur steht die Goroutine jetzt im `select` statt im Send:

```
--- FAIL: TestFirstDoesNotLeak (0.00s)
panic: deadlock: main bubble goroutine has exited but blocked goroutines remain [recovered, repanicked]
...
goroutine 8 [select (durable), synctest bubble 1]:
github.com/casoon/go-tutorial/beispiele/06-concurrency/leak.First.func1()
	.../beispiele/06-concurrency/leak/main.go:33 +0x78
```

**Warum so:** Das `fetch(u)` steht absichtlich vor dem `select` in einer eigenen Zeile. In `case ch <- fetch(u):` würde Go den Ausdruck zwar auch zuerst auswerten, bevor das `select` wartet – das ist aber eine Regel, die man kennen muss, und in der eigenen Zeile sieht jeder Leser sofort, dass der Fetch auf jeden Fall vollständig läuft. Genau das ist auch die Schwäche dieser Lösung gegenüber der Context-Variante aus dem Kapitel: Die langsamen Fetches werden nicht abgebrochen, sie laufen zu Ende und werfen ihr Ergebnis dann weg. Für echte HTTP-Requests wäre ein Context, den `First` nach der ersten Antwort abbricht, besser, weil er auch Verbindungen und Server-Arbeit spart; `done` ist im Grunde ein selbstgebauter `ctx.Done()`. Gegenüber dem Puffer `len(urls)` hat `done` den Vorteil, dass es auch funktioniert, wenn die Zahl der Sender vorher nicht feststeht. Das `close` gehört in ein `defer`, damit es auch dann passiert, wenn die Funktion später einmal einen weiteren Ausstieg bekommt.

## Übung 4: `workers.Wait()` und `close(results)` vertauschen (Profi)

Nach dem Vertauschen sieht das Ende von `Run` so aus:

```go
close(jobs)
close(results) // too early
workers.Wait()
<-writerDone
```

Die bestehenden Tests bleiben grün:

```
ok  	github.com/casoon/go-tutorial/internal/scheduler	1.269s
```

Der Grund liegt im Fake-Checker: Er respektiert den Context. In `TestRunStopsOnCancel` sind beim `cancel()` beide Checks unterwegs; `fakeChecker.Check` wacht über `<-ctx.Done()` auf und gibt einen Fehler zurück, und der Worker überspringt mit `continue` das Senden. In `TestRunChecksAllTargets` sind beim `cancel()` alle zehn Checks längst fertig, die Worker warten leer in `range jobs`. In keinem Test gibt es also einen Worker, der nach dem Abbruch noch auf `results` sendet – und nur dann knallt es. Die Tests prüfen das Verhalten eines höflichen Checkers, nicht die Reihenfolge im Shutdown.

Den unhöflichen Fall liefert ein Checker, der `ctx` ignoriert, wie eine Prüfung auf Basis einer API ohne Context-Unterstützung:

```go
// stubbornChecker ignores ctx, like a check built on an API that knows
// nothing about contexts: it always takes 100ms and always delivers.
type stubbornChecker struct{}

func (stubbornChecker) Check(_ context.Context, t monitor.Target) (monitor.Result, error) {
	time.Sleep(100 * time.Millisecond)
	return monitor.Result{TargetID: t.ID, StatusCode: 200}, nil
}

func TestRunWaitsForWorkersBeforeClosingResults(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := &fakeStore{pending: []monitor.Target{{ID: 1}}}
		ctx, cancel := context.WithCancel(t.Context())
		s := New(store, stubbornChecker{}, discardLogger(), 1, time.Second)

		done := make(chan struct{})
		go func() {
			s.Run(ctx)
			close(done)
		}()

		time.Sleep(50 * time.Millisecond)
		cancel()
		<-done

		// The late result is still saved: the writer drains results
		// with a context that survives the cancel.
		if got := store.savedCount(); got != 1 {
			t.Fatalf("saved = %d, want 1", got)
		}
	})
}
```

Mit der vertauschten Reihenfolge passiert Folgendes: Bei 50 ms (virtueller Zeit) kommt der Abbruch, `dispatch` kehrt zurück, `jobs` und `results` werden geschlossen, und `Run` wartet in `workers.Wait()`. Bei 100 ms ist der Check fertig, und der Worker sendet auf den bereits geschlossenen Channel:

```
panic: send on closed channel [recovered, repanicked]
goroutine 12 [running, synctest bubble 1]:
...
github.com/casoon/go-tutorial/internal/scheduler.(*Scheduler).work(...)
	.../internal/scheduler/scheduler.go:124 +0xf8
github.com/casoon/go-tutorial/internal/scheduler.(*Scheduler).Run.func1()
	.../internal/scheduler/scheduler.go:69 +0x28
FAIL	github.com/casoon/go-tutorial/internal/scheduler	0.268s
```

Zeile 124 ist `results <- res` im Worker. Mit der richtigen Reihenfolge ist der Test grün, und er prüft dabei noch etwas: Das Ergebnis, das 50 ms nach dem Abbruch eintrifft, wird trotzdem gespeichert. Der Patch enthält nur den Fake und den Test, die Reihenfolge in `Run` bleibt die richtige.

```bash
git apply loesungen/06-concurrency/uebung-4.patch
go test -race -v ./internal/scheduler
```

**Warum so:** `synctest` macht den Test deterministisch – „Abbruch bei 50 ms, Ergebnis bei 100 ms“ passiert in jedem Lauf genau so, ohne echte Wartezeit und ohne Flakiness. Mit echter Zeit müsste man auf ein günstiges Timing hoffen, und ein solcher Test fällt im CI einmal pro Woche um. Ein einzelner Worker und ein einzelnes Ziel reichen; mehr Komplexität würde nur verschleiern, worum es geht. Der Test ist bewusst als dauerhafter Regressionstest formuliert („wartet `Run` auf die Worker?“) statt als Test, der eine Panik erwartet: So bleibt er grün, solange der Code richtig ist, und schlägt laut fehl, sobald jemand die Zeilen wieder vertauscht. Die eigentliche Lehre steckt im ersten Teil der Aufgabe: Eine Shutdown-Reihenfolge ist erst getestet, wenn ein Test den ungünstigsten Fall erzwingt – hier einen Checker, der sich nicht an die Abbruch-Konvention hält.
