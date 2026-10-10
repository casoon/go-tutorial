# Teil 06: Concurrency

Nach diesem Kapitel kannst du Goroutines, Channels, `select`, `WaitGroup` und `Mutex` gezielt einsetzen, einen Worker-Pool mit sauberem Shutdown bauen und die zwei typischen Fehler – Leaks und Data Races – erkennen und vermeiden.

## `go doSomething()`

Nebenläufigkeit beginnt in Go mit einem Schlüsselwort:

```go
go check(target)
```

`check` läuft jetzt in einer eigenen Goroutine, und die aufrufende Funktion macht sofort mit der nächsten Zeile weiter. Eine Goroutine ist kein Betriebssystem-Thread. Die Go-Laufzeit verteilt beliebig viele Goroutines auf wenige Threads (in der Regel so viele, wie `GOMAXPROCS` angibt), und jede Goroutine startet mit einem Stack von wenigen Kilobyte, der bei Bedarf wächst. Zehntausende gleichzeitige Goroutines sind deshalb in einem Server kein Problem; in `net/http` bekommt jede eingehende Verbindung ihre eigene.

Drei Eigenschaften überraschen Umsteiger:

- Eine Goroutine hat keinen Rückgabewert und kein Handle. Es gibt kein `Task<T>` und kein `Promise`, das man später abfragen könnte. Ergebnisse und Fehler musst du selbst zurückmelden, meist über einen Channel.
- Man kann eine Goroutine nicht von außen beenden. Sie endet, wenn ihre Funktion zurückkehrt. Wer sie stoppen will, muss ihr das mitteilen – über einen Channel oder einen Context (Kapitel 7).
- Wenn `main` zurückkehrt, endet das Programm, egal wie viele Goroutines noch laufen. Es wird auf niemanden gewartet.

Das folgende Programm gibt deshalb wahrscheinlich gar nichts aus:

```go
func main() {
	go fmt.Println("checking https://example.com")
} // main returns, the process exits
```

Alles Weitere in diesem Kapitel dreht sich um zwei Fragen: Wie kommen Daten zwischen Goroutines hin und her, und woher weiß man, wann eine Goroutine fertig ist?

## Goroutines blockieren, Funktionen sind nicht gefärbt

Bevor es um Channels geht, lohnt ein Blick auf den grundlegenden Unterschied zu C# und TypeScript. Dort sieht ein HTTP-Check so aus:

```csharp
async Task<int> CheckAsync(string url, CancellationToken ct)
{
    var resp = await client.GetAsync(url, ct);
    return (int)resp.StatusCode;
}
```

`await` gibt den Thread frei, solange auf das Netzwerk gewartet wird. Der Preis: Die Funktion wird `async`, ihr Rückgabetyp wird `Task<int>`, und jeder Aufrufer, der nicht blockieren will, muss selbst `async` werden. Die Eigenschaft „asynchron“ breitet sich durch die Aufrufkette aus. Dafür hat sich der Begriff „Function Coloring“ eingebürgert: Es gibt rote (async) und blaue (sync) Funktionen, und sie lassen sich nicht frei mischen.

In Go gibt es diese Unterscheidung nicht. `client.Do(req)` *blockiert* die aufrufende Goroutine, bis die Antwort da ist. Die Laufzeit parkt die Goroutine, registriert den Socket beim Netpoller des Betriebssystems (epoll, kqueue, IOCP) und lässt den Thread in der Zwischenzeit andere Goroutines ausführen. Was C# mit `await` im Code markiert, erledigt Go unsichtbar im Scheduler.

Für den Code heißt das: Jede Funktion ist synchron geschrieben. `check.Checker.Check` in [check.go](../internal/check/check.go) ist eine gewöhnliche Funktion, die ein Ergebnis zurückgibt. Ob sie parallel läuft, entscheidet nicht sie selbst, sondern der Aufrufer – der Scheduler, der sie in mehreren Goroutines startet. Bibliotheken bieten deshalb in Go fast immer blockierende APIs an, und das ist richtig so: Nebenläufigkeit ist eine Entscheidung der Anwendung, nicht der Bibliothek.

