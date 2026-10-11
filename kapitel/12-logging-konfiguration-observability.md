# Teil 12: Logging, Konfiguration, Observability

Nach diesem Kapitel kannst du einen Go-Dienst so ausstatten, dass er sich im Betrieb verhält, wie man es erwartet: strukturierte Logs, Konfiguration über die Umgebung, Metriken, Health Checks und ein sauberes Herunterfahren.

> **upwatch in diesem Teil:** die Verdrahtung in [main.go](../cmd/upwatch/main.go) (Logger, Debug-Listener, Graceful Shutdown), [config.go](../internal/config/config.go), die expvar-Zähler in [scheduler.go](../internal/scheduler/scheduler.go) und `/healthz`/`/readyz` in [httpapi.go](../internal/httpapi/httpapi.go)
> **Voraussetzungen:** Teil 06 (WaitGroup, Scheduler), Teil 07 (Context), Teil 08 (HTTP-API)

## Auf einen Blick

Die Bausteine dieses Kapitels gibt es in jedem Server-Ökosystem. In Go kommen alle aus der Standardbibliothek und werden in `main` sichtbar zusammengesteckt statt von einem Host-Framework:

| Aufgabe | .NET | Java (Spring) | Go in upwatch |
|---|---|---|---|
| Konfiguration | `IConfiguration`, `appsettings.json` | `application.yml`, `@ConfigurationProperties` | `config.Load(getenv)` über Umgebungsvariablen |
| Logging | `ILogger`, Serilog | SLF4J mit Logback | `log/slog` |
| Metriken | `System.Diagnostics.Metrics` | Micrometer | `expvar` |
| Health Checks | Health-Checks-Middleware (`MapHealthChecks`) | Spring Boot Actuator | eigene Handler `healthz`, `readyz` |
| Herunterfahren | `IHostApplicationLifetime` | Shutdown-Hooks, `SmartLifecycle` | `signal.NotifyContext` und `srv.Shutdown` |

## log/slog: strukturiertes Logging aus der Standardbibliothek

Das alte Package `log` schreibt Textzeilen: `log.Printf("target %d down: %v", id, err)`. Für Menschen lesbar, für Maschinen schwer auszuwerten. Deshalb benutzten Go-Dienste lange externe Bibliotheken wie zap, zerolog oder logrus. Seit Go 1.21 gibt es `log/slog` in der Standardbibliothek, und für neue Projekte ist es der Standard.

slog trennt zwei Dinge: Der `*slog.Logger` ist das, was dein Code aufruft. Der `slog.Handler` entscheidet, wie und wohin geschrieben wird – wie `ILogger` und die Logging-Provider in .NET oder die Sinks in Serilog. Die Standardbibliothek bringt zwei Handler mit, für Text und für JSON. upwatch wählt zwischen ihnen beim Start:

```go
func newLogger(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.LogJSON {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
```

So sieht ein kurzer Lauf mit dem Text-Handler aus. Gebaut wird zuerst ein Binary, denn in diesem Kapitel schickst du dem Prozess Signale: `go run` startet das Programm als Kindprozess, und ein `kill` auf den `go`-Prozess erreicht das Programm nicht zuverlässig.

```bash
DIR=$(mktemp -d)
go build -o "$DIR/upwatch" ./cmd/upwatch
cd "$DIR"
UPWATCH_ADDR=127.0.0.1:18100 UPWATCH_DEBUG_ADDR=127.0.0.1:18101 \
UPWATCH_DB=upwatch.db UPWATCH_TICK=1s UPWATCH_ALLOW_PRIVATE_TARGETS=true \
  ./upwatch &
PID=$!
curl -s -X POST localhost:18100/targets -d '{"url":"http://127.0.0.1:18100/gibtsnicht"}'
curl -s localhost:18100/healthz; curl -s localhost:18100/readyz
kill -TERM $PID
```

