# Lösungen zu Teil 11: Testing

Zu den Aufgaben: [Teil 11](../kapitel/11-testing.md#übungen)

## Übung 1: zwei eigene Tabellenfälle (Aufwärmen)

```go
{name: "keeps port", in: "https://example.com:8443/status", want: "https://example.com:8443/status"},
{name: "keeps query", in: "https://example.com/health?full=1&lang=de", want: "https://example.com/health?full=1&lang=de"},
```

Mit einem absichtlich falschen `want` – hier `"https://example.com/health?full=1"`, als hätte man erwartet, dass nur der erste Parameter übrig bleibt – meldet `go test ./internal/monitor`:

```text
--- FAIL: TestNormalizeURL (0.00s)
    --- FAIL: TestNormalizeURL/keeps_query (0.00s)
        monitor_test.go:46: NormalizeURL("https://example.com/health?full=1&lang=de") = "https://example.com/health?full=1&lang=de", want "https://example.com/health?full=1"
FAIL
```

Die Meldung ist ohne Blick in den Testcode verständlich: Der Subtest-Name sagt, welcher Fall es ist, danach stehen Aufruf mit Eingabe, tatsächliches und erwartetes Ergebnis. Nur die Zeilennummer zeigt auf die gemeinsame Prüfzeile in der Schleife, nicht auf die Tabellenzeile – deshalb ist ein sprechender `name` wichtig.

```bash
git apply loesungen/11-testing/uebung-1.patch
go test ./...
```

**Warum so:** Die beiden Fälle halten Verhalten fest, auf das sich andere Teile verlassen: Der Port gehört zum Ziel, und die Query bleibt vollständig und in ihrer Reihenfolge erhalten (`NormalizeURL` sortiert und kürzt sie nicht). Andere sinnvolle Fälle wären ein IPv6-Host wie `http://[::1]:8080/` oder Großbuchstaben im Host – letzterer würde zeigen, dass `NormalizeURL` den Host *nicht* kleinschreibt, also `https://Example.com` und `https://example.com` zwei verschiedene Ziele sind. Ob das gewollt ist, ist eine fachliche Frage; ein Tabellenfall macht sie wenigstens sichtbar.

## Übung 2: Längenprüfung rückgängig machen und eine zweite Fuzz-Eigenschaft (Kern)

Rückgängig gemacht heißt: Am Ende von `NormalizeURL` steht wieder `return u.String(), nil` ohne Prüfung des Ergebnisses. Ein normaler `go test ./internal/monitor -v` zeigt:

```text
--- FAIL: TestNormalizeURL (0.00s)
    --- FAIL: TestNormalizeURL/too_long_after_encoding (0.00s)
--- PASS: FuzzNormalizeURL (0.00s)
    --- PASS: FuzzNormalizeURL/seed#0 (0.00s)
    --- PASS: FuzzNormalizeURL/seed#1 (0.00s)
    --- PASS: FuzzNormalizeURL/seed#2 (0.00s)
    --- PASS: FuzzNormalizeURL/4b9a2fad74729647 (0.00s)
    --- PASS: FuzzNormalizeURL/fe74701a910db376 (0.00s)
FAIL
```

Es schlägt nur der Tabellenfall „too long after encoding“ fehl. Ein normaler Lauf findet ihn, weil ein Fuzz-Test ohne `-fuzz` wie ein Tabellentest über seinen Seed-Korpus läuft, und weil der Fehler nach dem Fund zusätzlich als lesbarer Tabellenfall festgehalten wurde.

Wie Teil 11 erwähnt, löst die Korpusdatei `4b9a2fad74729647` – die ursprünglich gefundene Eingabe – den Fehler nicht mehr aus. Sie enthält Leerzeichen, und die später ergänzte Prüfung auf Leerraum lehnt sie ab, bevor die Längenprüfung überhaupt erreicht wird. Als Regressionstest für die Länge ist sie damit wertlos geworden; geschützt hat die Korrektur nur noch der Tabellenfall. Das ist eine allgemeine Eigenschaft von Fuzz-Korpora: Eine gespeicherte Eingabe prüft einen bestimmten Pfad nur so lange, wie die Prüfungen davor sie durchlassen.

Die zweite Eigenschaft kommt in denselben Fuzz-Test, damit sie den vorhandenen Korpus mitbenutzt. Dazu ein Seed, der die Länge wieder abdeckt:

```go
// Grows from 720 to 2120 bytes when percent-encoded. The corpus file
// 4b9a2fad74729647 no longer reaches the length check, because the
// whitespace check rejects it first.
f.Add("https://example.com/" + strings.Repeat("\xa1", 700))
f.Fuzz(func(t *testing.T, raw string) {
	got, err := NormalizeURL(raw)
	if err != nil {
		return
	}
	// What we accept is what gets stored, so it must fit the limit.
	if len(got) > MaxURLLength {
		t.Fatalf("NormalizeURL(%q) returned %d bytes, limit is %d", raw, len(got), MaxURLLength)
	}
	// ... idempotence check as before
})
```

Mit rückgängig gemachter Korrektur scheitert jetzt auch der Fuzz-Test im normalen Lauf (`FuzzNormalizeURL/seed#3`). Im Fuzz-Modus fand er den Fehler, noch bevor er neue Eingaben erzeugte – eine Eingabe aus dem lokalen Fuzz-Cache genügte, auf die Grenze minimiert:

```text
--- FAIL: FuzzNormalizeURL (14.98s)
    --- FAIL: FuzzNormalizeURL (0.00s)
        monitor_test.go:105: NormalizeURL("http://\xdc\xdc\xdc…") returned 2049 bytes, limit is 2048
```

Mit der Korrektur lief der Fuzzer eine Minute ohne Fund:

```bash
go test -run=^$ -fuzz=FuzzNormalizeURL -fuzztime=60s ./internal/monitor
```

```text
fuzz: elapsed: 1m0s, execs: 24645427 (254553/sec), new interesting: 14 (total: 459)
PASS
```

```bash
git apply loesungen/11-testing/uebung-2.patch
go test ./...
```

Fuzz-Läufe schreiben gefundene Eingaben nach `testdata/fuzz/`. Starte sie deshalb in einem Arbeitsbaum, in dem du solche Dateien bewusst übernimmst oder wieder löschst.

**Warum so:** Die Idempotenz-Eigenschaft hätte den Fehler auch gefunden, aber über einen Umweg („rejected on second pass“). Die neue Eigenschaft sagt direkt, was gelten soll, und ihre Meldung nennt die Länge. Sie schützt außerdem einen Fall, den Idempotenz nicht abdeckt: Fiele auch die frühe Prüfung der Eingabe weg, würden beide Durchläufe dieselbe zu lange URL liefern – idempotent, aber falsch. Ob man sie in einen bestehenden Fuzz-Test schreibt oder einen eigenen anlegt, ist eine Abwägung: Im selben Test teilt sie sich Korpus und Laufzeit, ein eigener Test lässt sich gezielt einzeln fuzzen, weil `-fuzz` immer nur ein Ziel auf einmal nimmt.

## Übung 3: t.Parallel in TestNormalizeURL (Kern)

```go
func TestNormalizeURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		// ...
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeURL(tt.in)
			// ...
```

```bash
git apply loesungen/11-testing/uebung-3.patch
go test -race -count=5 ./internal/monitor
```

```text
ok  	github.com/casoon/go-tutorial/internal/monitor	1.310s
```

Das ist hier sicher, weil jeder Fall nur seine eigene Zeile der Tabelle liest und `NormalizeURL` keinen gemeinsamen Zustand hat. Seit Go 1.22 bekommt jede Schleifeniteration ein eigenes `tt`, also ist auch die früher übliche Zeile `tt := tt` nicht mehr nötig.

In `TestTargetsAPI` wäre dasselbe falsch, weil die Fälle aufeinander aufbauen und denselben Server mit derselben Datenbank benutzen. Mit `t.Parallel()` in den Subtests laufen sie gleichzeitig, und die Reihenfolge ist Zufall. Ausprobiert, sechs Läufe hintereinander:

```text
        httpapi_test.go:103: status = 201, want 409 (body: {"id":1,"url":"https://example.com",…}
        httpapi_test.go:103: status = 204, want 404 (body: )
        httpapi_test.go:103: status = 404, want 200 (body: {"error":"target not found"}
        httpapi_test.go:103: status = 404, want 200 (body: {"error":"target not found"}
FAIL
```

Vier der sechs Läufe scheiterten, jeweils mit anderen Fällen, zwei waren grün. „get“ findet das Ziel nicht, weil „delete“ schon gelaufen ist; „delete again“ bekommt 204, weil es vor „delete“ dran war; und „duplicate“ legt nach dem Löschen ein neues Ziel an, das wieder die ID 1 bekommt.

**Warum so:** `t.Parallel()` ist eine Zusage, dass ein Test keinen Zustand mit anderen teilt. Für reine Funktionen wie `NormalizeURL` stimmt das, für einen Szenario-Test per Definition nicht. Gefährlich ist vor allem, dass der falsche Umbau nicht immer scheitert: Zwei grüne Läufe von sechs hätten ihn durch ein Review bringen können. Wer die API-Fälle parallel haben will, muss sie unabhängig machen – jeder Subtest mit eigenem `newServer(t)` und eigenen Daten –, und dann ist es kein Szenario-Test mehr.

## Übung 4: Scheduler-Test mit fehlschlagendem SaveResult (Profi)

Statt eines zweiten Fakes bekommt `fakeStore` zwei Felder: einen Fehler, den `SaveResult` zurückgibt, wenn er gesetzt ist, und einen Zähler für alle Aufrufe:

```go
type fakeStore struct {
	mu       sync.Mutex
	pending  []monitor.Target
	saved    []monitor.Result
	saveErr  error // if set, SaveResult fails with it
	attempts int   // SaveResult calls, successful or not
}

func (f *fakeStore) SaveResult(_ context.Context, r monitor.Result) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, r)
	return nil
}
```

Der Test prüft beides, was die Übung verlangt: dass jedes Ergebnis einen Speicherversuch bekommt und dass der Scheduler danach weiterarbeitet. Für das Zweite legt er nach den ersten Fehlern zwei neue Ziele nach, die erst beim nächsten Tick abgeholt werden:

```go
store := &fakeStore{saveErr: errors.New("disk full")}
// ... five pending targets
failuresBefore := saveFailures.Value()
s := New(store, fakeChecker{}, discardLogger(), 3, time.Second)
// ... start s.Run in a goroutine

// 5 checks of 100ms on 3 workers need 2 rounds = 200ms.
time.Sleep(200 * time.Millisecond)
synctest.Wait()
// attemptCount() == 5

// Failed saves must not stop the scheduler: targets that become
// due later are claimed on the next tick (1s) and checked (100ms).
store.add(monitor.Target{ID: 6}, monitor.Target{ID: 7})
time.Sleep(900 * time.Millisecond)
synctest.Wait()
// attemptCount() == 7, savedCount() == 0
if got := saveFailures.Value() - failuresBefore; got != 7 {
	t.Errorf("upwatch_save_failures_total grew by %d, want 7", got)
}
```

```bash
git apply loesungen/11-testing/uebung-4.patch
go test -race -count=5 ./internal/scheduler
```

Den Zähler prüft der Test über die Differenz vor und nach dem Lauf. Das ist nötig, weil `saveFailures` eine Variable auf Package-Ebene ist, die bei `expvar.NewInt` einmal pro Prozess registriert wird. Prüft man stattdessen den absoluten Wert, scheitert schon der zweite Durchlauf von `go test -count=2`:

```text
--- FAIL: TestRunContinuesWhenSaveFails (0.00s)
    scheduler_test.go:178: upwatch_save_failures_total grew by 14, want 7
```

(Die Meldung ist im Experiment unverändert geblieben; geprüft wurde dort der absolute Wert, der nach zwei Läufen bei 14 steht.)

Problematisch an diesem Ansatz ist, dass globaler Zustand den Test an seine Umgebung bindet. Die Differenz stimmt nur, solange kein anderer Test im selben Package gleichzeitig Speicherfehler erzeugt; ein späteres `t.Parallel()` irgendwo im Package macht den Test unzuverlässig. Zurücksetzen lässt sich der Zähler nur mit `Set(0)`, was dasselbe Problem hat, und ein zweites `expvar.NewInt` mit demselben Namen panickt, also kann sich kein Test einen eigenen Zähler anlegen.

**Warum so:** Den Fake zu erweitern statt einen zweiten zu schreiben, hält die Ziel-Logik von `ClaimDue` an einer Stelle. Die zweite Phase mit nachgelegten Zielen ist der eigentliche Beweis für „läuft weiter“ – ohne sie könnte der Scheduler nach dem ersten Fehler stehen geblieben sein, und der Test würde es nicht merken. Die saubere Lösung für das Zählerproblem wäre, die Metriken wie Store und Logger über `New` hineinzureichen (etwa ein kleines Struct mit drei `*expvar.Int`, die `main` registriert und der Test unregistriert erzeugt). Das ändert aber die Signatur von `New`, und für drei Zähler ist der Differenz-Trick ein vertretbarer Kompromiss, solange man seine Grenze kennt.