Der Trade-off: In C# siehst du an jedem `await`, wo eine Funktion unterbrochen werden kann. In Go kann jede Goroutine zu jeder Zeit unterbrochen werden, und mehrere laufen echt parallel auf mehreren Kernen. Geteilte Daten brauchen deshalb immer Synchronisation – auch dort, wo in einer Single-Thread-Event-Loop wie Node.js nichts passieren könnte.

## Channels

Ein Channel ist eine typisierte Leitung zwischen Goroutines:

```go
results := make(chan monitor.Result) // unbuffered
results <- res                        // send
res := <-results                      // receive
```

Ein *ungepufferter* Channel hat keine Kapazität. Ein Send blockiert, bis eine andere Goroutine empfängt, und umgekehrt. Damit ist jede Übergabe gleichzeitig ein Synchronisationspunkt: Wenn `results <- res` zurückkehrt, weißt du, dass jemand das Ergebnis angenommen hat.

Ein *gepufferter* Channel hat eine feste Kapazität:

```go
serveErr := make(chan error, 1)
```

Sends blockieren erst, wenn der Puffer voll ist. Das entkoppelt Sender und Empfänger, kann aber auch Probleme verdecken: Ein Puffer von 1000 verschiebt einen Stau nur nach hinten. Gepufferte Channels mit Kapazität 1 sind ein häufiges Muster für „eine Goroutine meldet genau ein Ergebnis, und es ist nicht sicher, ob jemand es abholt“. Genau so nutzt [main.go](../cmd/upwatch/main.go) `serveErr`: Die Server-Goroutine kann ihren Fehler immer ablegen und sich beenden, auch wenn `main` gerade mit dem Shutdown beschäftigt ist und nicht mehr liest.

### Richtung im Typ

In Funktionssignaturen kannst du einschränken, was mit einem Channel erlaubt ist:

```go
func (s *Scheduler) dispatch(ctx context.Context, jobs chan<- monitor.Target)
func (s *Scheduler) work(ctx context.Context, jobs <-chan monitor.Target, results chan<- monitor.Result)
```

`chan<- T` darf nur senden, `<-chan T` nur empfangen. Ein bidirektionaler Channel wird beim Aufruf automatisch umgewandelt. Das ist Dokumentation, die der Compiler prüft: `dispatch` kann `jobs` nicht versehentlich lesen, `work` kann `jobs` nicht schließen.

### `close` und `range`

`close(ch)` signalisiert: Es kommen keine Werte mehr. Empfänger bekommen danach die restlichen gepufferten Werte und dann sofort den Nullwert; mit der Zwei-Werte-Form erkennst du das Ende:

```go
t, ok := <-jobs // ok == false: closed and drained
```

Üblicher ist `range`, das genau bis zum Schließen läuft:

```go
for t := range jobs {
	// handle t
}
// jobs was closed
```

Senden auf einen geschlossenen Channel panikt, doppeltes Schließen auch. Schließen ist also keine Aufräumarbeit wie bei einer Datei, sondern eine Nachricht an die Empfänger. Ein Channel, den niemand schließt, wird trotzdem vom Garbage Collector eingesammelt, sobald keiner ihn mehr referenziert. Schließen musst du nur, wenn ein Empfänger auf das Ende wartet.

## `select`

`select` wartet auf mehrere Channel-Operationen gleichzeitig und führt die erste aus, die bereit ist. Sind mehrere bereit, wird zufällig gewählt. Das wichtigste Muster ist „sende, aber gib auf, wenn abgebrochen wird“, hier aus dem Dispatcher des Schedulers:

```go
for _, t := range due {
	select {
	case jobs <- t:
	case <-ctx.Done():
		return
	}
}

select {
case <-ticker.C:
case <-ctx.Done():
	return
}
```

Ohne das `select` würde `jobs <- t` beim Shutdown ewig blockieren, wenn kein Worker mehr annimmt. Mit `default` wird `select` nicht-blockierend: Ist kein Fall bereit, läuft sofort der `default`-Zweig. Und ein `nil`-Channel blockiert für immer, wodurch man einzelne Fälle in einer `select`-Schleife gezielt abschalten kann, indem man die Variable auf `nil` setzt.

## `sync.WaitGroup`

Eine `WaitGroup` zählt laufende Goroutines und lässt eine andere warten, bis alle fertig sind. Die klassische Form mit `Add` und `Done`:

```go
var wg sync.WaitGroup
for _, t := range targets {
	wg.Add(1)
	go func() {
		defer wg.Done()
		check(t)
	}()
}
wg.Wait()
```

Seit Go 1.25 gibt es `WaitGroup.Go`, das beides zusammenfasst und den häufigsten Fehler – `Add` vergessen oder in der Goroutine statt davor aufrufen – unmöglich macht:

```go
var wg sync.WaitGroup
for _, t := range targets {
	wg.Go(func() { check(t) })
}
wg.Wait()
```

upwatch verwendet ausschließlich diese Form. Dass die Closure `t` direkt benutzen darf, liegt an einer Änderung in Go 1.22: Seitdem bekommt jede Schleifeniteration eine eigene Variable. Vorher teilten sich alle Goroutines dieselbe, und man sah überall `t := t` als Workaround. In älterem Code findest du das noch.

Die Dokumentation von `WaitGroup.Go` enthält einen Satz, der leicht überlesen wird: Die Funktion darf nicht paniken. Eine Panik in einer Goroutine beendet ohnehin das ganze Programm (Kapitel 5), aber `Go` ist nicht dafür gebaut, sie aufzufangen.

## `sync.Mutex`: wenn geteilter Zustand einfacher ist

Aus der Go-Welt stammt der Satz „Don't communicate by sharing memory; share memory by communicating.“ Er wird gern so verstanden, als seien Mutexe unidiomatisch. Das ist falsch. Channels sind gut, um *Besitz* weiterzugeben – ein Ziel geht vom Dispatcher an einen Worker, das Ergebnis vom Worker an den Writer – und um Abläufe zu koordinieren. Wenn mehrere Goroutines einen gemeinsamen *Zustand* lesen und ändern, ist ein Mutex meist klarer:

```go
// Stats counts results per status code. It is safe for concurrent use.
type Stats struct {
	mu     sync.Mutex
	counts map[int]int
}

func (s *Stats) Record(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counts[status]++
}
```

Konventionen: Der Mutex steht direkt über den Feldern, die er schützt, ist ein Wert (kein Zeiger) und wird nie kopiert – deshalb haben solche Typen Pointer-Receiver, und `go vet` warnt, wenn du einen `Mutex` kopierst. `Lock` und `defer Unlock` gehören zusammen. `sync.RWMutex` erlaubt beliebig viele gleichzeitige Leser oder einen Schreiber; das lohnt sich, wenn deutlich mehr gelesen als geschrieben wird, wie im In-Memory-Store in Kapitel 8. Für einzelne Zähler gibt es `sync/atomic` mit Typen wie `atomic.Int64`.

Im Test des Schedulers schützt ein Mutex die Fake-Datenbank, weil Dispatcher und Writer gleichzeitig darauf zugreifen ([scheduler_test.go](../internal/scheduler/scheduler_test.go)). Der echte SQLite-Store braucht keinen eigenen, denn `*sql.DB` ist bereits für gleichzeitige Nutzung gebaut.

## Der Worker-Pool im Scheduler

Jetzt kommt alles zusammen. Der Scheduler in [scheduler.go](../internal/scheduler/scheduler.go) prüft alle fälligen Ziele mit einer festen Zahl von Workern. Sein Paketkommentar zeigt den Datenfluss:

```
ticker ─► ClaimDue ─► jobs ─┬─► worker ─┐
                            ├─► worker ─┼─► results ─► writer ─► SaveResult
                            └─► worker ─┘
```

Links holt ein Dispatcher in jedem Takt die fälligen Ziele aus der Datenbank und schiebt sie in `jobs`. Mehrere Worker lesen aus demselben Channel – das ist *Fan-out*: Arbeit wird auf mehrere Goroutines verteilt, und wer gerade frei ist, nimmt sich das nächste Ziel. Alle Worker schreiben in denselben Channel `results`, aus dem genau ein Writer liest – das ist *Fan-in*.

```go
func (s *Scheduler) Run(ctx context.Context) {
	jobs := make(chan monitor.Target)
	results := make(chan monitor.Result)

	var workers sync.WaitGroup
	for range s.workers {
		workers.Go(func() { s.work(ctx, jobs, results) })
	}

	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		s.write(ctx, results)
	}()

	s.dispatch(ctx, jobs)

	// Shutdown order matters: the dispatcher has returned, so nobody sends
	// on jobs any more. Closing it ends the workers' range loops. Once all
	// workers are gone nobody sends on results, so it can be closed too.
	close(jobs)
	workers.Wait()
	close(results)
	<-writerDone
}
```