```
time=2026-10-10T23:17:03.826+02:00 level=INFO msg=starting version=dev addr=127.0.0.1:18100 db=upwatch.db
time=2026-10-10T23:17:03.829+02:00 level=WARN msg="checks may reach private and loopback addresses"
time=2026-10-10T23:17:03.830+02:00 level=INFO msg=listening addr=127.0.0.1:18100
time=2026-10-10T23:17:03.830+02:00 level=INFO msg="debug listener" addr=127.0.0.1:18101
time=2026-10-10T23:17:04.643+02:00 level=INFO msg=request method=POST path=/targets status=201 duration=793.625µs
time=2026-10-10T23:17:04.842+02:00 level=INFO msg=request method=GET path=/gibtsnicht status=404 duration=28.959µs
time=2026-10-10T23:17:04.842+02:00 level=WARN msg="target down" target_id=1 status=404 err="" duration=10.596875ms
time=2026-10-10T23:17:06.172+02:00 level=INFO msg=request method=GET path=/healthz status=200 duration=77.084µs
time=2026-10-10T23:17:06.193+02:00 level=INFO msg=request method=GET path=/readyz status=200 duration=138.167µs
time=2026-10-10T23:17:10.173+02:00 level=INFO msg="shutting down"
time=2026-10-10T23:17:10.173+02:00 level=INFO msg=stopped
```

Die `GET /gibtsnicht`-Zeile ist upwatchs eigener Check, der bei der eigenen API ankommt; `UPWATCH_ALLOW_PRIVATE_TARGETS` lässt ihn zu, obwohl der SSRF-Schutz aus Teil 13 Loopback-Adressen sonst sperrt.

### Jetzt du: dasselbe als JSON

Starte den Lauf noch einmal mit `UPWATCH_LOG_JSON=true` (den Debug-Listener kannst du weglassen), lege das Ziel an, warte zwei Sekunden und schicke `kill -TERM $PID`. Achte auf zwei Dinge: Jede Zeile ist jetzt ein JSON-Objekt, und `duration` hat eine andere Einheit. Am Ende stehen „shutting down“ und „stopped“, und `wait $PID; echo $?` zeigt Exit-Code 0.

<details><summary>So sah der Lauf beim Schreiben dieses Kapitels aus</summary>

```
{"time":"2026-10-10T23:17:23.597029+02:00","level":"INFO","msg":"starting","version":"dev","addr":"127.0.0.1:18100","db":"upwatch.db"}
{"time":"2026-10-10T23:17:23.598934+02:00","level":"WARN","msg":"checks may reach private and loopback addresses"}
{"time":"2026-10-10T23:17:23.599+02:00","level":"INFO","msg":"listening","addr":"127.0.0.1:18100"}
{"time":"2026-10-10T23:17:24.623581+02:00","level":"INFO","msg":"request","method":"POST","path":"/targets","status":201,"duration":352875}
{"time":"2026-10-10T23:17:25.610653+02:00","level":"INFO","msg":"request","method":"GET","path":"/gibtsnicht","status":404,"duration":18417}
{"time":"2026-10-10T23:17:25.610791+02:00","level":"WARN","msg":"target down","target_id":1,"status":404,"err":"","duration":10019042}
{"time":"2026-10-10T23:17:26.131888+02:00","level":"INFO","msg":"shutting down"}
{"time":"2026-10-10T23:17:26.132002+02:00","level":"INFO","msg":"stopped"}
```

</details>

Inhaltlich ist das identisch, aber Loki, Elasticsearch, CloudWatch oder `jq` verarbeiten jede Zeile direkt. Der Text-Handler schreibt `time.Duration` lesbar als `10.596875ms`, der JSON-Handler als Ganzzahl in Nanosekunden; Dashboards und Abfragen müssen mit dieser Einheit rechnen.

Die Faustregel: Text lokal, JSON überall, wo eine Maschine die Logs einsammelt. Deshalb setzen das [Dockerfile](../Dockerfile) und die [systemd-Unit](../deploy/upwatch.service) `UPWATCH_LOG_JSON=true`, während der Default Text ist. upwatch schreibt nach `stdout` und kümmert sich nicht um Dateien oder Rotation – das erledigen journald, Docker oder die Plattform.

### Level

slog kennt vier Level: `Debug`, `Info`, `Warn`, `Error`. Alles unterhalb des eingestellten Levels wird verworfen, bevor Attribute formatiert werden; ein abgeschaltetes `Debug` kostet fast nichts. upwatch liest das Level aus `UPWATCH_LOG_LEVEL` über `slog.Level.UnmarshalText`, das `debug`, `info`, `warn` und `error` versteht. `make run` setzt `debug`.

Um das Level zur Laufzeit zu ändern, etwa über einen Admin-Endpunkt, übergibst du statt eines festen Levels eine `*slog.LevelVar`:

