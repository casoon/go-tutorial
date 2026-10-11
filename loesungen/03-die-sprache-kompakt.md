# Lösungen zu Teil 03: Die Sprache, kompakt

Zu den Aufgaben: [Teil 03](../kapitel/03-die-sprache-kompakt.md#übungen)

## Übung 1: `Uptime` (Aufwärmen)

Die Funktion gehört dorthin, wo `Result` und `Up()` stehen: ins Paket `monitor`. Sie liegt in einer eigenen Datei `internal/monitor/uptime.go`:

```go
// Uptime returns the share of successful checks, between 0 and 1.
// An empty slice yields 0: without checks there is no evidence that the
// target is up, and a NaN from 0/0 would leak into JSON and comparisons.
func Uptime(results []Result) float64 {
	if len(results) == 0 {
		return 0
	}
	up := 0
	for _, r := range results {
		if r.Up() {
			up++
		}
	}
	return float64(up) / float64(len(results))
}
```

Der Patch enthält einen table-driven Test mit `nil`, nur erfolgreichen, nur fehlgeschlagenen und gemischten Ergebnissen.

```bash
git apply loesungen/03-die-sprache-kompakt/uebung-1.patch
go test ./internal/monitor
```

Für einen leeren Slice gibt die Lösung `0` zurück. Generics brauchst du nicht: Die Funktion arbeitet mit genau einem Typ, `monitor.Result`, und braucht genau dessen Methode `Up()`.

**Warum so:** Ohne den Sonderfall liefert `0/0` in Gleitkomma kein Panic, sondern `NaN` – und `NaN` ist tückisch: Es ist mit nichts gleich, nicht einmal mit sich selbst, und `encoding/json` weigert sich, es zu kodieren. `0` ist eine vertretbare, aber nicht die einzige Wahl. Wer „keine Daten“ von „0 % verfügbar“ unterscheiden muss, etwa für eine Statusseite, gibt besser `(float64, bool)` zurück, nach dem Muster `v, ok := m[key]`. Und `float64(up)` ist nötig, weil Go nicht implizit konvertiert; `up / len(results)` wäre eine Ganzzahldivision und ergäbe fast immer `0`. Ein `T any` wäre hier falsch, weil die Funktion ein bestimmtes Verhalten braucht, nicht einen beliebigen Typ – siehe „Wann nicht“ im Kapitel.

## Übung 2: `StatusDegraded` (Kern)

Zu ändern sind vier Stellen in [grundlagen/main.go](../beispiele/03-die-sprache-kompakt/grundlagen/main.go): die Konstanten, `String`, die Klassifizierung in `Result.Status` und die Ausgabeschleife in `main`.

```go
const (
	StatusUnknown Status = iota // 0, also the zero value
	StatusUp                    // 1
	StatusDown                  // 2
	StatusDegraded              // 3: up, but slower than slowThreshold

	numStatuses // not a status: the count, for loops over all of them
)

// slowThreshold separates up from degraded.
const slowThreshold = 2 * time.Second
```

```go
	case StatusDegraded:
		return "degraded"
```

```go
func (r Result) Status() Status {
	switch {
	case r.StatusCode == 0:
		return StatusUnknown
	case r.StatusCode < 400 && r.Duration > slowThreshold:
		return StatusDegraded
	case r.StatusCode < 400:
		return StatusUp
	default:
		return StatusDown
	}
}
```

In `main` zählte die Schleife bisher fest bis 3 (`for i := range 3`). Mit `numStatuses` läuft sie über alle Werte, und `range` über eine Ganzzahl vom Typ `Status` liefert direkt `Status`-Werte:

```go
for s := range numStatuses {
	fmt.Printf("%-8s %d\n", s, counts[s])
}
```

Damit die neue Zeile auch etwas zeigt, bekommt `results` ein langsames, erfolgreiches Ergebnis (`200` nach 2,5 Sekunden). Die Ausgabe:

```
unknown  1
up       1
down     1
degraded 1
```

In `TestResultStatus` bekommt die Tabelle ein Feld `duration`, und es kommen vier Fälle dazu: die Grenze (genau 2 Sekunden ist noch `up`), der eigentliche Fall (2,5 Sekunden ist `degraded`), ein langsamer Fehler (503 nach 5 Sekunden bleibt `down`) und ein langsames Ergebnis ohne Statuscode (bleibt `unknown`).

```bash
git apply loesungen/03-die-sprache-kompakt/uebung-2.patch
go test ./beispiele/03-die-sprache-kompakt/grundlagen
go run ./beispiele/03-die-sprache-kompakt/grundlagen
```

**Warum so:** Die neue Konstante steht am Ende, damit die bestehenden Werte gleich bleiben – mit `iota` ändert ein Einschub in der Mitte die Zahlen aller folgenden Konstanten, und gespeicherte Werte bekämen eine andere Bedeutung. Im `switch` ist die Reihenfolge entscheidend: Der Fall `degraded` muss vor dem allgemeinen `< 400` stehen, weil `switch` den ersten passenden Fall nimmt. Der wichtigste Testfall ist die Grenze, denn „langsamer als 2 Sekunden“ heißt `>` und nicht `>=`, und genau so ein Off-by-one fällt sonst niemandem auf. Die Sentinel-Konstante `numStatuses` ist ein verbreitetes Muster, damit niemand eine fest verdrahtete `3` vergisst; der Preis ist ein Wert, der wie ein Status aussieht, aber keiner ist. Die Alternative ist ein Slice `[]Status{StatusUnknown, StatusUp, StatusDown, StatusDegraded}`, der die Reihenfolge der Ausgabe ausdrücklich festlegt.

## Übung 3: `num` mit Rückgabewert (Kern)

Im Patch ersetzt die Variante die bisherige Closure in [config.go](../internal/config/config.go) – beide nebeneinander gingen nicht, weil eine unbenutzte lokale Variable ein Compilerfehler ist:

```go
// num returns the value instead of writing through a pointer. The
// default is a parameter, so an unset or invalid variable yields def.
num := func(key string, def int) int {
	v := getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		errs = append(errs, fmt.Errorf("%s: want a positive integer, got %q", key, v))
		return def
	}
	return n
}
```

Die Aufrufstelle:

```go
num("UPWATCH_WORKERS", &cfg.Workers)      // before
cfg.Workers = num("UPWATCH_WORKERS", 8)   // after
```

Länger wird die Aufrufstelle um die Zuweisung `cfg.Workers = …`: Das Ziel steht jetzt links statt als Pointer im Argument. Der Default kommt als zweiter Parameter herein. Deshalb fällt `Workers: 8` aus dem Struct-Literal am Anfang von `Load` weg; der Wert steht jetzt direkt neben dem Namen der Variablen.

```bash
git apply loesungen/03-die-sprache-kompakt/uebung-3.patch
go test ./internal/config
```

Die bestehenden Tests in `config_test.go` (Defaults, Overrides, Sammeln aller Fehler) bleiben unverändert grün – die Änderung ist von außen nicht sichtbar.

**Warum so:** Beide Varianten sind vertretbar, und es ist eher eine Geschmacksfrage. Die Rückgabe-Variante ist für sich genommen leichter zu verstehen: keine Pointer, ein Wert rein, ein Wert raus, und die Closure ließe sich ohne Änderung auch für eine lokale Variable verwenden. Dafür musst du das Ziel zweimal denken (links und, falls du `cfg.Workers` als Default übergibst, rechts), und die Defaults sind nicht mehr an einer Stelle versammelt, solange `str` und `dur` weiter das Struct-Literal benutzen. upwatch hat sich für die Pointer-Variante entschieden, weil dann alle Defaults in einem Block stehen und alle vier Closures gleich aussehen. Wichtig ist in beiden Fällen, dass ein ungültiger Wert den Default liefert *und* einen Fehler in `errs` hinterlässt: `Load` gibt am Ende ohnehin `errors.Join(errs...)` zurück, und `main` bricht dann ab.

## Übung 4: `Percentile` ohne Seiteneffekt (Profi)

```go
// Percentile returns the p-th percentile (0 to 100) by the nearest-rank
// method: the smallest value that at least p percent of all values do
// not exceed. It sorts a copy, never values itself: the caller's slice
// shares its array with us, and sorting it in place would reorder the
// caller's data behind its back. An empty slice yields the zero value.
func Percentile[T cmp.Ordered](values []T, p float64) T {
	if len(values) == 0 {
		var zero T
		return zero
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	rank = min(max(rank, 1), len(sorted))
	return sorted[rank-1]
}
```

Der Test, der genau das Aliasing prüft:

```go
func TestPercentileLeavesInputAlone(t *testing.T) {
	durations := []time.Duration{3 * time.Second, time.Second, 2 * time.Second}
	before := slices.Clone(durations)

	if got := Percentile(durations, 50); got != 2*time.Second {
		t.Errorf("median = %v, want 2s", got)
	}
	if !slices.Equal(durations, before) {
		t.Errorf("input was reordered: %v, want %v", durations, before)
	}
}
```

Ein zweiter Test prüft die Werte für p = 0, 40, 50, 80, 95 und 100 auf fünf Statuscodes und den leeren Slice. `main` gibt zusätzlich `p95 duration: 180ms` aus.

```bash
git apply loesungen/03-die-sprache-kompakt/uebung-4.patch
go test ./beispiele/03-die-sprache-kompakt/generics
```

Dass der Test wirklich etwas prüft, zeigt die Gegenprobe: Mit `sorted := values` statt `slices.Clone(values)` schlägt er fehl.

```
--- FAIL: TestPercentileLeavesInputAlone (0.00s)
    main_test.go:71: input was reordered: [1s 2s 3s], want [3s 1s 2s]
```

**Warum so:** Ein Slice-Parameter ist eine Kopie des Slice-Headers, nicht der Elemente. `slices.Sort(values)` würde das Array des Aufrufers sortieren, und der Fehler fiele erst viel später an anderer Stelle auf, etwa weil Ergebnisse plötzlich nicht mehr chronologisch sind. `slices.Clone` kostet eine Allokation und ist hier richtig; wer viele Perzentile aus denselben Daten braucht, sortiert einmal selbst und übergibt den sortierten Slice an eine Funktion, die das voraussetzt. Für Perzentile gibt es mehrere Definitionen – Nearest Rank wie hier, lineare Interpolation zwischen Nachbarwerten wie in vielen Statistikpaketen. Nearest Rank liefert immer einen Wert, der tatsächlich vorkam, und funktioniert deshalb mit jedem `cmp.Ordered`, auch mit Strings; Interpolation bräuchte Arithmetik und damit einen Constraint wie `Number`. `cmp.Ordered` statt `Number` ist hier also genau richtig, weil die Funktion nur vergleicht.