Der Worker ist kurz:

```go
func (s *Scheduler) work(ctx context.Context, jobs <-chan monitor.Target, results chan<- monitor.Result) {
	for t := range jobs {
		res, err := s.checker.Check(ctx, t)
		if err != nil {
			// Only happens when ctx was canceled: the check was cut short
			// by shutdown and its result would be misleading.
			continue
		}
		results <- res
	}
}
```

Einige Entscheidungen darin sind nicht offensichtlich.

**Warum ungepufferte Channels?** Sie erzeugen Gegendruck. Sind alle Worker beschäftigt, blockiert der Dispatcher beim Senden und holt keine neuen Ziele aus der Datenbank. `ClaimDue` fragt höchstens `s.workers*4` Ziele auf einmal ab, sodass nie mehr Arbeit in Bearbeitung ist, als in absehbarer Zeit erledigt werden kann. Ein großer Puffer würde Ziele aus der Datenbank „beanspruchen“, die dann im Speicher warten – und beim Shutdown verloren gingen.

**Warum ein einzelner Writer?** SQLite erlaubt immer nur einen Schreiber. Acht Worker, die gleichzeitig `SaveResult` aufrufen, würden sich um die Schreibsperre streiten. Der Writer serialisiert die Schreibzugriffe, und weil ein `INSERT` im Vergleich zu einem HTTP-Check sehr schnell ist, wird er nicht zum Engpass. Nebenbei bündelt er Logging und Metriken an einer Stelle.

**Warum `struct{}` für `writerDone`?** Der Channel transportiert keine Daten, nur das Ereignis „fertig“. `struct{}` belegt keinen Speicher und macht das im Typ deutlich. `close` ist dabei das Signal – es weckt jeden Empfänger auf, auch mehrere.

### Die Shutdown-Reihenfolge

Die vier Zeilen am Ende von `Run` sind der Kern des sauberen Herunterfahrens. Die Reihenfolge ergibt sich aus einer einzigen Regel: Ein Channel darf erst geschlossen werden, wenn garantiert niemand mehr darauf sendet.

1. `s.dispatch(ctx, jobs)` kehrt zurück, wenn `ctx` abgebrochen wird. Ab jetzt sendet niemand mehr auf `jobs`.
2. `close(jobs)` – die Worker beenden ihre `range`-Schleife, sobald sie ihren aktuellen Check abgeschlossen haben.
3. `workers.Wait()` – wartet, bis alle Worker zurückgekehrt sind. Danach sendet niemand mehr auf `results`.
4. `close(results)` – der Writer beendet seine `range`-Schleife, nachdem er alle bereits gelieferten Ergebnisse gespeichert hat.
5. `<-writerDone` – wartet, bis der Writer wirklich fertig ist. Erst dann kehrt `Run` zurück, und `main` darf die Datenbank schließen.

Vertauschst du zwei Schritte, gibt es Fehler. `close(results)` vor `workers.Wait()` führt zur Panik, wenn ein Worker noch ein Ergebnis senden will. Fehlt `<-writerDone`, schließt `main` womöglich die Datenbank, während der Writer noch schreibt. Fehlt `close(jobs)`, warten die Worker ewig und `workers.Wait()` kehrt nie zurück.

Dass der Writer während des Shutdowns überhaupt noch speichern kann, obwohl `ctx` bereits abgebrochen ist, liegt an `context.WithoutCancel` – das ist Thema in Kapitel 7.

### Dasselbe Muster, reduziert

[pool/main.go](../beispiele/06-concurrency/pool/main.go) zeigt die Struktur ohne Datenbank und echtes HTTP. Der simulierte Check dauert zwischen 50 und 150 ms:

```go
func Run(ctx context.Context, urls []string, workers int, check CheckFunc) []Result {
	jobs := make(chan string)
	results := make(chan Result)

	// Fan-out: several workers receive from the same channel.
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for u := range jobs {
				results <- check(ctx, u)
			}
		})
	}

	// Fan-in: one goroutine collects from the shared results channel.
	var out []Result
	collected := make(chan struct{})
	go func() {
		defer close(collected)
		for r := range results {
			out = append(out, r)
		}
	}()

	dispatch(ctx, urls, jobs)

	close(jobs)    // 1. no more jobs: workers leave their range loops
	wg.Wait()      // 2. all workers gone: nobody sends on results
	close(results) // 3. collector leaves its range loop
	<-collected    // 4. out is complete and no longer written to
	return out
}
```