```go
var level slog.LevelVar // zero value is Info
opts := &slog.HandlerOptions{Level: &level}
level.Set(slog.LevelDebug) // takes effect immediately, safe from any goroutine
```

Wichtiger als die Technik ist, wie man Level einsetzt. In upwatch ist `Warn` für Zustände, die jemand bemerken sollte, die aber keine Fehler des Dienstes sind („target down“, gefährliche Konfiguration). `Error` heißt, dass upwatch selbst etwas nicht geschafft hat („save result“, „claim due targets“, „request failed“). Ein nicht erreichbares Ziel ist für einen Monitoring-Dienst kein Fehler, sondern das, wofür er existiert.

### Strukturierte Attribute

Nach der Nachricht folgen Attribute als abwechselnde Schlüssel und Werte:

```go
s.logger.Warn("target down",
	"target_id", r.TargetID, "status", r.StatusCode, "err", r.Err,
	"duration", r.Duration)
```

Die Nachricht bleibt konstant („target down“), die variablen Teile stehen in Attributen. Dadurch kannst du nach `msg="target down"` filtern und nach `target_id` gruppieren, ohne Strings zu parsen. `fmt.Sprintf` in einer Log-Nachricht ist deshalb fast immer ein verschenktes Attribut.

Typisierte Attribute wie `slog.Int("status", 200)` oder `slog.Duration("duration", d)` sind etwas schneller und verhindern vertauschte Paare. Gegen vergessene Werte hilft auch `go vet` mit seinem slog-Check („call to slog.Logger.Info missing a final value“).

Halte die Schlüssel im ganzen Projekt einheitlich. upwatch benutzt durchgehend `err` für Fehler und `target_id` für Ziele; alle Logzeilen zu Ziel 7 findest du so mit einer Abfrage.

### Logger übergeben, nicht global holen

upwatch erzeugt den Logger in `main` und reicht ihn an die Konstruktoren weiter: `scheduler.New(store, checker, logger, ...)`, `httpapi.New(store, logger)`. Die Packages greifen nie auf einen globalen Logger zu – dieselbe Überlegung wie bei der Datenbank, explizite Abhängigkeiten, die im Test austauschbar sind. In den Tests schreibt der übergebene Logger nach `io.Discard`.

Trotzdem ruft `main` einmal `slog.SetDefault(logger)` auf. Das betrifft die Top-Level-Funktionen `slog.Info` und Co. und das alte `log`-Package: Nach `SetDefault` landet auch ein `log.Printf` aus irgendeiner Bibliothek im selben Handler. Ohne diesen Aufruf stünden im JSON-Log plötzlich Textzeilen. Für APIs, die noch einen `*log.Logger` erwarten, allen voran `http.Server.ErrorLog` (dort meldet `net/http` etwa fehlgeschlagene TLS-Handshakes), baut `slog.NewLogLogger(logger.Handler(), slog.LevelWarn)` die Brücke.

### With: Logger mit Kontext

`logger.With("target_id", t.ID, "url", t.URL)` gibt einen neuen Logger zurück, der diese Attribute an jeden Eintrag anhängt. Typisch ist das pro Request mit einer Request-ID oder pro Job. upwatch braucht es derzeit nicht, weil die Request-Middleware alles Relevante in einer Zeile pro Request loggt. Sobald ein Handler mehrere Zeilen schreibt, die man zusammen sehen will, ist `With` mit einer Request-ID der nächste Schritt (Übung 1). `logger.WithGroup("http")` setzt zusätzlich einen Namensraum: Aus `status` wird `http.status` im Text-Handler und ein verschachteltes Objekt im JSON-Handler.

### LogValuer: Geheimnisse maskieren

Ein Typ, der `slog.LogValuer` implementiert, bestimmt selbst, wie er geloggt wird. Das ist die richtige Stelle, um Geheimnisse zu schützen – einmal am Typ, statt an jeder Log-Stelle daran denken zu müssen:

```go
// Secret is redacted whenever it is logged as an attribute.
type Secret string

func (Secret) LogValue() slog.Value { return slog.StringValue("[redacted]") }
```

Das wirkt, wenn der Wert selbst ein Attribut ist: `logger.Info("config", "token", cfg.Token)`. Steckt er dagegen in einem Struct, das als Ganzes geloggt wird (`"config", cfg`), fragt slog `LogValue` der Felder nicht ab – der JSON-Handler kodiert das Struct mit `encoding/json`, und der Token steht im Klartext im Log. Wer sichergehen will, gibt `Secret` zusätzlich `MarshalText` und `String`, die ebenfalls `[redacted]` liefern, oder loggt Structs mit Geheimnissen gar nicht als Ganzes.

