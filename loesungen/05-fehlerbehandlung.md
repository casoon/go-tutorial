# Lösungen zu Teil 05: Fehlerbehandlung

Zu den Aufgaben: [Teil 05](../kapitel/05-fehlerbehandlung.md#übungen)

Übung 1 ist ein Experiment ohne Patch, die anderen drei ändern Code und haben je einen Patch. Die Ausgaben stammen aus echten Läufen auf einem Apple-M-Rechner mit Go 1.27.1.

## Übung 1: `%w` durch `%v` ersetzen (Aufwärmen)

Die Änderung in `Describe`:

```go
return "", fmt.Errorf("describe: %v", err)
```

```bash
go test ./beispiele/05-fehlerbehandlung/...
```

```
--- FAIL: TestStatusFor (0.00s)
    --- FAIL: TestStatusFor/not_found_through_two_wraps (0.00s)
        main_test.go:29: StatusFor(describe: target 42: not found) = 500, want 404
FAIL
FAIL	github.com/casoon/go-tutorial/beispiele/05-fehlerbehandlung/fehler	0.214s
```

Genau ein Testfall schlägt fehl: „not found through two wraps“. Der Repository-Fehler `target 42: not found` ist noch korrekt mit `%w` verpackt, aber `Describe` formatiert ihn jetzt nur noch als Text in einen neuen Fehler. Dieser neue Fehler hat keine `Unwrap`-Methode, also findet `errors.Is(err, ErrNotFound)` in `StatusFor` nichts mehr, und der Fall landet im `default`-Zweig: 500 statt 404. Eine abgeschnittene Stelle reicht, damit die ganze Kette darunter unsichtbar wird.

Bemerkenswert ist, welcher Test *nicht* fehlschlägt: `TestWrappedMessage` vergleicht die Meldung `describe: target 7: not found`, und die ist mit `%v` Zeichen für Zeichen dieselbe. Am Text kann man den Unterschied zwischen `%w` und `%v` nicht sehen.

Danach die Änderung rückgängig machen, etwa mit `git restore beispiele/05-fehlerbehandlung/fehler/main.go`.

**Warum so:** Das Experiment zeigt, warum Tests auf Fehler mit `errors.Is` und `errors.As` prüfen statt auf Strings. Ein String-Vergleich hätte diese Regression nicht bemerkt, obwohl sie im Betrieb aus jedem 404 einen 500 gemacht hätte. Es zeigt auch, dass `%v` keine harmlose Stilfrage ist: Eine einzige Schicht, die mit `%v` verpackt, kappt die Kette für alle darüberliegenden Schichten. Das ist an einer API-Grenze gewollt (wie in `Opaque` oder `readJSON`), mitten in einem Paket aber fast immer ein Versehen.

## Übung 2: `errors.AsType` in `handleError` (Aufwärmen)

Ja, die Tests laufen weiter durch. Weil `AsType` den gefundenen Wert und ein `bool` zurückgibt, passt es nicht direkt in einen `case`-Ausdruck. Die kleinste Änderung zieht den Aufruf vor das `switch`:

```go
func (a *API) handleError(w http.ResponseWriter, r *http.Request, err error) {
	ve, isValidation := errors.AsType[*monitor.ValidationError](err)
	switch {
	case isValidation:
		writeError(w, http.StatusBadRequest, ve.Error())
	case errors.Is(err, monitor.ErrNotFound):
	// ... unchanged
```

```bash
git apply loesungen/05-fehlerbehandlung/uebung-2.patch
go test -race ./internal/httpapi
```

```
ok  	github.com/casoon/go-tutorial/internal/httpapi	1.364s
```

Die bestehenden Testfälle „invalid url“ (`ftp://x` → 400 mit „scheme“) und „duplicate“ (409) decken beide Zweige vor und nach dem geänderten Fall ab; einen neuen Test braucht die Änderung nicht, weil sich das Verhalten nicht ändert.

**Warum so:** `errors.AsType` und `errors.As` sind gleichwertig, der Unterschied ist Lesbarkeit. Mit `AsType` entfällt die Hilfsvariable, und der Typ steht genau einmal im Code; `go vet` muss nicht mehr prüfen, ob du `&ve` statt `ve` übergeben hast, weil der Compiler den Typparameter schon auf `error` beschränkt. Der Preis in dieser Form: `AsType` läuft jetzt für jeden Fehler, auch wenn ein späterer Fall gemeint ist – bei einer Kette von zwei, drei Gliedern bedeutungslos. Die Alternative ist ein `if ve, ok := errors.AsType[…](err); ok { …; return }` vor dem `switch`, das aber den einen Ort, an dem alle Fälle nebeneinander stehen, in zwei Blöcke teilt. In bestehendem Code lohnt sich die Umstellung allein nicht; in neuem Code, der Go 1.26 voraussetzt, ist `AsType` die angenehmere Form.

## Übung 3: `sql.ErrNoRows` zusätzlich einpacken (Kern)

```go
t, err := scanTarget(row)
if errors.Is(err, sql.ErrNoRows) {
	return monitor.Target{}, fmt.Errorf("target %d: %w (%w)", id, monitor.ErrNotFound, err)
}
```

Der Patch enthält dazu einen Test, der festhält, was sich geändert hat:

```bash
git apply loesungen/05-fehlerbehandlung/uebung-3.patch
go test -v -run TestTargetLeaksNoRows ./internal/sqlitestore
```

```
=== RUN   TestTargetLeaksNoRows
    store_test.go:168: target 99: not found (sql: no rows in result set)
--- PASS: TestTargetLeaksNoRows (0.00s)
```

Mit zwei `%w` liefert `fmt.Errorf` einen Fehler mit `Unwrap() []error`, und `errors.Is` findet jetzt beide: `monitor.ErrNotFound` *und* `sql.ErrNoRows`. Alle bestehenden Tests bleiben grün, auch die API antwortet weiterhin mit 404, denn `handleError` fragt nur nach dem Domänenfehler.

Wer könnte sich darauf verlassen? Jeder Aufrufer von `Store.Target`: allen voran `handleError` in [httpapi.go](../internal/httpapi/httpapi.go), wo jemand irgendwann `case errors.Is(err, sql.ErrNoRows):` ergänzt, weil es funktioniert; außerdem `getTarget` und `listResults`, die `Target` direkt aufrufen, und jeder künftige Aufrufer außerhalb des Pakets, etwa ein CLI. Sobald so eine Zeile existiert, ist `sql.ErrNoRows` stillschweigend Teil des Vertrags von `Target`.

Unerwünscht ist das aus drei Gründen. Die HTTP-Schicht müsste `database/sql` importieren und wüsste damit, wie gespeichert wird. Jede andere Store-Implementierung – ein In-Memory-Store für Tests, ein Store für eine andere Datenbank – müsste `sql.ErrNoRows` nachahmen, obwohl sie gar kein SQL spricht, sonst verhält sich der Handler mit ihr anders. Und der Store könnte seine eigene Implementierung nicht mehr ändern, ohne Aufrufer zu brechen: Holt er das Ziel künftig aus einem Cache, gibt es kein `sql.ErrNoRows` mehr, und der Handler-Zweig dafür ist plötzlich tot.

**Warum so:** Die Übung ist ein Gedankenexperiment; der Patch zeigt die Änderung und belegt ihre Wirkung, als Lösung für upwatch übernehmen solltest du ihn nicht. Die Schreibweise `%w (%w)` hält die Meldung lesbar, `%w: %w` ginge genauso. Mehrere `%w` sind dort richtig, wo ein Fehler wirklich zu zwei dokumentierten Kategorien gehört, etwa „Timeout“ und „Ziel nicht erreichbar“ innerhalb desselben Pakets. An einer Paketgrenze gilt die Regel aus dem Kapitel: übersetzen statt durchreichen, damit die Implementierung austauschbar bleibt. Den Treiberfehler im Log zu haben, wäre dagegen legitim – dafür reicht `%v`, das den Text behält, ohne die Kette zu öffnen.

## Übung 4: `*FieldError` in `config.Load` (Profi)

Der neue Typ trägt Variablenname und Wert und dazu, was erwartet wurde. Damit bleibt die Meldung exakt dieselbe wie vorher:

```go
// FieldError reports an environment variable whose value cannot be used.
// Callers can collect every FieldError from the joined error of Load.
type FieldError struct {
	Key   string // name of the environment variable
	Value string // the value as found in the environment
	Want  string // what would have been accepted
}

func (e *FieldError) Error() string {
	return fmt.Sprintf("%s: want %s, got %q", e.Key, e.Want, e.Value)
}
```

`num` gibt ihn zurück, und weil die Aufgabe *alle* fehlerhaften Variablen einsammeln will, tun `dur` und `boolean` dasselbe:

```go
errs = append(errs, &FieldError{Key: key, Value: v, Want: "a positive integer"})
```

Der Test sammelt die Namen ein. `errors.As` auf den ganzen `Join`-Fehler hielte beim ersten Treffer an; deshalb wird der Fehler erst über `Unwrap() []error` in seine Teile zerlegt, und `errors.As` prüft jeden Teil einzeln:

```go
func fieldErrors(err error) []*FieldError {
	parts := []error{err}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		parts = joined.Unwrap()
	}
	var out []*FieldError
	for _, part := range parts {
		var fe *FieldError
		if errors.As(part, &fe) {
			out = append(out, fe)
		}
	}
	return out
}

func TestLoadCollectsFieldErrors(t *testing.T) {
	_, err := Load(env(map[string]string{
		"UPWATCH_WORKERS":   "zero",
		"UPWATCH_TICK":      "-1s",
		"UPWATCH_LOG_JSON":  "yes please",
		"UPWATCH_LOG_LEVEL": "loud", // not a FieldError, must be skipped
	}))
	// ... keys must be UPWATCH_WORKERS, UPWATCH_TICK, UPWATCH_LOG_JSON
```

```bash
git apply loesungen/05-fehlerbehandlung/uebung-4.patch
go test -race ./internal/config
```

```
ok  	github.com/casoon/go-tutorial/internal/config	1.194s
```

Von außen sieht man keinen Unterschied, die Meldung beim Start ist unverändert:

```
$ UPWATCH_WORKERS=zero UPWATCH_TICK=-1s go run ./cmd/upwatch
upwatch: config: UPWATCH_WORKERS: want a positive integer, got "zero"
UPWATCH_TICK: want a positive duration like 5s, got "-1s"
exit status 1
```

**Warum so:** Das Feld `Want` ist nicht verlangt, aber ohne es müsste jede der drei Closures ihren eigenen Fehlertyp haben oder die Meldung würde ungenauer. `UPWATCH_LOG_LEVEL` bleibt bewusst bei `fmt.Errorf("…: %w", err)`, weil dort der Fehler von `slog.Level.UnmarshalText` mehr sagt als ein fester Text; der Test belegt nebenbei, dass solche Teile beim Einsammeln einfach übersprungen werden. Die Zerlegung geht nur eine Ebene tief, weil `Load` genau eine Ebene erzeugt; ein allgemeiner Helfer müsste rekursiv in jedes `Unwrap() []error` und `Unwrap() error` hineinsteigen. Statt `errors.As` mit Hilfsvariable könnte die Schleife auch `errors.AsType[*FieldError](part)` verwenden (siehe Übung 2). Wofür das Ganze? Ein Programm, das `config.Load` aufruft – ein Installer, ein Admin-Werkzeug –, kann jetzt gezielt die fehlerhaften Felder markieren, statt Meldungstexte zu parsen.
