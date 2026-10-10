# Teil 14: Performance und Speicher

Nach diesem Kapitel weißt du, wo Go Speicher anlegt, was der Garbage Collector tut und wie du mit Benchmarks und pprof herausfindest, ob sich eine Optimierung lohnt – und warum sie sich bei upwatch meistens nicht lohnt.

## Erst messen

Dieses Kapitel steht absichtlich fast am Ende. Go-Code ist ohne besondere Mühe schnell genug für die meisten Server: kompiliert, mit Werttypen, ohne JIT-Aufwärmphase und mit einer Laufzeit, die tausende gleichzeitige Verbindungen mit wenig Speicher bedient. Die typischen Engpässe eines Services sind Datenbankabfragen, Netzwerk und Sperren, nicht die Schleife, die einen String zusammensetzt.

Die Regel, die in der Go-Community immer wieder zitiert wird, lautet daher: erst messen, dann optimieren. Go macht das Messen leicht. Benchmarks sind Teil von `go test`, Profiling ist Teil der Standardbibliothek, und beides funktioniert ohne zusätzliche Werkzeuge. Optimierungen ohne Messung sind dagegen Vermutungen, die Code komplizierter machen.

## Stack und Heap

Ein Go-Programm legt Werte an zwei Orten ab. Der Stack gehört einer einzelnen Goroutine, wächst und schrumpft mit den Funktionsaufrufen und kostet beim Aufräumen nichts: Kehrt eine Funktion zurück, ist ihr Bereich frei. Der Heap ist gemeinsamer Speicher, den der Garbage Collector verwaltet. Alles, was dort liegt, muss der GC irgendwann finden, prüfen und freigeben.

In C# entscheidet der Typ, wo ein Wert landet: `struct` tendenziell auf dem Stack, `class` immer auf dem Heap. In Java liegen Objekte grundsätzlich auf dem Heap, und der JIT kann einzelne Allokationen per Escape Analysis wegoptimieren. In Go entscheidet nicht der Typ und nicht `new` oder `&`, sondern allein der Compiler, und zwar danach, ob ein Wert die Funktion überlebt. Ein `&result{}` kann auf dem Stack landen, ein `make([]int, n)` auf dem Heap. Für die Korrektheit ist das egal – Go hat keine hängenden Zeiger, weil der Compiler einen Wert, auf den nach dem Return noch jemand zeigt, automatisch auf den Heap legt. Für die Performance ist es nicht egal, denn Heap-Allokationen kosten Zeit beim Anlegen und Arbeit für den GC.

## Escape Analysis sichtbar machen

Der Compiler verrät seine Entscheidungen mit `-gcflags=-m`. Das Beispiel [beispiele/14-performance/escape](../beispiele/14-performance/escape/main.go) enthält vier kleine Funktionen:

```go
func up(r result) bool {
	return r.statusCode >= 200 && r.statusCode < 400
}

func newResult(code int) *result {
	r := result{statusCode: code}
	return &r
}

func sum() int64 {
	durations := make([]int64, 8)
	// ... fill and add up
}

func collect(n int) []int64 {
	durations := make([]int64, 0, n)
	for i := range n {
		durations = append(durations, int64(i))
	}
	return durations
}
```

```bash
go build -gcflags=-m -o /dev/null ./beispiele/14-performance/escape
```

Mit Go 1.27 auf einem Apple-M-Rechner liefert das (gekürzt: Pfade auf den Dateinamen verkürzt, ohne die Zeilen zu `fmt.Println`):

```
./main.go:16:6: can inline up
./main.go:22:6: can inline newResult
./main.go:29:6: can inline sum
./main.go:42:6: can inline collect
./main.go:23:2: moved to heap: r
./main.go:30:19: make([]int64, 8) does not escape
./main.go:43:19: make([]int64, 0, n) escapes to heap
./main.go:45:21: append escapes to heap
./main.go:53:47: make([]int64, 0, 4) does not escape
./main.go:53:47: append does not escape
```

Die Zeilen lesen sich so:

- `up` bekommt eine Kopie und gibt einen `bool` zurück. Nichts verlässt die Funktion, es gibt keine Meldung, also auch keine Heap-Allokation.
- `newResult` gibt einen Zeiger auf eine lokale Variable zurück. `r` muss den Aufruf überleben: „moved to heap“.
- In `sum` hat der Slice eine feste Größe und verlässt die Funktion nicht. Er liegt auf dem Stack.
- In `collect` ist die Größe erst zur Laufzeit bekannt und der Slice wird zurückgegeben. Er liegt auf dem Heap.

Die letzten beiden Zeilen sind der interessante Teil. Weil `collect` klein genug ist, wird der Aufruf `collect(4)` in `main` inlined. Danach kennt der Compiler die Kapazität (4) und sieht, dass das Ergebnis `main` nicht verlässt – und legt den Slice auf den Stack. Ob etwas auf den Heap kommt, hängt also nicht nur von der Funktion ab, sondern auch davon, wo und wie sie aufgerufen wird. Das ist einer der Gründe, warum man Performance-Fragen nicht durch Lesen von Code beantworten sollte, sondern durch Messen.

Für den Alltag reicht eine grobe Faustregel: Werte, die zurückgegeben werden, in Interfaces landen (auch als Argument für `fmt.Println` oder `slog`), in Closures eingefangen werden, die länger leben, oder deren Größe erst zur Laufzeit feststeht, landen meist auf dem Heap.

## Der Garbage Collector

Go hat einen nebenläufigen Mark-and-Sweep-Collector. Er läuft größtenteils parallel zum Programm auf anderen Kernen und hält die Goroutines nur für kurze Phasen an. Anders als .NET und die gängigen JVM-Collectoren ist er nicht generationell und verschiebt keine Objekte. Das hält ihn einfach und die Pausen kurz, kostet aber Durchsatz bei Programmen, die extrem viele kurzlebige Objekte erzeugen. Go gleicht das teilweise durch Escape Analysis aus: Viele kurzlebige Werte, die in Java im jungen Heap landen würden, landen in Go gar nicht erst auf dem Heap.

Die Zahl der Stellschrauben ist klein. Wer aus der JVM-Welt kommt, wo man zwischen Collectoren wählt und Dutzende Flags setzt, wird das merken. Go hat zwei:

`GOGC` (Default 100) bestimmt, wann der nächste Zyklus startet: wenn der Heap um diesen Prozentsatz über das gewachsen ist, was nach dem letzten Zyklus lebendig war. Bei 10 MB lebendigem Heap startet der nächste Zyklus also bei etwa 20 MB. Höhere Werte bedeuten weniger GC-Arbeit und mehr Speicher, niedrigere das Umgekehrte. `GOGC=off` schaltet den GC ab.

`GOMEMLIMIT` (seit Go 1.19) ist ein weiches Speicherlimit für die gesamte Go-Laufzeit, zum Beispiel `GOMEMLIMIT=200MiB`. Nähert sich der Prozess dem Limit, sammelt der GC häufiger, unabhängig von `GOGC`. Das ist vor allem in Containern nützlich: Setzt du `GOMEMLIMIT` etwas unter das Speicherlimit des Containers, räumt Go auf, bevor der OOM-Killer den Prozess beendet. Weich heißt: Liegt mehr lebendiger Speicher vor, als das Limit erlaubt, überschreitet Go es lieber, als in einer Endlosschleife zu sammeln.

Beide Werte lassen sich auch zur Laufzeit über `runtime/debug.SetGCPercent` und `SetMemoryLimit` ändern. Mit `GODEBUG=gctrace=1` gibt die Laufzeit pro GC-Zyklus eine Zeile mit Heap-Größen und Pausenzeiten auf stderr aus.

Für upwatch musst du nichts davon anfassen. Der Prozess hält ein paar Megabyte lebendigen Heap; die Defaults sind dafür gemacht.

## Slices und Allokationen

Ein Slice, der mit `append` wächst, muss gelegentlich ein größeres Backing-Array anlegen und den Inhalt umkopieren. Wenn du die Endgröße kennst, kannst du das mit `make(T, 0, n)` vermeiden. upwatch macht das in [httpapi.go](../internal/httpapi/httpapi.go), wo die DTOs für die JSON-Antwort gebaut werden:

```go
out := make([]targetJSON, 0, len(targets))
for _, t := range targets {
	out = append(out, toTargetJSON(t))
}
```

`len(targets)` ist bekannt, also gibt es genau eine Allokation. Das Beispiel [beispiele/14-performance/bench](../beispiele/14-performance/bench/) vergleicht beide Varianten mit 1000 Elementen:

```go
func collectGrow(n int) []target {
	var out []target
	for i := range n {
		out = append(out, target{ID: int64(i), URL: "https://example.com"})
	}
	return out
}

func collectPrealloc(n int) []target {
	out := make([]target, 0, n)
	// same loop
	return out
}
```

Wenn die Größe nicht bekannt ist, lass `append` seine Arbeit machen. Es wächst in Schritten, die das Umkopieren im Mittel billig halten. Eine geschätzte Kapazität, die oft zu groß ist, verschwendet dagegen Speicher bei jedem Aufruf.

Die Länge mit `make(T, n)` statt der Kapazität zu setzen und dann `append` aufzurufen, ist ein häufiger Fehler: Der Slice beginnt dann mit `n` Nullwerten, und die neuen Elemente kommen dahinter.

## Strings, []byte und strings.Builder

Strings sind in Go unveränderlich, `[]byte` ist veränderbar. Die Umwandlung zwischen beiden kopiert in der Regel die Daten, weil sonst jemand über das `[]byte` den String ändern könnte. Der Compiler spart sich die Kopie in einigen eindeutigen Fällen, etwa bei `m[string(b)]` als Map-Schlüssel oder bei Vergleichen. Wenn du ständig hin- und herwandelst, lohnt es sich, bei einer Darstellung zu bleiben: `bytes` und `strings` haben fast dieselben Funktionen.

`s += x` in einer Schleife legt bei jedem Schritt einen neuen String an und kopiert alles bisherige. Das ist dasselbe Problem wie in Java und C#, und die Lösung ist dieselbe: ein Builder. `strings.Builder` sammelt in einem wachsenden Puffer und gibt am Ende den String zurück, ohne den Puffer noch einmal zu kopieren.

```go
func joinBuilder(codes []int) string {
	var b strings.Builder
	for i, c := range codes {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(c))
	}
	return b.String()
}
```

Wenn die Endgröße ungefähr bekannt ist, reserviert `b.Grow(n)` den Platz vorab. Für ein paar Teile ist `+` oder `fmt.Sprintf` völlig in Ordnung – der Builder lohnt sich in Schleifen.

## Benchmarks

Ein Benchmark ist eine Funktion `BenchmarkXxx(b *testing.B)` in einer `_test.go`-Datei. Seit Go 1.24 schreibt man die Schleife mit `b.Loop()`:

```go
func BenchmarkJoinBuilder(b *testing.B) {
	codes := statusCodes(100)
	for b.Loop() {
		joinBuilder(codes)
	}
}
```

`b.Loop` kümmert sich um zwei Dinge, die man mit der älteren Form `for i := 0; i < b.N; i++` selbst beachten musste: Der Code vor der Schleife zählt nicht zur gemessenen Zeit, und der Compiler darf die Aufrufe in der Schleife nicht wegoptimieren, auch wenn das Ergebnis nicht benutzt wird. In [monitor_test.go](../internal/monitor/monitor_test.go) steht ein Benchmark für `NormalizeURL` in genau dieser Form.

`b.Loop` verhindert allerdings nicht, dass der Compiler einen Aufruf inlined und dann anders alloziert als im echten Code. Im Beispiel landete `collectPrealloc(1000)` so zunächst komplett auf dem Stack und meldete 0 Allokationen – ein Messwert, der mit dem echten Handler nichts zu tun hat, weil dort der Slice in die JSON-Antwort geht. Die Benchmarks weisen das Ergebnis deshalb einer Paketvariablen `sink` zu. Solche Effekte sind der Grund, warum man Benchmarks wie Tests liest: Messen sie wirklich das, was im Programm passiert?

Ausgeführt werden Benchmarks mit `-bench`, `-benchmem` ergänzt die Allokationen:

```bash
go test -bench=. -benchmem ./beispiele/14-performance/bench
```

Auf einem Apple M4 Pro mit Go 1.27.1 kam dabei heraus:

```
cpu: Apple M4 Pro
BenchmarkJoinPlus-14           	  166946	      6875 ns/op	   42064 B/op	     298 allocs/op
BenchmarkJoinBuilder-14        	  891014	      1384 ns/op	    1344 B/op	     107 allocs/op
BenchmarkCollectGrow-14        	  249103	      4836 ns/op	   59368 B/op	      11 allocs/op
BenchmarkCollectPrealloc-14    	  661416	      1924 ns/op	   24576 B/op	       1 allocs/op
```

Die Spalten sind Iterationen, Zeit pro Aufruf, Bytes pro Aufruf und Allokationen pro Aufruf. Die Allokationsspalten sind oft aussagekräftiger als die Zeit, weil sie stabil sind und direkt zeigen, was der GC später aufräumen muss. Die 107 Allokationen beim Builder sind übrigens nicht der Builder selbst: 100 davon kommen von `strconv.Itoa`, das für jede Zahl einen kleinen String erzeugt. Wer auch die loswerden will, schreibt mit `strconv.AppendInt` direkt in einen `[]byte`-Puffer. Bei 100 Statuscodes lohnt das nicht.

Einzelne Läufe schwanken. Zeiten aus einem einzigen Lauf zu vergleichen, führt zu Schlussfolgerungen aus Rauschen. Das Werkzeug dafür ist `benchstat` aus `golang.org/x/perf`: Du lässt Vorher und Nachher jeweils mehrfach laufen und vergleichst die Verteilungen.

```bash
go test -bench=. -benchmem -count=10 ./internal/monitor > old.txt
# change the code
go test -bench=. -benchmem -count=10 ./internal/monitor > new.txt
go run golang.org/x/perf/cmd/benchstat@latest old.txt new.txt
```

`benchstat` zeigt für jeden Benchmark Median und Schwankung beider Läufe, die Änderung in Prozent und ob sie statistisch belastbar ist. Steht dort `~`, ist der Unterschied nicht von Rauschen zu unterscheiden. Für die Werte oben (sechs Läufe) meldete benchstat zum Beispiel `7.284µ ± 6%` für `JoinPlus` – sechs Prozent Schwankung auf einem Laptop mit laufenden Hintergrundprozessen. Eine „Optimierung“ um drei Prozent wäre darin nicht sichtbar.

## pprof: Wo die Zeit wirklich hingeht

Benchmarks messen Funktionen, die du schon im Verdacht hast. Ein Profil zeigt, wo ein laufendes Programm seine Zeit und seinen Speicher tatsächlich verbraucht. `net/http/pprof` stellt Profile über HTTP bereit, und upwatch hängt sie an den separaten Debug-Listener in [main.go](../cmd/upwatch/main.go):

```go
mux.HandleFunc("GET /debug/pprof/", pprof.Index)
mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
```

`pprof.Index` liefert auch die benannten Profile wie `heap`, `goroutine` und `allocs` aus. Der Listener startet nur mit gesetzter Variable und gehört auf localhost (siehe [Teil 13](13-security.md)):

```bash
UPWATCH_DEBUG_ADDR=127.0.0.1:6060 go run ./cmd/upwatch
```

Ein CPU-Profil über zehn Sekunden holst du in einem zweiten Terminal:

```bash
go tool pprof 'http://127.0.0.1:6060/debug/pprof/profile?seconds=10'
```

`go tool pprof` lädt das Profil, speichert es lokal und öffnet eine interaktive Konsole. Dort zeigt `top` die teuersten Funktionen, `list Funktionsname` den Quelltext mit Kosten pro Zeile und `web` einen Aufrufgraphen im Browser. Bequemer ist oft die Web-Oberfläche mit Flame Graph: `go tool pprof -http=localhost:8081 <profil>`. Für eine schnelle Textausgabe gibt es `-top`.

Lokal gemessen auf einem Apple M4 Pro mit Go 1.27.1: upwatch mit fünf Zielen (seine eigene `/healthz`, deshalb mit `UPWATCH_ALLOW_PRIVATE_TARGETS=true`), sonst ohne Last. Das CPU-Profil:

```
Duration: 10.10s, Total samples = 20ms (  0.2%)
```