Dasselbe Interface eignet sich, um große Structs kompakt zu loggen: `Target.LogValue` im Beispiel [beispiele/12-logging-konfiguration-observability/slog](../beispiele/12-logging-konfiguration-observability/slog/main.go) gibt nur `id` und `url` als Gruppe zurück und lässt das Token-Feld weg. Das Beispiel zeigt alle Bausteine dieses Abschnitts. Es entfernt den Zeitstempel über `ReplaceAttr`, damit die Ausgabe reproduzierbar ist; ein echter Dienst behält ihn.

```bash
go run ./beispiele/12-logging-konfiguration-observability/slog
```

```
level=INFO msg="check finished" target_id=7 status=200 duration=84ms
{"level":"INFO","msg":"check finished","target_id":7,"status":200,"duration":84000000}
level=DEBUG msg="printed after raising the level"
level=WARN msg="target down" target.id=7 target.url=https://example.com status=503
{"level":"WARN","msg":"target down","target":{"id":7,"url":"https://example.com"},"status":503}
level=INFO msg="token loaded" token=[redacted]
level=INFO msg="with context"
level=INFO msg="legacy log call, e.g. from a library"
level=WARN msg="http: TLS handshake error from 203.0.113.9:51234: EOF"
```

Die vorletzte Zeile stammt aus einem `log.Printf` nach `slog.SetDefault`, die letzte aus einem `*log.Logger`, den `slog.NewLogLogger` erzeugt hat.

## Konfiguration über Umgebungsvariablen

upwatch liest seine gesamte Konfiguration aus Umgebungsvariablen. Das folgt dem Prinzip „Config“ der Twelve-Factor-Regeln: Alles, was sich zwischen Umgebungen unterscheidet – Adressen, Pfade, Zugangsdaten, Log-Format –, gehört in die Umgebung, nicht in den Code und nicht ins Repository. Umgebungsvariablen funktionieren auf dem Laptop, unter systemd und in jedem Container-Orchestrator gleich.

```go
type Config struct {
	Addr         string        // UPWATCH_ADDR
	DebugAddr    string        // UPWATCH_DEBUG_ADDR, empty disables /debug/*
	Workers      int           // UPWATCH_WORKERS
	Tick         time.Duration // UPWATCH_TICK
	LogLevel     slog.Level    // UPWATCH_LOG_LEVEL (debug, info, warn, error)
	// ... DBPath, CheckTimeout, LogJSON, AllowPrivate
}
```

Das Struct ist die Dokumentation: Jedes Feld nennt seine Variable; alle neun mit Defaults stehen als Tabelle in Teil 15. Die Defaults sind für lokale Entwicklung gewählt, damit `go run ./cmd/upwatch` ohne jede Einstellung funktioniert.

### Load(getenv): testbar ohne globale Zustände

Die entscheidende Designentscheidung steckt in der Signatur. `Load` ruft nicht selbst `os.Getenv` auf, sondern bekommt eine Funktion mit derselben Signatur; im Programm ist das `os.Getenv`, im Test eine Map, die der Helfer `env` in eine solche Funktion verwandelt:

```go
func Load(getenv func(string) string) (Config, error)
func FromEnv() (Config, error) { return Load(os.Getenv) }

cfg, err := Load(env(map[string]string{"UPWATCH_WORKERS": "2"})) // in config_test.go
```

Die Tests verändern keine echten Umgebungsvariablen, beeinflussen sich also nicht gegenseitig und könnten parallel laufen. Eine Funktion als Parameter ist dabei die kleinste mögliche Form eines Interfaces.

### errors.Join: alle Fehler auf einmal

`Load` bricht beim ersten ungültigen Wert nicht ab, sondern sammelt alle Fehler in einem Slice und gibt am Ende `errors.Join(errs...)` zurück. Mit drei Fehlern gleichzeitig, aufgerufen mit dem Binary von oben:

```bash
UPWATCH_WORKERS=zero UPWATCH_TICK=-1s UPWATCH_LOG_LEVEL=loud ./upwatch; echo "exit=$?"
```

