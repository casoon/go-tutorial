# Lösungen zu Teil 12: Logging, Konfiguration, Observability

Zu den Aufgaben: [Teil 12](../kapitel/12-logging-konfiguration-observability.md#übungen)

## Übung 1: Request-ID in der Middleware (Kern)

Die ID entsteht in `logRequests`, weil das die äußerste Middleware ist: Jeder Request läuft durch sie, auch einer, der später panickt. Von dort wandert sie auf drei Wegen weiter – als Response-Header, als Attribut in der Logzeile und im Context des Requests:

```go
// requestIDKey is the context key for the request ID. An unexported type
// cannot collide with keys from other packages.
type requestIDKey struct{}

// requestID returns the ID that logRequests stored in ctx, or "".
func requestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// log returns the API logger with the request ID of r attached. Handlers
// that log use it, so their lines can be matched with the request line.
func (a *API) log(r *http.Request) *slog.Logger {
	return a.logger.With("request_id", requestID(r.Context()))
}

func (a *API) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		id := rand.Text() // crypto/rand
		w.Header().Set("X-Request-Id", id)
		r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))

		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		a.logger.Info("request",
			"request_id", id,
			"method", r.Method,
			// ...
```

Damit ist auch die Frage nach den Handlern beantwortet, die selbst loggen: Sie holen sich die ID nicht einzeln, sondern rufen `a.log(r)` statt `a.logger` auf. Das betrifft in upwatch drei Stellen – `serverError`, `readyz` und die Panic-Meldung in `recoverPanics`. Ein Test schickt `GET /targets` an einen `failingStore`, damit der Handler eine eigene Zeile schreibt, und prüft, dass beide Logzeilen dieselbe ID tragen wie der Header und dass der nächste Request eine neue bekommt.

Ein Lauf mit dem gebauten Binary:

```text
HTTP/1.1 404 Not Found
X-Request-Id: KRVTKFCS6KFMGJBGP76Q2MRBQA
```

```text
time=2026-10-11T11:47:17.279+02:00 level=INFO msg=request request_id=KRVTKFCS6KFMGJBGP76Q2MRBQA method=GET path=/targets/99 status=404 duration=320.083µs
```

```bash
git apply loesungen/12-logging-konfiguration-observability/uebung-1.patch
go test ./...
```

**Warum so:** Im Context liegt die ID, nicht ein fertiger Logger. So bleibt der Context ein Träger von Request-Daten, die auch andere Stellen brauchen können (etwa ein ausgehender Header beim Aufruf eines anderen Dienstes), und der Logger kommt weiter explizit aus der `API`-Struktur. Die Alternative – `logger.With(…)` einmal in der Middleware bauen und im Context ablegen – spart ein paar Allokationen pro Logzeile, versteckt aber eine Abhängigkeit im Context, die man der Signatur nicht ansieht. `rand.Text` (seit Go 1.24) liefert 26 Zeichen aus Base32 ohne Fehlerbehandlung; für eine ID reicht das, eine UUID-Bibliothek ist nicht nötig. Eine vom Client mitgeschickte `X-Request-Id` übernimmt die Lösung bewusst nicht: Hinter einem eigenen Reverse Proxy, der die ID setzt, wäre das sinnvoll, von beliebigen Clients aber nur nach einer Prüfung von Länge und Zeichensatz, damit niemand Logzeilen mit eigenen Inhalten füllt.

## Übung 2: Zähler für Timeouts (Kern)

```go
var (
	checksTotal   = expvar.NewInt("upwatch_checks_total")
	checksDown    = expvar.NewInt("upwatch_checks_down_total")
	checksTimeout = expvar.NewInt("upwatch_checks_timeout_total")
	saveFailures  = expvar.NewInt("upwatch_save_failures_total")
)

// in write:
checksTotal.Add(1)
// check.Checker reports a timed-out check with exactly this text.
if r.Err == "timeout" {
	checksTimeout.Add(1)
}
```

Ein Scheduler-Test mit einem `timeoutChecker`, der nach 100 ms ein Ergebnis mit `Err: "timeout"` liefert, prüft per Differenz, dass drei Timeouts drei Mal gezählt werden – und zusätzlich als „down“, denn ein Timeout ist auch ein fehlgeschlagener Check.

Für den echten Lauf antwortet die langsame Website aus Teil 06 nach drei Sekunden, upwatch gibt jedem Check aber nur eine:

```bash
DIR=$(mktemp -d)
go build -o "$DIR/upwatch" ./cmd/upwatch
go build -o "$DIR/langsam" ./beispiele/06-concurrency/langsam
"$DIR/langsam" -addr 127.0.0.1:18320 -delay 3s &
UPWATCH_ADDR=127.0.0.1:18322 UPWATCH_DEBUG_ADDR=127.0.0.1:18321 UPWATCH_DB="$DIR/u.db" \
UPWATCH_TICK=1s UPWATCH_CHECK_TIMEOUT=1s UPWATCH_ALLOW_PRIVATE_TARGETS=true "$DIR/upwatch" &
curl -s -X POST localhost:18322/targets -d '{"url":"http://127.0.0.1:18320/","interval_seconds":10}'
sleep 2.5
curl -s localhost:18321/debug/vars | grep upwatch_
```

```text
"upwatch_checks_down_total": 1,
"upwatch_checks_timeout_total": 1,
"upwatch_checks_total": 1,
"upwatch_save_failures_total": 0
```

Im Log steht dazu `msg="target down" target_id=1 status=0 err=timeout duration=1.00033225s`.

```bash
git apply loesungen/12-logging-konfiguration-observability/uebung-2.patch
go test ./...
```

**Warum so:** Der Zähler gehört in den Writer, neben die beiden anderen, weil dort jedes Ergebnis genau einmal vorbeikommt; in den Workern wäre er ebenso korrekt, aber auf zwei Orte verteilt. Die Schwäche der Lösung ist der Vergleich mit dem String `"timeout"`: Der Scheduler verlässt sich auf einen Text, den `check.describe` erzeugt, und niemand bemerkt es, wenn sich der Text ändert. Sauberer wäre eine Konstante in `monitor` (etwa `monitor.ErrTextTimeout`), die beide Packages benutzen, oder ein eigenes Feld im `Result`. Die Übung verlangt den kleinsten Schritt, deshalb steht der Kommentar an der Vergleichsstelle. Mit dem Prometheus-Client würde man statt eines vierten Zählers einen Zähler mit Label `reason` anlegen; `expvar` kennt keine Labels.

## Übung 3: stop() direkt nach dem Signal (Profi)

```go
case <-ctx.Done():
	// Restore the default signal handling: a second Ctrl+C or SIGTERM
	// during shutdown now kills the process instead of being swallowed.
	stop()
	logger.Info("shutting down")
}
```

Damit das Herunterfahren lange genug dauert, um ein zweites Signal zu schicken, braucht es einen Request, der nicht fertig wird. Ein kleines Skript öffnet eine Verbindung, schickt Header mit `Content-Length: 100` und nur einen Teil des Bodys; der Handler wartet dann in `readJSON`, bis der `ReadTimeout` von zehn Sekunden greift. Eine halbe Sekunde später geht das erste `SIGINT` an den Prozess, eine Sekunde danach das zweite – dasselbe wie zweimal Ctrl+C im Terminal. (Wer das wie hier aus einem Skript heraus macht, muss beachten, dass Hintergrundprozesse einer nicht-interaktiven Shell `SIGINT` ignorieren; das Skript setzt die Standardbehandlung vor dem Start zurück.)

Ohne die Änderung (Zeitstempel weggelassen):

```text
level=INFO msg="shutting down"
--- second SIGINT
level=INFO msg=request method=POST path=/targets status=400 duration=9.97810575s
level=INFO msg=stopped
exit=0 after 11s
```

Mit der Änderung:

```text
level=INFO msg="shutting down"
--- second SIGINT
exit=130 after 1s
```

Ohne `stop()` fängt `NotifyContext` das zweite Signal ab und tut nichts damit; upwatch wartet, bis der hängende Request am `ReadTimeout` scheitert, und endet dann sauber mit Exit-Code 0. Mit `stop()` gilt für das zweite Signal wieder das Standardverhalten von Go: Der Prozess wird sofort beendet. Es gibt keine Zeile „stopped“, und der Exit-Code 130 (128 plus Signal 2) zeigt, dass ein Signal den Prozess beendet hat, nicht `run`. Ein einzelnes `SIGTERM` führt weiterhin zum normalen Ablauf bis „stopped“.

Auf `defer stop()` hat das keine schädliche Auswirkung: Die Funktion, die `NotifyContext` zurückgibt, darf mehrfach aufgerufen werden, der zweite Aufruf tut nichts. Das `defer` bleibt trotzdem nötig, und zwar für die Pfade, die `case <-ctx.Done()` nie erreichen – etwa wenn `sqlitestore.Open` scheitert und `run` früh zurückkehrt. Die eigentliche Konsequenz liegt woanders: Beim harten Abbruch laufen keine `defer` mehr, auch nicht `store.Close()`. SQLite verkraftet das dank WAL, beim nächsten Start wird das Journal übernommen; bei anderen Ressourcen (halb geschriebene Dateien, nicht abgemeldete Registrierungen) wäre das zu prüfen.

```bash
git apply loesungen/12-logging-konfiguration-observability/uebung-3.patch
go test ./...
```

**Warum so:** Das zweite Signal ist eine bewusste Entscheidung des Menschen am Terminal: „Ich will nicht warten.“ Ein Dienst, der sie ignoriert, zwingt zu `kill -9`, was dasselbe Ergebnis ohne Erklärung liefert. Unter systemd oder Kubernetes ändert die Zeile nichts am Normalfall, weil dort nur ein `SIGTERM` kommt und nach der Frist `SIGKILL`. Ein Hinweis im Log wie „press Ctrl+C again to force“ wäre lokal nett, stünde aber auch in den Produktionslogs, wo niemand Ctrl+C drückt – deshalb bleibt die Zeile unverändert.

## Übung 4: UPWATCH_LOG_JSON=yes ablehnen (Aufwärmen)

```go
func TestLoadRejectsYes(t *testing.T) {
	_, err := Load(env(map[string]string{"UPWATCH_LOG_JSON": "yes"}))
	if err == nil {
		t.Fatal("UPWATCH_LOG_JSON=yes accepted, want an error")
	}
	// The message must name the variable, the expected form and the
	// value that was given, so it can be fixed without reading the code.
	for _, want := range []string{"UPWATCH_LOG_JSON", "true or false", `"yes"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %s", err, want)
		}
	}
}
```

Die Meldung lautet `UPWATCH_LOG_JSON: want true or false, got "yes"`. Welche Werte gelten, entscheidet `strconv.ParseBool`; `go doc strconv.ParseBool` nennt sie: `1`, `t`, `T`, `TRUE`, `true`, `True`, `0`, `f`, `F`, `FALSE`, `false`, `False`. Ein zweiter Test hält das fest – einige dieser Schreibweisen werden akzeptiert, `yes`, `no`, `on`, `off`, `2` und auch `tRuE` abgelehnt.

```bash
git apply loesungen/12-logging-konfiguration-observability/uebung-4.patch
go test ./...
```

**Warum so:** Der Test prüft nicht den exakten Text, sondern die drei Bestandteile, die eine brauchbare Meldung ausmachen; so überlebt er eine Umformulierung, fängt aber eine Meldung ohne Variablennamen ab. Dass `yes` abgelehnt wird, ist eine Entscheidung, über die man streiten kann – Docker-Compose-Dateien und manche Shell-Skripte benutzen `yes`/`no`. Eine eigene Funktion, die zusätzlich `yes`, `no`, `on`, `off` versteht, wäre schnell geschrieben. Gegen sie spricht, dass `ParseBool` die Erwartung jedes Go-Entwicklers ist, und dass eine klare Ablehnung beim Start (Exit-Code 1, Meldung mit Variable und Wert) den Fehler ohnehin sofort sichtbar macht.