```bash
go run ./beispiele/06-concurrency/pool
```

```
...
https://site10.example 200 99ms
https://site11.example 200 148ms
12 checks with 4 workers in 360ms
```

Die Laufzeit schwankt wegen der zufälligen Dauer. Der Test dazu ist trotzdem exakt, weil er in einer `testing/synctest`-Blase läuft (seit Go 1.25): Dort ist die Zeit virtuell und springt vorwärts, sobald alle Goroutines blockieren. Zehn Checks à 100 ms auf drei Workern brauchen genau vier Runden:

```go
synctest.Test(t, func(t *testing.T) {
	start := time.Now()
	got := Run(t.Context(), urls(10), 3, slowCheck)
	if len(got) != 10 {
		t.Fatalf("got %d results, want 10", len(got))
	}
	// 10 checks on 3 workers: 4 rounds of 100ms.
	if d := time.Since(start); d != 400*time.Millisecond {
		t.Errorf("took %s, want 400ms", d)
	}
})
```

Der echte Scheduler wird genauso getestet; Kapitel 11 geht darauf genauer ein.

## Wer sendet, schließt

Aus dem Worker-Pool lässt sich eine Faustregel ableiten, die fast alle Channel-Probleme verhindert: **Nur der Sender schließt einen Channel, nie der Empfänger.** Der Empfänger weiß nicht, ob noch etwas kommt; schließt er, panikt der nächste Send.

Bei einem einzelnen Sender ist das einfach: Er schließt, wenn er fertig ist, typischerweise mit `defer close(ch)`. Bei mehreren Sendern – den Workern, die alle auf `results` schreiben – darf keiner von ihnen schließen, denn keiner weiß, ob die anderen fertig sind. Dann schließt ein Koordinator, der auf alle Sender wartet. In `Run` ist das die Funktion selbst mit `workers.Wait()` gefolgt von `close(results)`.

Die zweite Hälfte der Regel: Jede Goroutine, die du startest, braucht ein klar definiertes Ende. Wenn du nicht in einem Satz sagen kannst, wann und warum eine Goroutine zurückkehrt, hast du vermutlich einen Leak.

## Goroutine-Leaks

Ein Goroutine-Leak entsteht, wenn eine Goroutine für immer blockiert – meist auf einem Send oder Receive, auf den nie jemand antwortet. Der Garbage Collector räumt blockierte Goroutines nicht auf. Sie belegen Speicher, halten Referenzen auf alles, was ihre Closure kennt, und in einem Server, der tagelang läuft, summiert sich das.

Ein klassischer Fall: mehrere Spiegelserver anfragen und die erste Antwort nehmen ([leak/main.go](../beispiele/06-concurrency/leak/main.go)).

```go
// FirstLeaky returns the first answer. The other goroutines block forever
// on the unbuffered channel, because nobody receives their result.
func FirstLeaky(urls []string, fetch FetchFunc) string {
	ch := make(chan string)
	for _, u := range urls {
		go func() { ch <- fetch(u) }()
	}
	return <-ch
}
```

Die Funktion liefert das richtige Ergebnis, und jeder Unit-Test, der nur den Rückgabewert prüft, ist grün. Aber nach dem ersten Empfang liest niemand mehr aus `ch`, und die übrigen Goroutines hängen für immer in `ch <- ...`. Der Fix ist hier ein Puffer, der für jede Antwort Platz hat:

```go
ch := make(chan string, len(urls))
```

Jetzt kann jede Goroutine ihr Ergebnis ablegen und sich beenden, auch wenn es niemand abholt. Alternativ kann jede Goroutine mit `select` auf einen Context warten, den der Aufrufer nach der ersten Antwort abbricht – das hat den Vorteil, dass auch die laufenden Requests abgebrochen werden.

```bash
go run ./beispiele/06-concurrency/leak
```

```
FirstLeaky: goroutines left over: 200
First:      goroutines left over: 0
```