```
upwatch: config: UPWATCH_WORKERS: want a positive integer, got "zero"
UPWATCH_TICK: want a positive duration like 5s, got "-1s"
UPWATCH_LOG_LEVEL: slog: level string "loud": unknown name
exit=1
```

Ein Neustart, alle Probleme sichtbar, statt drei Runden „korrigieren, deployen, nächster Fehler“. `errors.Join` (seit Go 1.20) gibt `nil` zurück, wenn die Liste leer ist, deshalb braucht es keine Fallunterscheidung. Geprüft wird beim Start, bevor irgendetwas anderes passiert: Ein Dienst, der mit kaputter Konfiguration sofort mit Exit-Code 1 aussteigt, ist leichter zu betreiben als einer, der erst beim ersten Check scheitert.

### Dateien, Flags und Secrets

Eine Konfigurationsdatei lohnt sich, wenn die Konfiguration selbst Daten ist (etwa eine versionierte Liste von Zielen); Bibliotheken wie Viper oder koanf schichten dann Dateien, Umgebung und Flags ähnlich wie `IConfiguration`. Für Kommandozeilenwerkzeuge ist das `flag`-Package der natürliche Weg, das Shutdown-Beispiel weiter unten benutzt es. Für einen Dienst mit neun Einstellungen reicht die Umgebung; Secrets gehören dort in eine Datei, die nur root lesen darf (`EnvironmentFile=` in der systemd-Unit, mehr in Teil 13 und 15).

## Metriken mit expvar

Logs erzählen, was passiert ist. Metriken zählen, wie oft. upwatch benutzt dafür `expvar` aus der Standardbibliothek:

```go
var (
	checksTotal  = expvar.NewInt("upwatch_checks_total")
	checksDown   = expvar.NewInt("upwatch_checks_down_total")
	saveFailures = expvar.NewInt("upwatch_save_failures_total")
)
```

Der Writer des Schedulers erhöht sie mit `checksTotal.Add(1)`. `expvar.Int` ist atomar und damit aus beliebigen Goroutines sicher. Abrufbar sind die Werte als JSON unter `/debug/vars`, zusammen mit Laufzeitstatistiken wie `memstats`. Während des Laufs von oben:

```bash
curl -s localhost:18101/debug/vars | grep upwatch_
```

```
"upwatch_checks_down_total": 1,
"upwatch_checks_total": 1,
"upwatch_save_failures_total": 0
```

`/debug/vars` ist nicht auf dem API-Port erreichbar, sondern auf einem eigenen Listener, der nur startet, wenn `UPWATCH_DEBUG_ADDR` gesetzt ist:

```go
func serveDebug(addr string, logger *slog.Logger) {
	mux := http.NewServeMux()
	mux.Handle("GET /debug/vars", expvar.Handler())
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	// ... more pprof handlers, then ListenAndServe on addr
}
```

Dort hängt auch `pprof` für CPU- und Speicherprofile (Teil 14). Beides verrät viel über das Innenleben des Prozesses und gehört deshalb auf `127.0.0.1` oder in ein internes Netz. Warum ein eigener `ServeMux` dafür nötig ist und was beim Import von `expvar` und `net/http/pprof` sonst passiert, steht in Teil 13.

## Health Checks: Liveness und Readiness

upwatch hat zwei Endpunkte, die auf den ersten Blick dasselbe tun:

```go
// healthz answers "is the process alive?" and never touches dependencies.
func (a *API) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// readyz answers "can this instance serve traffic?" and checks the database.
func (a *API) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if err := a.store.Ping(ctx); err != nil {
		a.logger.Warn("readiness check failed", "err", err)
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
```

Die Unterscheidung kommt aus Kubernetes, gilt aber für jeden Orchestrator und Load Balancer. **Liveness** (`/healthz`) fragt: Lebt der Prozess, oder hängt er? Schlägt sie fehl, wird der Prozess neu gestartet. **Readiness** (`/readyz`) fragt: Kann diese Instanz gerade Anfragen bedienen? Schlägt sie fehl, bekommt die Instanz vorübergehend keinen Traffic, wird aber nicht neu gestartet.

