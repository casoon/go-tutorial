# Lösungen zu Teil 14: Performance und Speicher

Zu den Aufgaben: [Teil 14](../kapitel/14-performance.md#übungen)

Alle Messungen auf einem Apple M4 Pro mit Go 1.27.1, während auf demselben Rechner andere Prozesse liefen. Zeiten werden bei dir anders aussehen; Bytes und Allokationen sollten übereinstimmen.

## Übung 1: Escape Analysis in listTargets (Aufwärmen)

```bash
go build -gcflags=-m ./internal/httpapi
```

Die Zeilen zu `listTargets` (Zeile 88–98 in [httpapi.go](../internal/httpapi/httpapi.go)):

```
internal/httpapi/httpapi.go:96:33: inlining call to toTargetJSON
internal/httpapi/httpapi.go:94:13: make([]targetJSON, 0, len(targets)) escapes to heap
internal/httpapi/httpapi.go:96:15: append escapes to heap
internal/httpapi/httpapi.go:98:30: out escapes to heap
```

Ja, der Slice landet auf dem Heap. Die ausführliche Fassung mit `-gcflags='-m -m'` nennt den Grund (gekürzt):

```
internal/httpapi/httpapi.go:98:30: out escapes to heap in (*API).listTargets:
internal/httpapi/httpapi.go:98:30:   flow: {heap} ← &{storage for out}:
internal/httpapi/httpapi.go:98:30:     from out (spill) at internal/httpapi/httpapi.go:98:30
internal/httpapi/httpapi.go:98:30:     from writeJSON(w, 200, out) (call parameter) at internal/httpapi/httpapi.go:98:11
internal/httpapi/httpapi.go:94:13: make([]targetJSON, 0, len(targets)) escapes to heap in (*API).listTargets:
internal/httpapi/httpapi.go:94:13:   flow: out ← &{storage for make([]targetJSON, 0, len(targets))}:
...
internal/httpapi/httpapi.go:94:13:   flow: {storage for out} ← out:
internal/httpapi/httpapi.go:94:13:     from out (interface-converted) at internal/httpapi/httpapi.go:98:30
```

Die Kette liest sich von unten nach oben: `out` wird bei `writeJSON(w, http.StatusOK, out)` in ein `any` umgewandelt („interface-converted“). Der Slice-Header kommt dafür in eigenen Speicher („storage for out“), und dieser Speicher fließt als Argument in `writeJSON` und von dort zum Encoder – der Compiler kann nicht beweisen, dass niemand ihn über den Aufruf hinaus festhält. Damit muss auch das Backing-Array aus `make` auf den Heap. Unabhängig davon wäre es auch ohne Interface auf dem Heap gelandet, weil `len(targets)` erst zur Laufzeit feststeht.

**Warum so:** Das ist die Faustregel aus dem Kapitel in der Praxis – Werte, die in Interfaces landen, und Größen, die erst zur Laufzeit feststehen, gehen auf den Heap. Daran ist nichts zu reparieren: Der Slice muss den Handler überleben, bis der JSON-Encoder fertig ist, und eine Allokation pro Request ist neben dem Datenbankzugriff bedeutungslos. Die Vorallokation mit `len(targets)` sorgt dafür, dass es bei dieser einen bleibt, statt `append` mehrmals umkopieren zu lassen.

## Übung 2: Vorallokation in Store.Results messen (Profi)

Der Benchmark legt in einer echten SQLite-Datenbank im Temp-Verzeichnis zwei Ziele an, eines mit 1000 und eines mit 5 Ergebnissen, und misst drei Fälle:

```go
func BenchmarkResults(b *testing.B) {
	s, err := Open(b.Context(), filepath.Join(b.TempDir(), "bench.db"))
	// ...
	full := benchTarget(b, s, "https://example.com/full", 1000)
	sparse := benchTarget(b, s, "https://example.com/sparse", 5)

	for _, bc := range []struct {
		target monitor.Target
		rows   int
		limit  int
	}{
		{full, 1000, 1000},
		{full, 1000, 50},
		{sparse, 5, 1000},
	} {
		b.Run(fmt.Sprintf("rows=%d/limit=%d", bc.rows, bc.limit), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				res, err := s.Results(b.Context(), bc.target.ID, bc.limit)
				if err != nil || len(res) != min(bc.rows, bc.limit) {
					b.Fatalf("Results = %d rows, %v", len(res), err)
				}
			}
		})
	}
}
```

`benchTarget` schreibt die Ergebnisse in einer Transaktion direkt in die Tabelle, damit das Aufsetzen Millisekunden statt Sekunden dauert. Der Ablauf:

```bash
git apply loesungen/14-performance/uebung-2.patch
go test -run='^$' -bench=Results -benchmem -count=10 ./internal/sqlitestore > old.txt
# in Store.Results: results := make([]monitor.Result, 0, limit)
go test -run='^$' -bench=Results -benchmem -count=10 ./internal/sqlitestore > new.txt
go run golang.org/x/perf/cmd/benchstat@latest old.txt new.txt
```

```
                                │   old.txt   │                new.txt                │
                                │   sec/op    │    sec/op     vs base                 │
Results/rows=1000/limit=1000-14   1.086m ± 3%   1.183m ± 18%         ~ (p=0.052 n=10)
Results/rows=1000/limit=50-14     77.91µ ± 3%   75.85µ ± 21%         ~ (p=0.143 n=10)
Results/rows=5/limit=1000-14      25.99µ ± 1%   77.01µ ± 41%  +196.36% (p=0.000 n=10)

                                │   old.txt    │                 new.txt                 │
                                │     B/op     │     B/op       vs base                  │
Results/rows=1000/limit=1000-14   286.7Ki ± 0%    188.6Ki ± 0%    -34.23% (p=0.000 n=10)
Results/rows=1000/limit=50-14     17.44Ki ± 0%    11.20Ki ± 0%    -35.82% (p=0.000 n=10)
Results/rows=5/limit=1000-14      2.977Ki ± 0%   73.856Ki ± 0%  +2381.27% (p=0.000 n=10)

                                │   old.txt   │              new.txt               │
                                │  allocs/op  │  allocs/op   vs base               │
Results/rows=1000/limit=1000-14   4.791k ± 0%   4.781k ± 0%  -0.21% (p=0.000 n=10)
Results/rows=1000/limit=50-14      289.5 ± 0%    283.5 ± 0%  -2.07% (p=0.000 n=10)
Results/rows=5/limit=1000-14       63.00 ± 0%    60.00 ± 0%  -4.76% (p=0.000 n=10)
```

Ein zweiter Durchgang kurz danach, während der Rechner stärker ausgelastet war, ergab für denselben Code doppelt so lange Zeiten mit ± 15 bis 49 Prozent Schwankung – und für `rows=1000/limit=1000` plötzlich einen „signifikanten“ Unterschied von +35 Prozent. Die Spalten `B/op` und `allocs/op` waren in beiden Durchgängen identisch.

Die Antwort auf die Frage: Nein, es lohnt sich nicht. Bei 50 Zeilen spart die Vorallokation 6 Allokationen von 290 und gut 6 KiB, die Zeit ändert sich nicht messbar. Die Allokationen pro Abfrage kommen fast vollständig aus dem Treiber und aus `rows.Scan` (rund 4,8 pro Zeile), nicht aus dem Slice. Dafür kostet die Änderung im ungünstigen Fall viel: Ein Ziel mit 5 Ergebnissen und `limit=1000` reserviert 1000 × 72 Byte, also gut 70 KiB, die genullt und vom GC wieder eingesammelt werden müssen – 25-mal so viel Speicher und messbar langsamer.

Der Patch enthält deshalb nur den Benchmark, nicht die Änderung an `Results`. Die Zeile probierst du selbst wie oben beschrieben.

**Warum so:** `limit` ist eine Obergrenze, keine erwartete Größe – das ist der Unterschied zu `len(targets)` in `listTargets`, wo die Endgröße exakt feststeht. Wer trotzdem vorallozieren will, nimmt eine kleine Kappe wie `make([]monitor.Result, 0, min(limit, 64))`; aber auch das lohnt erst, wenn ein Profil `Results` als Engpass zeigt. Der Benchmark selbst ist das eigentlich Wertvolle: Er misst gegen echtes SQLite und deckt mit dem dünn besetzten Ziel genau den Fall ab, an dem die naheliegende Optimierung kippt. Und der zweite Durchgang zeigt, warum man Zeiten von einem belasteten Laptop nicht trauen sollte, Allokationen aber schon.

## Übung 3: Goroutine-Profil (Kern)

```bash
UPWATCH_DEBUG_ADDR=127.0.0.1:18352 UPWATCH_ADDR=127.0.0.1:18351 \
  UPWATCH_ALLOW_PRIVATE_TARGETS=true UPWATCH_TICK=1s ./upwatch
# second terminal: five targets pointing at upwatch itself, then
go tool pprof -top http://127.0.0.1:18352/debug/pprof/goroutine
```

Mit der Default-Einstellung `UPWATCH_WORKERS=8` (gekürzt):

```
Type: goroutine
Showing nodes accounting for 25, 100% of 25 total
      flat  flat%   sum%        cum   cum%
        23 92.00% 92.00%         23 92.00%  runtime.gopark
...
         0     0%   100%          1  4.00%  github.com/casoon/go-tutorial/internal/scheduler.(*Scheduler).Run
         0     0%   100%          8 32.00%  github.com/casoon/go-tutorial/internal/scheduler.(*Scheduler).Run.func1
         0     0%   100%          1  4.00%  github.com/casoon/go-tutorial/internal/scheduler.(*Scheduler).Run.func2
         0     0%   100%          1  4.00%  github.com/casoon/go-tutorial/internal/scheduler.(*Scheduler).dispatch
         0     0%   100%          8 32.00%  github.com/casoon/go-tutorial/internal/scheduler.(*Scheduler).work
         0     0%   100%          1  4.00%  github.com/casoon/go-tutorial/internal/scheduler.(*Scheduler).write
...
         0     0%   100%          2  8.00%  net/http.(*persistConn).readLoop
         0     0%   100%          2  8.00%  net/http.(*persistConn).writeLoop
...
         0     0%   100%          9 36.00%  sync.(*WaitGroup).Go.func1
```

Ja, die Worker sind da: 8 Goroutines in `Scheduler.work`, gestartet über `Run.func1`, die Closure in `workers.Go(func() { s.work(ctx, jobs, results) })`. Dazu kommen der Writer (`Run.func2` mit `write`) und `Run` selbst, das in `dispatch` auf den Ticker wartet. `WaitGroup.Go.func1` zählt 9, weil `main` den Scheduler ebenfalls mit `wg.Go` startet. Gegenprobe mit `UPWATCH_WORKERS=3`: 14 Goroutines insgesamt, 3 in `work`, 4 in `WaitGroup.Go.func1`. Die Zahl passt also genau zu `UPWATCH_WORKERS`, und sie hängt nicht von der Zahl der Ziele ab.

Den Rest teilen sich die Laufzeit und `net/http`: je ein Accept-Loop für API und Debug-Listener, `serveDebug`, der Signal-Handler von `signal.NotifyContext`, zwei Hintergrund-Goroutines von `database/sql` und pro offener Keep-alive-Verbindung des Check-Clients ein `readLoop` und ein `writeLoop`. Mit `?debug=1` liefert der Endpunkt dieselben Daten als Text mit gruppierten Stacks, etwa `3 @ 0x… ` gefolgt von `scheduler.(*Scheduler).work … scheduler.go:117`.

**Warum so:** Das Goroutine-Profil ist das Werkzeug, um Leaks zu finden: Eine feste, erklärbare Zahl wie hier ist gesund, eine Zahl, die mit der Laufzeit oder der Zahl der Requests wächst, ist es nicht. Dass die Worker-Zahl fest ist und nicht mit den Zielen skaliert, ist die Entscheidung aus Teil 06 – ein Pool begrenzt die gleichzeitigen Checks. `go tool pprof` legt das geladene Profil übrigens unter `~/pprof/` ab; mit `PPROF_TMPDIR` lenkst du es woandershin.

## Übung 4: gctrace im Leerlauf (Kern)

Zwei Instanzen gleichzeitig, eine mit Default, eine mit `GOGC=400`, jeweils mit `GODEBUG=gctrace=1`, rund sieben Minuten lang. Nach fünf Minuten Leerlauf habe ich auf beide je 20.000 `GET /targets` geschickt.

Mit `GOGC=100` (Default):

```
gc 1 @0.002s 2%: 0.11+0.57+0.030 ms clock, 1.5+0.10/0.80/0.014+0.43 ms cpu, 3->4->2 MB, 4 MB goal, 0 MB stacks, 0 MB globals, 14 P
GC forced
gc 2 @120.007s 0%: 0.24+5.7+0.007 ms clock, 3.4+0/13/0+0.10 ms cpu, 3->3->2 MB, 4 MB goal, 0 MB stacks, 0 MB globals, 14 P
GC forced
gc 3 @245.008s 0%: 0.17+1.0+0.25 ms clock, 2.5+0.94/2.5/0+3.6 ms cpu, 2->2->2 MB, 4 MB goal, 0 MB stacks, 0 MB globals, 14 P
...  (162 cycles during the 20,000 requests)
gc 165 @300.390s 0%: 0.044+0.51+0.004 ms clock, 0.61+0.019/1.1/0.25+0.063 ms cpu, 4->4->2 MB, 5 MB goal, 0 MB stacks, 0 MB globals, 14 P
GC forced
gc 166 @425.006s 0%: 0.11+0.43+0.003 ms clock, 1.6+0.052/0.96/0+0.054 ms cpu, 3->3->2 MB, 5 MB goal, 0 MB stacks, 0 MB globals, 14 P
```

Mit `GOGC=400`:

```
gc 1 @296.171s 0%: 0.050+0.80+0.027 ms clock, 0.70+0/1.6/0.028+0.38 ms cpu, 15->15->2 MB, 16 MB goal, 0 MB stacks, 0 MB globals, 14 P
...  (20 cycles during the 20,000 requests)
gc 20 @297.935s 0%: 0.050+0.45+0.013 ms clock, 0.70+0.019/1.0/0.15+0.19 ms cpu, 15->15->2 MB, 16 MB goal, 0 MB stacks, 0 MB globals, 14 P
GC forced
gc 21 @420.007s 0%: 0.093+0.42+0.003 ms clock, 1.3+0.13/1.0/0+0.048 ms cpu, 14->14->2 MB, 16 MB goal, 0 MB stacks, 0 MB globals, 14 P
```

Die Felder `3->4->2 MB, 4 MB goal` bedeuten: Heap beim Start des Zyklus, Heap am Ende der Markierphase, davon lebendig – und das Ziel für den nächsten Zyklus. upwatch hat rund 2 MB lebendigen Heap. Das Heap-Ziel ist mit dem Default 4 MB, unter Last 5 MB; mit `GOGC=400` 16 MB.

Warum 4 MB und nicht 2 × 2 MB? Die Laufzeit setzt ein Mindestziel von 4 MB, skaliert mit `GOGC/100` – bei `GOGC=400` also 16 MB. Bei so kleinem Heap bestimmt dieses Minimum das Ziel, nicht der lebendige Heap.

Im Leerlauf läuft der GC mit Default praktisch nur, weil die Laufzeit ihn erzwingt: „GC forced“ alle zwei Minuten (bei 120 s und 245 s). Mit `GOGC=400` lief in den ersten fünf Minuten überhaupt kein Zyklus, nicht einmal ein erzwungener – der Zwangszyklus greift nur, wenn seit dem letzten GC zwei Minuten vergangen sind, und es gab noch keinen. Erst die Last löste den ersten aus; danach kam auch bei `GOGC=400` zwei Minuten später ein erzwungener (gc 21). Unter Last war der Unterschied deutlich: 162 Zyklen mit Default gegen 20 mit `GOGC=400` für dieselben 20.000 Requests, bei einem Heap, der jeweils bis knapp unter das Ziel wuchs (4–5 MB gegen 15 MB). Die Pausen (erster und dritter Wert unter „clock“) lagen in beiden Fällen bei Zehntelmillisekunden.

**Warum so:** `GOGC` tauscht Speicher gegen GC-Arbeit, und genau das zeigt der Vergleich: viermal so großes Ziel, ein Achtel der Zyklen. Für upwatch ist beides irrelevant – die Zyklen kosten Bruchteile einer Millisekunde und laufen im Leerlauf nur alle zwei Minuten. Interessant wird `GOGC` erst bei Programmen mit viel kurzlebigem Müll und knappem CPU-Budget, und im Container ist `GOMEMLIMIT` meist der bessere Hebel, weil es eine Obergrenze setzt, statt das Wachstum nur zu skalieren.