100 Aufrufe mit drei Spiegeln, zwei verwaiste Goroutines pro Aufruf.

Leaks findest du in Tests am zuverlässigsten mit `synctest`: `synctest.Test` verlangt, dass alle in der Blase gestarteten Goroutines enden. Bleibt eine dauerhaft blockiert, schlägt der Test fehl, statt zu hängen. Ersetzt man im Test aus [leak/main_test.go](../beispiele/06-concurrency/leak/main_test.go) `First` durch `FirstLeaky`, sieht die Ausgabe so aus (gekürzt):

```
--- FAIL: TestFirstDoesNotLeak (0.00s)
panic: deadlock: main bubble goroutine has exited but blocked goroutines remain [recovered, repanicked]
...
goroutine 37 [chan send (durable), synctest bubble 1]:
github.com/casoon/go-tutorial/beispiele/06-concurrency/leak.FirstLeaky.func1()
	.../beispiele/06-concurrency/leak/main.go:19 +0x3c
```

Der Stacktrace zeigt genau die Zeile mit dem hängenden Send.

## Race Conditions und `go test -race`

Ein Data Race liegt vor, wenn zwei Goroutines gleichzeitig auf dieselbe Speicherstelle zugreifen, mindestens eine davon schreibt und keine Synchronisation dazwischen liegt. Das Ergebnis ist nicht nur „manchmal falsch“: Bei Strings, Slices und Interfaces kann ein Race inkonsistente Werte erzeugen, und gleichzeitige Schreibzugriffe auf eine Map lassen die Laufzeit oft mit „concurrent map writes“ abbrechen. [racy.go](../beispiele/06-concurrency/race/racy.go) zählt Checks ohne Schutz:

```go
var checks int
var wg sync.WaitGroup
for range 1000 {
	wg.Go(func() {
		checks++ // read, add, write: not atomic, not synchronized
	})
}
wg.Wait()
fmt.Println("checks:", checks)
```

Die Datei trägt `//go:build ignore`, damit sie nicht zum Paket gehört und `go test ./beispiele/...` grün bleibt. Gestartet wird sie direkt mit dem Race-Detector:

```bash
cd beispiele/06-concurrency/race
go run -race racy.go
```

```
==================
WARNING: DATA RACE
Read at 0x00c0001ae018 by goroutine 8:
  main.main.func1()
      .../beispiele/06-concurrency/race/racy.go:19 +0x2c

Previous write at 0x00c0001ae018 by goroutine 7:
  main.main.func1()
      .../beispiele/06-concurrency/race/racy.go:19 +0x3c
...
==================
checks: 835
Found 2 data race(s)
exit status 66
```

Zwei Dinge sind bemerkenswert. Der Zähler steht bei 835 statt 1000: Inkremente sind verloren gegangen, weil zwei Goroutines denselben alten Wert gelesen und beide `+1` zurückgeschrieben haben. Und der Detector nennt beide beteiligten Zugriffe mit Zeile – bei einem Race, das in Produktion einmal pro Woche auftritt, ist das Gold wert.

Der Race-Detector instrumentiert jeden Speicherzugriff und macht das Programm dadurch deutlich langsamer und speicherhungriger. Er gehört deshalb in Tests und CI, nicht in den Produktions-Build. upwatch führt alle Tests mit `-race` aus (`make test` im [Makefile](../Makefile)). Wichtig: Er findet nur Races, die während des Laufs tatsächlich auftreten. Ein Test, der den nebenläufigen Pfad nicht ausführt, beweist nichts. Deshalb startet der Test der korrekten Variante ([race/main_test.go](../beispiele/06-concurrency/race/main_test.go)) bewusst 100 Goroutines:

```bash
go test -race ./beispiele/06-concurrency/race/
```

Entfernst du dort den Mutex aus `Stats`, schlägt der Test fehl.

## Nicht alles parallelisieren

Goroutines sind billig, aber nicht kostenlos – und vor allem ist nebenläufiger Code schwerer zu lesen, zu testen und zu debuggen als sequenzieller. Ein paar Überlegungen, bevor du `go` schreibst:

**Wartet die Arbeit auf etwas?** Parallelität lohnt sich, wenn Goroutines die meiste Zeit auf I/O warten, wie HTTP-Checks. Drei Datenbankabfragen, die je eine Millisekunde dauern, parallel auszuführen, bringt dagegen kaum etwas und kostet Lesbarkeit. Bei CPU-Arbeit ist mehr als `GOMAXPROCS` parallel laufende Goroutines sinnlos.

**Verträgt die Gegenseite es?** Die Anzahl gleichzeitiger Zugriffe sollte immer begrenzt sein. upwatch begrenzt Checks über die Zahl der Worker (`UPWATCH_WORKERS`) und Schreibzugriffe über den einzelnen Writer. Für einmalige Fan-outs ohne festen Pool reicht ein gepufferter Channel als Semaphor:

```go
sem := make(chan struct{}, 4) // at most 4 at a time
for _, t := range targets {
	wg.Go(func() {
		sem <- struct{}{}
		defer func() { <-sem }()
		check(t)
	})
}
```

**Wie kommen Fehler zurück?** Sequenziell ist das ein `return err`. Parallel brauchst du einen Weg, Fehler zu sammeln und bei Bedarf die übrigen Goroutines abzubrechen. Das Modul `golang.org/x/sync/errgroup` (nicht Teil der Standardbibliothek, aber vom Go-Team gepflegt) bündelt `WaitGroup`, ersten Fehler und Context-Abbruch; upwatch braucht es nicht, weil Checks ihre Fehler im Ergebnis tragen.

**Spielt die Reihenfolge eine Rolle?** Ergebnisse paralleler Arbeit kommen in beliebiger Reihenfolge an. Wenn du sie geordnet brauchst, musst du sie nachträglich sortieren oder per Index in ein vorab angelegtes Slice schreiben.

Die Server-Grundstruktur ist in Go ohnehin schon nebenläufig: `net/http` startet für jede Verbindung eine Goroutine. Innerhalb eines Handlers sequenziell zu arbeiten, ist der Normalfall und meist richtig.

## Im Projekt

- [scheduler.go](../internal/scheduler/scheduler.go): Worker-Pool mit Fan-out und Fan-in, Channel-Richtungen in den Signaturen, `select` mit `ctx.Done()`, `WaitGroup.Go`, die Shutdown-Reihenfolge.
- [scheduler_test.go](../internal/scheduler/scheduler_test.go): `synctest` mit virtueller Zeit, ein Fake-Store mit Mutex.
- [main.go](../cmd/upwatch/main.go): `wg.Go` für den Scheduler, der gepufferte `serveErr`-Channel, `select` zwischen Serverfehler und Signal.
- [store.go](../internal/sqlitestore/store.go): `ClaimDue` beansprucht fällige Ziele in einer Transaktion, damit ein langsamer Check nicht in zwei aufeinanderfolgenden Takten doppelt gestartet wird – Nebenläufigkeit über Prozessgrenzen hinweg, gelöst in der Datenbank.
- [Makefile](../Makefile): `go test -race ./...`.

## Übungen

1. Vertausche in `Scheduler.Run` die Zeilen `workers.Wait()` und `close(results)`. Die bestehenden Tests bleiben grün – warum? Schreibe einen Fake-Checker, der `ctx` ignoriert und nach dem Abbruch noch ein Ergebnis liefert, und einen `synctest`-Test, der die Panik „send on closed channel“ auslöst. Stelle danach die Reihenfolge wieder her.
2. Gib dem `jobs`-Channel im Scheduler einen Puffer von 100. Welche Auswirkung hat das auf `ClaimDue` und auf Ziele, die beim Shutdown im Puffer liegen? Schreibe einen `synctest`-Test, der das sichtbar macht.
3. Schreibe `First` aus dem Leak-Beispiel so um, dass es einen `context.Context` annimmt, nach der ersten Antwort abbricht und die anderen Goroutines über `select` auf `ctx.Done()` aussteigen lässt. Prüfe mit dem `synctest`-Test, dass nichts leckt.
4. Ersetze in `beispiele/06-concurrency/race` den Mutex in `Stats` durch zwei Felder vom Typ `atomic.Int64`, eines für Erfolge und eines für Fehler. Was gewinnst du, was verlierst du gegenüber der Map mit Mutex?

---

← [Teil 05: Fehlerbehandlung](05-fehlerbehandlung.md) · [Übersicht](../README.md) · [Teil 07: Context richtig verstehen](07-context.md) →