20 Millisekunden CPU in zehn Sekunden, und die verteilen sich auf Laufzeitfunktionen wie den Sweeper des GC und das Warten auf Netzwerkereignisse (`runtime.kevent` auf macOS). Es gibt nichts zu optimieren.

Im zweiten Durchlauf hat ein kleiner Lastgenerator aus 16 Goroutines `GET /targets/1/results?limit=100` in einer Schleife abgefragt, also synthetische Dauerlast auf die API über Loopback (gekürzt):

```
Duration: 10.13s, Total samples = 12.83s (126.61%)
      flat  flat%   sum%        cum   cum%
     5.81s 45.28% 45.28%      5.81s 45.28%  syscall.rawsyscalln
     3.11s 24.24% 69.52%      3.11s 24.24%  runtime.usleep
     1.03s  8.03% 77.55%      1.03s  8.03%  runtime.kevent
     ...
     0.06s  0.47% 87.22%      1.73s 13.48%  modernc.org/sqlite/lib._sqlite3VdbeExec
     0.06s  0.47% 87.69%      0.77s  6.00%  modernc.org/sqlite/lib._yy_reduce
     ...
     0.03s  0.23% 88.46%      0.81s  6.31%  modernc.org/sqlite/lib._sqlite3RunParser
```

`flat` ist die Zeit in der Funktion selbst, `cum` inklusive aller aufgerufenen Funktionen. Fast die Hälfte steckt in Systemaufrufen (Socket lesen und schreiben, Datei-I/O), ein Viertel im Scheduler, der zwischen Goroutines wechselt. Der eigene Code von upwatch taucht in den Top-Einträgen gar nicht auf. Sichtbar wird SQLite: Rund 13 Prozent für das Ausführen der Abfrage, rund 6 Prozent dafür, das SQL von `Results` bei jedem Aufruf neu zu parsen. Wäre das ein echtes Problem, wäre ein vorbereitetes Statement der naheliegende Ansatz – aber erst, wenn reale Last so aussieht, was bei einem Monitoring-Dienst mit ein paar Nutzern nicht der Fall ist.

Das Heap-Profil holst du genauso:

```bash
go tool pprof http://127.0.0.1:6060/debug/pprof/heap
```

Es enthält zwei Sichten. `inuse_space` (Default) zeigt, was gerade lebt – nach der Lastphase waren das bei upwatch etwa 6,5 MB. `-sample_index=alloc_space` zeigt alles, was seit dem Start alloziert wurde, und damit, wer den GC beschäftigt. Dort stand `sqlitestore.(*Store).Results` mit 28 Prozent kumuliert oben, gefolgt vom Header-Parsing in `net/textproto`. Auch das passt zu dem, was der Code tut: Die Last bestand nur aus diesem einen Endpunkt, und `Results` baut seinen Slice ohne Vorallokation auf.

Profile kannst du auch aus Tests erzeugen, ohne Server: `go test -cpuprofile cpu.out -memprofile mem.out -bench=.` schreibt die Dateien, `go tool pprof cpu.out` öffnet sie.

## Was das für upwatch bedeutet

Ein Check in upwatch wartet auf DNS, auf den TCP-Handshake, auf TLS und auf die Antwort eines fremden Servers. Das dauert Millisekunden bis Sekunden. Die CPU-Arbeit drumherum – Request bauen, Statuscode lesen, Ergebnis in SQLite schreiben – liegt im Bereich von Mikrosekunden. Der einzige Benchmark im Projekt, der für `NormalizeURL`, meldete rund 220 ns pro Aufruf auf dem M4 Pro mit Go 1.27.1 – für eine Funktion, die nur beim Anlegen eines Ziels läuft. Selbst eine Funktion im Check-Pfad doppelt so schnell zu machen, änderte an der Laufzeit eines Checks nichts Messbares.

Was bei upwatch tatsächlich Durchsatz bestimmt, ist die Zahl der Worker (`UPWATCH_WORKERS`), weil sie festlegt, wie viele Checks gleichzeitig auf Netzwerk warten dürfen, und der Check-Timeout, weil hängende Ziele Worker belegen. Das sind Konfigurationsfragen, keine Code-Optimierungen. Der einzelne Writer, der Ergebnisse in SQLite schreibt, wäre der nächste Engpass, aber erst bei einer Zahl von Zielen, für die man ohnehin über eine andere Datenbank nachdenken würde.