Deshalb prüft `healthz` keine Abhängigkeiten. Meldete es eine kurz nicht erreichbare Datenbank, würde der Orchestrator alle Instanzen gleichzeitig neu starten – was am Problem nichts ändert und es oft verschlimmert. `readyz` prüft die Datenbank mit einem eigenen Timeout von einer Sekunde, damit der Check selbst nicht hängt. Die Antwort enthält nur „database unavailable“; der eigentliche Fehler landet im Log (Teil 08 und 13). Wie die Probes konfiguriert werden und wie man `/readyz` beim Herunterfahren gezielt auf 503 schaltet, zeigt Teil 15.

## Graceful Shutdown

Ein Dienst wird ständig beendet: bei jedem Deployment, bei jedem Neustart durch systemd, wenn Kubernetes einen Pod verschiebt. Die Plattform schickt dafür `SIGTERM` und wartet eine Frist, bevor sie mit `SIGKILL` hart abbricht. Ein Dienst, der `SIGTERM` ignoriert, verliert laufende Requests und halb geschriebene Daten. `run` in [main.go](../cmd/upwatch/main.go) ist im Wesentlichen eine sorgfältige Antwort auf dieses Signal. Der Ablauf, unter systemd mit `TimeoutStopSec=30`:

```text
Zeit   Was passiert                                  Code in run()
─────  ────────────────────────────────────────────  ─────────────────────────────────
 0 s   SIGTERM kommt an                              NotifyContext bricht ctx ab,
                                                     select: case <-ctx.Done()
       ├─ API: Listener zu, laufende Requests        srv.Shutdown(shutdownCtx)  ─┐
       │  dürfen fertig werden                                                  │ parallel
       └─ Scheduler: Dispatcher stoppt, Writer       sched.Run sieht ctx.Done() ─┘
          speichert fertige Ergebnisse
       beides fertig                                 wg.Wait() kehrt zurück
       Aufräumen in umgekehrter Reihenfolge          defer: cancel, store.Close, stop
       Prozess endet mit Exit-Code 0
15 s   spätestens hier gibt Shutdown auf             return fmt.Errorf("shutdown: …"),
                                                     Exit-Code 1
30 s   TimeoutStopSec: systemd schickt SIGKILL       – (kein Code läuft mehr)
```

Im Normalfall ist alles nach Millisekunden erledigt; die Fristen sind Obergrenzen. Die folgenden Abschnitte gehen die Zeilen von oben nach unten durch. Wer aus ASP.NET Core kommt: Dort übernehmen `IHostApplicationLifetime.ApplicationStopping` und `HostOptions.ShutdownTimeout` diese Rolle, in Go stehen beide als sichtbarer Code in `run`.

### run() statt os.Exit

`os.Exit` beendet den Prozess sofort, **ohne** `defer`-Anweisungen auszuführen. Riefe die Logik bei einem Fehler `os.Exit(1)` oder `log.Fatal` auf, liefe `defer store.Close()` nie. Deshalb steckt alles in `run`, das einen Fehler zurückgibt; beim Verlassen laufen alle `defer`, und erst danach ruft `main` `os.Exit(1)`. Das Muster ist in Go-Programmen verbreitet und macht `run` nebenbei testbar.

### signal.NotifyContext

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
```

`signal.NotifyContext` verbindet Signale mit dem Context-Mechanismus aus Teil 07: Kommt `SIGINT` (Ctrl+C) oder `SIGTERM`, wird `ctx` abgebrochen. Dieser Context geht an den Scheduler, und alles, was davon abgeleitet ist, sieht den Abbruch. Einen globalen „shutting down“-Schalter, den jede Komponente abfragen müsste, gibt es nicht.

### Server starten, auf Fehler oder Signal warten

```go
var wg sync.WaitGroup
wg.Go(func() { sched.Run(ctx) })

serveErr := make(chan error, 1)
go func() {
	logger.Info("listening", "addr", cfg.Addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		serveErr <- err
	}
	close(serveErr)
}()

select {
case err := <-serveErr:
	stop()
	wg.Wait()
	return fmt.Errorf("http server: %w", err)
case <-ctx.Done():
	logger.Info("shutting down")
}
```

Der Scheduler läuft über `WaitGroup.Go` (seit Go 1.25), damit `run` später auf ihn warten kann. Der HTTP-Server läuft in einer eigenen Goroutine, weil `ListenAndServe` blockiert. Das `select` wartet auf das erste von zwei Ereignissen. Scheitert der Server – typischerweise, weil der Port belegt ist –, bricht `stop()` den Context ab, der Scheduler fährt herunter, und `run` gibt den Fehler zurück; der Puffer von 1 lässt die Server-Goroutine ihren Fehler abliefern, auch wenn gerade niemand liest. `http.ErrServerClosed` kommt nach jedem `Shutdown` und ist kein Fehler. Kommt dagegen das Signal, beginnt der geordnete Abbau.

### srv.Shutdown mit Timeout und Scheduler-Drain

```go
shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()
err = srv.Shutdown(shutdownCtx)
// Wait for the scheduler even if Shutdown timed out: the deferred
// store.Close must not run while workers still use the database.
wg.Wait()
if err != nil {
	return fmt.Errorf("shutdown: %w", err)
}

