# Lösungen zu Teil 04: Go-Denkweise statt OOP-Denkweise

Zu den Aufgaben: [Teil 04](../kapitel/04-go-denkweise.md#übungen)

## Übung 1: `countingStore` mit benanntem Feld (Aufwärmen)

Der erste Schritt ist nur die Umstellung von `Store` auf `inner Store` (und `c.Store.Save` auf `c.inner.Save`). Danach meldet der Compiler (gekürzt):

```
./main.go:104:53: store.Count undefined (type *countingStore has no field or method Count)
./main_test.go:20:26: unknown field Store in struct literal of type countingStore
./main_test.go:24:31: store.Count undefined (type *countingStore has no field or method Count)
```

Es fehlt `Count`. Gemeldet wird das nicht bei der Typdeklaration, sondern dort, wo jemand `Count` aufruft – in `main` und im Test. Das liegt daran, dass nirgends verlangt wird, dass `*countingStore` ein `Store` ist; ohne Aufrufer würde das Fehlen gar nicht auffallen.

Die Lösung ergänzt die weitergeleitete Methode und eine Zusicherung zur Übersetzungszeit:

```go
type countingStore struct {
	inner Store
	saves int
}

func (c *countingStore) Save(url string, status int) error {
	c.saves++
	return c.inner.Save(url, status)
}

// Count only forwards. With an embedded Store it came for free.
func (c *countingStore) Count() int { return c.inner.Count() }

// countingStore must stay usable wherever a Store is expected.
var _ Store = (*countingStore)(nil)
```

Im Test wird aus `&countingStore{Store: inner}` ein `&countingStore{inner: inner}`, in `main` entsprechend.

```bash
git apply loesungen/04-go-denkweise/uebung-1.patch
go test ./beispiele/04-go-denkweise/embedding
```

Mit der Zeile `var _ Store = …`, aber ohne `Count`, käme die Meldung an der richtigen Stelle und mit der richtigen Begründung:

```
./main.go:73:15: cannot use (*countingStore)(nil) (value of type *countingStore) as Store value in variable declaration: *countingStore does not implement Store (missing method Count)
```

**Warum so:** Beim Einbetten werden alle Methoden des eingebetteten Interfaces befördert; der Wrapper überschreibt nur, was er anders machen will. Mit einem benannten Feld musst du jede Methode selbst weiterleiten – bei zwei Methoden kein Problem, bei einem Interface mit zehn Methoden viel Schreibarbeit. Dafür ist das benannte Feld ausdrücklicher: Wenn `Store` eine neue Methode bekommt, leitet der eingebettete Wrapper sie stillschweigend weiter, der mit benanntem Feld bricht beim Compilieren und zwingt dich zu entscheiden, ob die neue Methode auch gezählt werden soll. Welche Variante besser ist, hängt davon ab, ob „alles andere einfach durchreichen“ wirklich gewollt ist. Die Zeile `var _ Store = (*countingStore)(nil)` ist dasselbe Muster wie `_ runnerStore = (*MemStore)(nil)` im Interfaces-Beispiel: Sie kostet nichts zur Laufzeit und verlegt die Fehlermeldung von den Aufrufern zur Typdefinition.

## Übung 2: `WithFallback` (Kern)

```go
// WithFallback asks primary first. Only if primary is unreachable (0) or
// reports a server error (5xx) does it ask fallback and return its answer.
func WithFallback(primary, fallback Checker) Checker {
	return CheckFunc(func(url string) int {
		if status := primary.Check(url); status != 0 && status < 500 {
			return status
		}
		return fallback.Check(url)
	})
}
```

Vor der Lösung schlägt der vorbereitete Test so fehl:

```
./uebung_test.go:33:11: undefined: WithFallback
```

Danach laufen alle vier Fälle durch:

```bash
git apply loesungen/04-go-denkweise/uebung-2.patch
go test -tags uebung ./beispiele/04-go-denkweise/interfaces
```

```
--- PASS: TestWithFallback (0.00s)
    --- PASS: TestWithFallback/primary_ok (0.00s)
    --- PASS: TestWithFallback/primary_redirect (0.00s)
    --- PASS: TestWithFallback/primary_server_error (0.00s)
    --- PASS: TestWithFallback/primary_unreachable (0.00s)
```

**Warum so:** Die Form ist dieselbe wie bei `WithRetry`: eine Funktion, die Checker entgegennimmt und einen Checker zurückgibt, gebaut aus einer Closure, die `CheckFunc` zum Interface macht. Dadurch lassen sich beide kombinieren, etwa `WithFallback(WithRetry(primary, 2), fallback)`, ohne dass einer vom anderen weiß. `fallback` wird nur im Fehlerfall aufgerufen, nicht vorsorglich parallel – das spart Anfragen und ist genau das, was der vierte Testfall prüft. Ob man auch bei `4xx` auf den Fallback ausweichen sollte, ist eine fachliche Frage: Ein 404 sagt, dass der Server antwortet, aber die Ressource fehlt, und ein zweiter Server würde daran meist nichts ändern. Die Aufgabe legt die Grenze bei 0 und ab 500, und der Test hält sie fest.

## Übung 3: `WriteCSV` als dritter Konsument (Kern)

```go
// csvStore happens to list the same methods as reportStore. It is still
// its own interface: the export and the report may need different things
// tomorrow, and neither should have to change because of the other.
type csvStore interface {
	Targets() []Target
	Results(targetID int) []int
}

func WriteCSV(w io.Writer, store csvStore) error {
	cw := csv.NewWriter(w)
	for _, t := range store.Targets() {
		for _, status := range store.Results(t.ID) {
			if err := cw.Write([]string{t.URL, strconv.Itoa(status)}); err != nil {
				return err
			}
		}
	}
	cw.Flush()
	return cw.Error()
}
```

In `run` kommt nach dem Bericht der CSV-Export, und die Liste der Zusicherungen bekommt `_ csvStore = (*MemStore)(nil)`. Die Ausgabe von `go run`:

```
https://example.com        [200]
https://flaky.example      [200]
https://example.com,200
https://flaky.example,200
```

Ein neuer Test `TestWriteCSV` prüft die Ausgabe Zeile für Zeile, darunter eine URL mit Komma, die `encoding/csv` in Anführungszeichen setzt.

```bash
git apply loesungen/04-go-denkweise/uebung-3.patch
go test ./beispiele/04-go-denkweise/interfaces
```

`MemStore` musste nicht geändert werden, keine Zeile. Er hat die beiden Methoden schon, und weil Interfaces implizit erfüllt werden, passt er ohne Weiteres auch zu einem Interface, das es beim Schreiben von `MemStore` noch gar nicht gab.

**Warum so:** `csvStore` ist inhaltlich identisch mit `reportStore`, und `WriteCSV(w io.Writer, store reportStore)` würde genauso kompilieren. Ein eigenes Interface ist trotzdem die Go-typische Wahl, weil jeder Konsument festhält, was *er* braucht; wird der Bericht später um eine Methode erweitert, bleibt der Export davon unberührt. Bei zwei Methoden kostet das vier Zeilen. `encoding/csv` statt `fmt.Fprintf(w, "%s,%d\n", …)` lohnt sich, sobald ein Feld ein Komma oder Anführungszeichen enthalten kann – bei nutzergelieferten URLs ist das nicht ausgeschlossen. Wichtig sind `Flush` und `cw.Error()` am Ende: `csv.Writer` puffert, und ein Schreibfehler zeigt sich unter Umständen erst beim Leeren des Puffers.

## Übung 4: `byCode` ohne Konstruktor (Profi)

```go
type Stats struct {
	mu     sync.Mutex
	up     int
	down   int
	byCode map[int]int
}

func (s *Stats) Record(status int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byCode == nil {
		s.byCode = make(map[int]int)
	}
	s.byCode[status]++
	// … up/down as before
}

// CodeCount reports how often status was recorded. Reading a nil map is
// fine and yields 0, so it needs no nil check.
func (s *Stats) CodeCount(status int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.byCode[status]
}
```

`TestStatsZeroValue` prüft zusätzlich, dass `CodeCount(200)` auf einem frischen `var s Stats` `0` liefert, und nach den vier `Record`-Aufrufen die Zählung `200: 2, 500: 1, 204: 1, 404: 0`.

```bash
git apply loesungen/04-go-denkweise/uebung-4.patch
go test -race ./beispiele/04-go-denkweise/embedding
```

Ohne die drei Zeilen, die die Map anlegen, bricht der Test mit einer Panic ab:

```
--- FAIL: TestStatsZeroValue (0.00s)
panic: assignment to entry in nil map [recovered, repanicked]
…
github.com/casoon/go-tutorial/beispiele/04-go-denkweise/embedding.(*Stats).Record(…)
	…/beispiele/04-go-denkweise/embedding/main.go:81 +0xb4
```

**Warum so:** Eine `nil`-Map lässt sich lesen, aber nicht beschreiben. Deshalb reicht es, die Map an der einzigen Stelle anzulegen, die schreibt – beim ersten `Record`. `CodeCount` braucht keinen Sonderfall, weil ein Lesezugriff auf eine `nil`-Map den Zero Value liefert. Das Anlegen steht innerhalb des Locks; außerhalb wäre es selbst ein Data Race, wenn zwei Goroutines gleichzeitig zum ersten Mal `Record` aufrufen. Die Alternative wäre ein `NewStats()`, das die Map anlegt – dann funktionierte `var stats Stats` aber nicht mehr, und genau das wäre die Falle: Jeder, der den Konstruktor vergisst, bekommt erst zur Laufzeit eine Panic. Statt `CodeCount` einfach die Map selbst herauszugeben, wäre dagegen keine gute Idee: Der Aufrufer hielte dann eine Referenz auf die interne Map und könnte sie ohne Lock lesen, während `Record` sie verändert.