Diese Einordnung ist der eigentliche Punkt des Kapitels: Die Werkzeuge sind gut und schnell zur Hand, und gerade deshalb gibt es keinen Grund, auf Verdacht zu optimieren.

## sync.Pool – mit Vorsicht

`sync.Pool` hält bereits angelegte Objekte zur Wiederverwendung vor, typischerweise Puffer:

```go
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

func render(v any) []byte {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)
	// ... write into buf
	return bytes.Clone(buf.Bytes())
}
```

Die Standardbibliothek nutzt das intern, etwa in `fmt` und `encoding/json`. Für eigenen Code gibt es einige Fallen. Der Pool darf Objekte jederzeit bei einem GC-Lauf verwerfen, er ist also kein Cache. Jedes Objekt muss vor der Wiederverwendung zurückgesetzt werden, sonst leaken Daten von einem Request in den nächsten. Wer nach dem `Put` noch eine Referenz auf den Puffer hält (oben deshalb `bytes.Clone`), hat einen Datenwettlauf. Und Puffer stark unterschiedlicher Größe im selben Pool halten nach einem einzigen großen Request dauerhaft viel Speicher fest.

Setze `sync.Pool` nur ein, wenn ein Profil zeigt, dass die Allokationen genau dieses Objekts ein Problem sind, und miss danach, ob es geholfen hat. In upwatch gibt es keine solche Stelle.

## Im Projekt

- [internal/httpapi/httpapi.go](../internal/httpapi/httpapi.go): `make([]targetJSON, 0, len(targets))` und `make([]resultJSON, 0, len(results))` – Vorallokation, wo die Größe bekannt ist.
- [internal/monitor/monitor_test.go](../internal/monitor/monitor_test.go): `BenchmarkNormalizeURL` mit `b.Loop`.
- [cmd/upwatch/main.go](../cmd/upwatch/main.go): `serveDebug` mit pprof und expvar auf einem eigenen Listener.
- [internal/check/check.go](../internal/check/check.go): `MaxIdleConnsPerHost` und das Leeren des Bodys, damit Verbindungen wiederverwendet werden – die einzige Performance-Maßnahme, die bei Netzwerk-I/O wirklich zählt.
- [internal/sqlitestore/store.go](../internal/sqlitestore/store.go): `SetMaxOpenConns(4)` und WAL-Modus, abgestimmt auf SQLite (siehe [Teil 10](10-datenbanken.md)).

## Übungen

1. Führe `go build -gcflags=-m ./internal/httpapi` aus und suche die Zeilen zu `out` in `listTargets`. Landet der Slice auf dem Heap? Warum? Probiere `-gcflags='-m -m'` für die ausführliche Begründung.
2. `Store.Results` baut seinen Slice ohne Kapazität auf, obwohl `limit` bekannt ist. Schreib einen Benchmark gegen eine echte SQLite-Datenbank im Temp-Verzeichnis (`t.TempDir()` gibt es auch als `b.TempDir()`), der 1000 Ergebnisse liest, ändere die Zeile auf `make([]monitor.Result, 0, limit)` und vergleiche mit `benchstat`. Lohnt sich das, wenn die meisten Abfragen nur 50 Zeilen liefern?
3. Starte upwatch mit `UPWATCH_DEBUG_ADDR=127.0.0.1:6060`, lege einige Ziele an und hole das Goroutine-Profil mit `go tool pprof -top http://127.0.0.1:6060/debug/pprof/goroutine`. Findest du die Worker des Schedulers wieder? Wie viele sind es, und passt das zu `UPWATCH_WORKERS`?
4. Starte upwatch mit `GODEBUG=gctrace=1` und lies die Zeilen auf stderr. Wie groß ist das Heap-Ziel, und wie oft läuft der GC im Leerlauf? Wiederhole es mit `GOGC=400`.

---

← [Teil 13: Security](13-security.md) · [Übersicht](../README.md) · [Teil 15: Deployment](15-deployment.md) →