logger.Info("stopped")
return nil
```

`srv.Shutdown` schließt sofort alle Listener und untätigen Verbindungen und wartet dann auf die laufenden Requests, höchstens 15 Sekunden. Der Timeout-Context basiert auf `context.Background()`, nicht auf `ctx` – der ist schon abgebrochen, und ein davon abgeleiteter Context wäre es sofort auch.

Parallel fährt der Scheduler herunter, wie in Teil 06 beschrieben: Laufende Checks werden abgebrochen, der Writer speichert fertige Ergebnisse mit `context.WithoutCancel`. `wg.Wait()` steht bewusst vor der Fehlerprüfung: Auch wenn `Shutdown` die Frist überschreitet, wartet `run` noch auf den Scheduler, dessen Context ja schon abgebrochen ist. Sonst würde das `defer store.Close()` die Datenbank schließen, während ein Worker noch speichert. Danach laufen die `defer` rückwärts ab, und die Datenbank wird erst geschlossen, wenn weder API noch Scheduler sie mehr benutzen. Die Reihenfolge ergibt sich aus der Struktur des Codes, nicht aus Konfiguration.

Die 15 Sekunden müssen unter der Frist der Plattform liegen. Welche Fristen systemd, Docker und Kubernetes setzen und wie man sie anpasst, steht in Teil 15.

### Ausprobieren

Das Beispiel [beispiele/12-logging-konfiguration-observability/shutdown](../beispiele/12-logging-konfiguration-observability/shutdown/main.go) reduziert das Muster auf einen Server mit einem Endpunkt, der zwei Sekunden braucht. Starte ihn, schicke einen Request und beende den Server, während der Request läuft (wieder als Binary, aus dem Grund von oben):

```bash
go build -o /tmp/shutdown ./beispiele/12-logging-konfiguration-observability/shutdown
/tmp/shutdown -addr 127.0.0.1:18102 &
PID=$!
curl -s -i localhost:18102/slow &
sleep 0.5; kill -TERM $PID
```

```
time=2026-10-10T23:17:37.587+02:00 level=INFO msg=listening addr=127.0.0.1:18102
time=2026-10-10T23:17:37.636+02:00 level=INFO msg="slow request started"
time=2026-10-10T23:17:38.118+02:00 level=INFO msg="shutting down" grace=10s
time=2026-10-10T23:17:39.637+02:00 level=INFO msg="slow request finished"
time=2026-10-10T23:17:39.705+02:00 level=INFO msg=stopped
```

Das Signal kommt eine halbe Sekunde nach Beginn des Requests, der Request läuft trotzdem zu Ende, und curl bekommt sein `HTTP/1.1 200 OK` – mit `Connection: close`, weil der Server auf dieser Verbindung keine weiteren Requests annimmt. Mit `-grace 500ms` ist die Frist kürzer als der Request:

```
time=2026-10-10T23:17:40.525+02:00 level=INFO msg="shutting down" grace=500ms
shutdown: graceful shutdown: context deadline exceeded
```

`Shutdown` gibt nach Ablauf der Frist `context.DeadlineExceeded` zurück, das Programm endet mit Exit-Code 1, und der Client verliert die Verbindung (curl endet mit Exit-Code 52, „empty reply from server“). Ein klar protokollierter Abbruch nach einer Frist ist besser als ein Prozess, der beliebig lange auf einen hängenden Client wartet. Ctrl+C im Terminal funktioniert übrigens auch mit `go run`, weil das Terminal das Signal an die ganze Prozessgruppe schickt.

### Zwei Verfeinerungen

Das Beispiel ruft nach dem Signal sofort `stop()` auf, damit ein zweites Ctrl+C den Prozess wieder hart beendet. Solange `stop` nicht aufgerufen ist, fängt `NotifyContext` weitere Signale ab, ohne etwas zu tun. In upwatch läuft `stop` erst als `defer` am Ende von `run`; ein zweites Ctrl+C während des Herunterfahrens bleibt dort wirkungslos, bis die Frist vorbei ist. Unter systemd ist das egal, weil nach `TimeoutStopSec` ohnehin `SIGKILL` folgt; beim lokalen Entwickeln ist das sofortige `stop()` angenehmer.

Die zweite Verfeinerung betrifft Umgebungen mit Load Balancer: `/readyz` schon vor `Shutdown` auf 503 schalten, damit keine neuen Anfragen mehr ankommen. Das ist Thema von Teil 15, samt Übung.

## Nächste Schritte

`expvar` ist ein guter Anfang ohne Abhängigkeit, kennt aber keine Labels („Checks pro Statuscode“), keine Histogramme und nicht das Format, das Monitoring-Systeme erwarten. Der übliche nächste Schritt ist der Prometheus-Client (`github.com/prometheus/client_golang`) mit einem `/metrics`-Endpunkt auf dem Debug-Listener, oder OpenTelemetry, das Metriken und Traces herstellerneutral an einen Collector schickt. Für Tracing zahlt sich aus, dass upwatch den Context konsequent durchreicht: Der aktuelle Span reist in `context.Context`, eine Middleware wie `otelhttp` legt ihn in `r.Context()`, und instrumentierte Treiber und Clients hängen ihre Kind-Spans automatisch darunter. Mit `InfoContext`, `WarnContext` und Co. kann ein passender slog-Handler Trace- und Span-ID an jede Logzeile hängen. upwatch nutzt die Context-Varianten noch nicht, weil sein Handler damit nichts anfangen würde – mit OpenTelemetry wäre das die erste Änderung.

## Im Projekt

- [main.go](../cmd/upwatch/main.go) – `run`, `newLogger`, `slog.SetDefault`, `slog.NewLogLogger`, `signal.NotifyContext`, Server-Timeouts, Graceful Shutdown, `serveDebug`.
- [config.go](../internal/config/config.go) und [config_test.go](../internal/config/config_test.go) – `Load(getenv)`, Defaults, `errors.Join`.
- [scheduler.go](../internal/scheduler/scheduler.go) – expvar-Zähler, `Warn` für „target down“, `Error` für Speicherfehler, `context.WithoutCancel` im Writer.
- [middleware.go](../internal/httpapi/middleware.go) – eine strukturierte Logzeile pro Request.
- [httpapi.go](../internal/httpapi/httpapi.go) – `healthz` und `readyz`.
- [upwatch.service](../deploy/upwatch.service) und [Dockerfile](../Dockerfile) – JSON-Logs und Stop-Timeout im Betrieb.

## Übungen

Musterlösungen mit Begründung stehen im Branch [`loesungen`](https://github.com/casoon/go-tutorial/blob/loesungen/loesungen/12-logging-konfiguration-observability.md) – versuch es zuerst selbst.

1. **Kern (empfohlen):** Ergänze die Request-Middleware in upwatch um eine Request-ID: Erzeuge pro Request eine zufällige ID, setze sie als Response-Header `X-Request-Id` und gib sie als Attribut `request_id` in der Logzeile aus. Wie würdest du die ID an Handler weiterreichen, die selbst loggen?
2. **Kern:** Füge einen vierten expvar-Zähler hinzu, der zählt, wie viele Checks mit einem Timeout endeten (`r.Err == "timeout"`). Starte upwatch mit `UPWATCH_DEBUG_ADDR` und prüfe den Wert über `/debug/vars`.
3. **Profi:** Ändere `run` so, dass `stop()` direkt nach `<-ctx.Done()` aufgerufen wird, und probiere mit einem lokal gebauten Binary aus, was ein zweites Ctrl+C während des Shutdowns bewirkt. Welche Auswirkung hat das auf `defer stop()`?
4. **Aufwärmen:** Schreib für `config.Load` einen Testfall, der prüft, dass `UPWATCH_LOG_JSON=yes` mit einer verständlichen Meldung abgelehnt wird. Schau dir dabei an, welche Werte `strconv.ParseBool` tatsächlich akzeptiert.

---

← [Teil 11: Testing](11-testing.md) · [Übersicht](../README.md) · [Teil 13: Security](13-security.md) →
