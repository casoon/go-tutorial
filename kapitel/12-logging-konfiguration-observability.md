# Teil 12: Logging, Konfiguration, Observability

Nach diesem Kapitel kannst du einen Go-Dienst so ausstatten, dass er sich im Betrieb verhält, wie man es erwartet: strukturierte Logs, Konfiguration über die Umgebung, Metriken, Health Checks und ein sauberes Herunterfahren.

## log/slog: strukturiertes Logging aus der Standardbibliothek

Das alte Package `log` schreibt Textzeilen: `log.Printf("target %d down: %v", id, err)`. Für Menschen lesbar, für Maschinen schwer auszuwerten. Deshalb benutzten Go-Dienste lange externe Bibliotheken wie zap, zerolog oder logrus. Seit Go 1.21 gibt es `log/slog` in der Standardbibliothek, und für neue Projekte ist es der Standard.

slog trennt zwei Dinge: Der `*slog.Logger` ist das, was dein Code aufruft. Der `slog.Handler` entscheidet, wie und wohin geschrieben wird. Das entspricht `ILogger` und den Logging-Providern in .NET oder den Sinks in Serilog. Die Standardbibliothek bringt zwei Handler mit, für Text und für JSON. upwatch wählt zwischen ihnen beim Start:

```go
func newLogger(cfg config.Config) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	if cfg.LogJSON {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}
```

So sieht ein kurzer Lauf von upwatch mit dem Text-Handler aus. Gestartet in einem Temp-Verzeichnis, ein Ziel angelegt, das auf eine nicht vorhandene Seite des eigenen Servers zeigt, dann mit `SIGTERM` beendet:

```bash
cd "$(mktemp -d)"
UPWATCH_ADDR=127.0.0.1:18080 UPWATCH_DEBUG_ADDR=127.0.0.1:16060 \
UPWATCH_DB=upwatch.db UPWATCH_TICK=1s UPWATCH_ALLOW_PRIVATE_TARGETS=true \
  upwatch &
curl -s -X POST localhost:18080/targets -d '{"url":"http://127.0.0.1:18080/gibtsnicht"}'
kill -TERM %1
```

```
time=2026-10-10T22:28:55.009+02:00 level=INFO msg=starting version=dev addr=127.0.0.1:18080 db=upwatch.db
time=2026-10-10T22:28:55.011+02:00 level=WARN msg="checks may reach private and loopback addresses"
time=2026-10-10T22:28:55.011+02:00 level=INFO msg=listening addr=127.0.0.1:18080
time=2026-10-10T22:28:55.011+02:00 level=INFO msg="debug listener" addr=127.0.0.1:16060
time=2026-10-10T22:28:56.039+02:00 level=INFO msg=request method=POST path=/targets status=201 duration=664.166µs
time=2026-10-10T22:28:57.030+02:00 level=INFO msg=request method=GET path=/gibtsnicht status=404 duration=56µs
time=2026-10-10T22:28:57.031+02:00 level=WARN msg="target down" target_id=1 status=404 err="" duration=16.868958ms
time=2026-10-10T22:28:58.080+02:00 level=INFO msg="shutting down"
time=2026-10-10T22:28:58.080+02:00 level=INFO msg=stopped
```

Die `GET /gibtsnicht`-Zeile ist upwatchs eigener Check, der bei der eigenen API ankommt – deshalb `UPWATCH_ALLOW_PRIVATE_TARGETS`, das den SSRF-Schutz aus Teil 13 für lokale Tests abschaltet und beim Start eine Warnung loggt.

Derselbe Ablauf mit `UPWATCH_LOG_JSON=true`:

```
{"time":"2026-10-10T22:28:46.565016+02:00","level":"INFO","msg":"starting","version":"dev","addr":"127.0.0.1:18080","db":"upwatch.db"}
{"time":"2026-10-10T22:28:46.566614+02:00","level":"WARN","msg":"checks may reach private and loopback addresses"}
{"time":"2026-10-10T22:28:46.566657+02:00","level":"INFO","msg":"listening","addr":"127.0.0.1:18080"}
{"time":"2026-10-10T22:28:47.601472+02:00","level":"INFO","msg":"request","method":"POST","path":"/targets","status":201,"duration":1153584}
{"time":"2026-10-10T22:28:48.58467+02:00","level":"INFO","msg":"request","method":"GET","path":"/gibtsnicht","status":404,"duration":52875}
{"time":"2026-10-10T22:28:48.58491+02:00","level":"WARN","msg":"target down","target_id":1,"status":404,"err":"","duration":15564708}
{"time":"2026-10-10T22:28:49.611603+02:00","level":"INFO","msg":"shutting down"}
{"time":"2026-10-10T22:28:49.611691+02:00","level":"INFO","msg":"stopped"}
```

Inhaltlich identisch, aber jede Zeile ist ein JSON-Objekt, das Loki, Elasticsearch, CloudWatch oder `jq` direkt verarbeiten können. Achte auf `duration`: Der Text-Handler schreibt `time.Duration` lesbar als `16.868958ms`, der JSON-Handler als Ganzzahl in Nanosekunden. Wer Dashboards baut, muss das wissen.

Die Faustregel: Text lokal, JSON überall, wo eine Maschine die Logs einsammelt. Deshalb setzen sowohl das [Dockerfile](../Dockerfile) als auch die [systemd-Unit](../deploy/upwatch.service) `UPWATCH_LOG_JSON=true`, während der Default für die Entwicklung Text ist. upwatch schreibt nach `stdout` und kümmert sich nicht um Dateien oder Rotation – das ist Aufgabe von journald, Docker oder der Plattform.

### Level

slog kennt vier Level: `Debug`, `Info`, `Warn`, `Error`. Alles unterhalb des eingestellten Levels wird verworfen, bevor Attribute formatiert werden; ein abgeschaltetes `Debug` kostet fast nichts. upwatch liest das Level aus `UPWATCH_LOG_LEVEL` und benutzt dafür `slog.Level.UnmarshalText`, das `debug`, `info`, `warn` und `error` versteht. `make run` setzt `debug`.

Wenn du das Level zur Laufzeit ändern willst, etwa über einen Admin-Endpunkt, übergibst du statt eines festen Levels eine `*slog.LevelVar`. Das Beispiel zu diesem Kapitel zeigt das.

Wie man Level einsetzt, ist wichtiger als die Technik. In upwatch ist `Warn` für Zustände, die jemand bemerken sollte, die aber keine Fehler des Dienstes sind („target down“, gefährliche Konfiguration). `Error` heißt, dass upwatch selbst etwas nicht geschafft hat („save result“, „claim due targets“, „request failed“). Ein nicht erreichbares Ziel ist für einen Monitoring-Dienst kein Fehler, sondern das, wofür er existiert.

### Strukturierte Attribute

Nach der Nachricht folgen Attribute als abwechselnde Schlüssel und Werte:

```go
s.logger.Warn("target down",
	"target_id", r.TargetID, "status", r.StatusCode, "err", r.Err,
	"duration", r.Duration)
```

Die Nachricht bleibt konstant („target down“), die variablen Teile stehen in Attributen. Dadurch kannst du nach `msg="target down"` filtern und nach `target_id` gruppieren, ohne Strings zu parsen. Das ist der Kern von strukturiertem Logging, und es ist der Grund, warum `fmt.Sprintf` in einer Log-Nachricht fast immer ein Zeichen für ein verschenktes Attribut ist.

Statt Schlüssel-Wert-Paaren kannst du typisierte Attribute benutzen: `slog.Int("status", 200)`, `slog.Duration("duration", d)`. Das ist etwas schneller und verhindert vertauschte Paare. Gegen vergessene Werte hilft auch `go vet`, das einen eigenen slog-Check hat:

```
main.go:10:2: call to slog.Logger.Info missing a final value
```

Halte die Schlüssel im ganzen Projekt einheitlich. upwatch benutzt durchgehend `err` für Fehler und `target_id` für Ziele; wer später nach allen Logzeilen zu Ziel 7 sucht, findet sie mit einer Abfrage.

### Logger übergeben, nicht global holen

upwatch erzeugt den Logger in `main` und reicht ihn an die Konstruktoren weiter: `scheduler.New(store, checker, logger, ...)`, `httpapi.New(store, logger)`. Die Packages greifen nie auf einen globalen Logger zu. Das ist dieselbe Überlegung wie bei der Datenbank: explizite Abhängigkeiten, die im Test austauschbar sind. In den Tests wird ein Logger übergeben, der nach `io.Discard` schreibt.

Trotzdem ruft `main` einmal `slog.SetDefault(logger)` auf. Das betrifft zwei Dinge: die Top-Level-Funktionen `slog.Info` und Co., und das alte `log`-Package. Nach `SetDefault` landet auch ein `log.Printf` aus irgendeiner Bibliothek im selben Handler, also im selben Format. Ohne diesen Aufruf hättest du im JSON-Log plötzlich Textzeilen.

### slog.NewLogLogger für http.Server.ErrorLog

Einige APIs der Standardbibliothek erwarten noch einen `*log.Logger`, allen voran `http.Server.ErrorLog`. Dort meldet `net/http` Dinge wie fehlgeschlagene TLS-Handshakes oder Panics in Handlern. `slog.NewLogLogger` baut eine Brücke:

```go
ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
```

Jede Zeile, die `net/http` dort schreibt, wird zu einem slog-Eintrag auf `Warn`-Level im selben Format wie alles andere.

### With: Logger mit Kontext

`logger.With(...)` gibt einen neuen Logger zurück, der die übergebenen Attribute an jeden Eintrag anhängt. Damit baust du Logger für einen Bereich, ohne die Attribute bei jedem Aufruf zu wiederholen:

```go
checkLog := logger.With("target_id", t.ID, "url", t.URL)
checkLog.Info("check started")
checkLog.Warn("target down", "status", 503)
```

Typisch ist das pro Request mit einer Request-ID oder pro Job. upwatch braucht es derzeit nicht, weil die Request-Middleware alles Relevante in einer einzigen Zeile pro Request loggt. Sobald ein Handler mehrere Zeilen schreibt, die man zusammen sehen will, ist `With` mit einer Request-ID der nächste Schritt; den Logger reicht man dann über den Context oder als Parameter weiter.

`logger.WithGroup("http")` setzt zusätzlich einen Namensraum: Aus `status` wird `http.status` im Text-Handler und ein verschachteltes Objekt im JSON-Handler.

### LogValuer: Geheimnisse maskieren

Ein Typ, der `slog.LogValuer` implementiert, bestimmt selbst, wie er geloggt wird. Das ist die richtige Stelle, um Geheimnisse zu schützen – einmal am Typ, statt an jeder Log-Stelle daran denken zu müssen:

```go
// Secret never shows up in logs, whatever handler is used.
type Secret string

func (Secret) LogValue() slog.Value { return slog.StringValue("[redacted]") }
```

Dasselbe Interface eignet sich, um große Structs kompakt zu loggen: `Target.LogValue` im Beispiel gibt nur `id` und `url` als Gruppe zurück und lässt das Token-Feld ganz weg.

### Das Beispiel

[beispiele/12-logging-konfiguration-observability/slog](../beispiele/12-logging-konfiguration-observability/slog/main.go) zeigt alle diese Bausteine. Es entfernt den Zeitstempel über `ReplaceAttr`, damit die Ausgabe reproduzierbar ist – in einem echten Dienst lässt du ihn natürlich drin.

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

upwatch liest seine gesamte Konfiguration aus Umgebungsvariablen. Das folgt dem Prinzip „Config“ aus den Twelve-Factor-Regeln: Alles, was sich zwischen Umgebungen unterscheidet – Adressen, Pfade, Zugangsdaten, Log-Format –, gehört in die Umgebung, nicht in den Code und nicht ins Repository. Umgebungsvariablen funktionieren auf dem Laptop, unter systemd und in jedem Container-Orchestrator gleich.

```go
type Config struct {
	Addr         string        // UPWATCH_ADDR
	DebugAddr    string        // UPWATCH_DEBUG_ADDR, empty disables /debug/*
	DBPath       string        // UPWATCH_DB
	Workers      int           // UPWATCH_WORKERS
	Tick         time.Duration // UPWATCH_TICK
	CheckTimeout time.Duration // UPWATCH_CHECK_TIMEOUT
	LogLevel     slog.Level    // UPWATCH_LOG_LEVEL (debug, info, warn, error)
	LogJSON      bool          // UPWATCH_LOG_JSON
	AllowPrivate bool          // UPWATCH_ALLOW_PRIVATE_TARGETS, for local testing only
}
```

Das Struct ist die Dokumentation: Jedes Feld nennt seine Variable. Die Defaults sind für lokale Entwicklung gewählt, damit `go run ./cmd/upwatch` ohne jede Einstellung funktioniert.

### Load(getenv): testbar ohne globale Zustände

Die entscheidende Designentscheidung steckt in der Signatur:

```go
// Load reads the configuration via getenv, typically os.Getenv.
// Passing the lookup function in keeps Load free of global state and
// trivially testable.
func Load(getenv func(string) string) (Config, error)

// FromEnv is Load with the real process environment.
func FromEnv() (Config, error) { return Load(os.Getenv) }
```

`Load` ruft nicht selbst `os.Getenv` auf, sondern bekommt eine Funktion mit derselben Signatur. Im Programm ist das `os.Getenv`, im Test eine Map:

```go
// env turns a map into a getenv function.
func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

cfg, err := Load(env(map[string]string{
	"UPWATCH_ADDR":    "127.0.0.1:9000",
	"UPWATCH_WORKERS": "2",
	// ...
}))
```

Die Tests verändern keine echten Umgebungsvariablen, beeinflussen sich also nicht gegenseitig und könnten parallel laufen. Es ist dasselbe Prinzip wie bei den Store-Interfaces: Abhängigkeiten werden hineingereicht. Eine Funktion als Parameter ist dabei die kleinste mögliche Form eines Interfaces.

### errors.Join: alle Fehler auf einmal

`Load` bricht beim ersten ungültigen Wert nicht ab, sondern sammelt alle Fehler und gibt sie am Ende gemeinsam zurück:

```go
// Report every problem at once instead of one per restart.
return cfg, errors.Join(errs...)
```

Mit drei Fehlern gleichzeitig sieht das so aus:

```bash
UPWATCH_WORKERS=zero UPWATCH_TICK=-1s UPWATCH_LOG_LEVEL=loud upwatch; echo "exit=$?"
```

```
upwatch: config: UPWATCH_WORKERS: want a positive integer, got "zero"
UPWATCH_TICK: want a positive duration like 5s, got "-1s"
UPWATCH_LOG_LEVEL: slog: level string "loud": unknown name
exit=1
```

Wer einen Dienst über systemd oder Kubernetes konfiguriert, weiß das zu schätzen: ein Neustart, alle Probleme sichtbar, statt drei Runden „korrigieren, deployen, nächster Fehler“. `errors.Join` (seit Go 1.20) gibt `nil` zurück, wenn die Liste leer ist, deshalb braucht es keine Fallunterscheidung. Die Fehlermeldungen enthalten den Namen der Variable und den falschen Wert, also alles, was man zum Korrigieren braucht.

Wichtig ist auch, wann geprüft wird: beim Start, bevor irgendetwas anderes passiert. Ein Dienst, der mit kaputter Konfiguration startet und erst beim ersten Check scheitert, ist schwerer zu betreiben als einer, der sofort mit Exit-Code 1 aussteigt.

### Konfigurationsdateien und das flag-Package

Umgebungsvariablen haben Grenzen. Sie sind flach, also unpraktisch für Listen und verschachtelte Strukturen, und bei vielen Einstellungen unübersichtlich. Eine Konfigurationsdatei lohnt sich, wenn die Konfiguration selbst Daten ist – etwa eine Liste von Zielen, die man versionieren will – oder wenn Administratoren sie bearbeiten, die keine Unit-Dateien anfassen sollen. JSON liest die Standardbibliothek (`encoding/json` mit `DisallowUnknownFields`, wie in der API); für YAML oder TOML brauchst du eine Abhängigkeit. Bibliotheken wie Viper oder koanf kombinieren Dateien, Umgebung und Flags in Schichten, ähnlich `IConfiguration` in .NET. Für einen Dienst mit neun Einstellungen wäre das mehr Mechanik als Nutzen.

Für Kommandozeilenwerkzeuge ist das `flag`-Package der natürliche Weg:

```go
addr := flag.String("addr", "127.0.0.1:8081", "listen address")
grace := flag.Duration("grace", 10*time.Second, "time in-flight requests get on shutdown")
flag.Parse()
```

`flag` erzeugt automatisch eine Hilfe mit `-h` und parst `time.Duration` direkt. Für Dienste ist es weniger geeignet, weil Orchestratoren und Container-Plattformen Konfiguration typischerweise als Umgebung übergeben. Beides lässt sich kombinieren, indem man den Default eines Flags aus der Umgebung liest.

Geheimnisse wie Datenbank-Passwörter oder API-Tokens gehören auch in die Umgebung, aber nicht in eine Datei im Repository. Die systemd-Unit von upwatch zeigt den Weg im Kommentar: `EnvironmentFile=/etc/upwatch/env`, eine Datei, die nur root lesen darf. Mehr dazu in Teil 13 und 15.

## Metriken mit expvar

Logs erzählen, was passiert ist. Metriken zählen, wie oft. upwatch benutzt dafür `expvar` aus der Standardbibliothek:

```go
// Metrics are published via expvar at /debug/vars.
var (
	checksTotal  = expvar.NewInt("upwatch_checks_total")
	checksDown   = expvar.NewInt("upwatch_checks_down_total")
	saveFailures = expvar.NewInt("upwatch_save_failures_total")
)
```

Der Writer des Schedulers erhöht sie mit `checksTotal.Add(1)`. `expvar.Int` ist atomar und damit aus beliebigen Goroutinen sicher. Abrufbar sind die Werte als JSON unter `/debug/vars`, zusammen mit Laufzeitstatistiken wie `memstats`:

```bash
curl -s localhost:16060/debug/vars | grep upwatch_
```

```
"upwatch_checks_down_total": 1,
"upwatch_checks_total": 1,
"upwatch_save_failures_total": 0
```

### Der separate Debug-Listener

`/debug/vars` ist nicht auf dem öffentlichen API-Port erreichbar, sondern auf einem eigenen Listener, der nur startet, wenn `UPWATCH_DEBUG_ADDR` gesetzt ist:

```go
// serveDebug exposes metrics (expvar) and profiles (pprof) on a separate
// listener. Bind it to localhost: profiles and runtime stats are not for
// the public internet.
func serveDebug(addr string, logger *slog.Logger) {
	mux := http.NewServeMux()
	mux.Handle("GET /debug/vars", expvar.Handler())
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	// ...
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		logger.Error("debug listener", "err", err)
	}
}
```

Dort hängt auch `pprof` für CPU- und Speicherprofile (Teil 14). Beides verrät viel über das Innenleben des Prozesses – Speicherverbrauch, Kommandozeile, Stacktraces – und gehört deshalb auf `127.0.0.1` oder in ein internes Netz.

Ein Detail, das man kennen muss: Die Packages `expvar` und `net/http/pprof` registrieren sich beim Import selbst auf `http.DefaultServeMux`. Ein Dienst, der seine öffentliche API über `http.DefaultServeMux` ausliefert, veröffentlicht damit ungewollt `/debug/vars` und `/debug/pprof/`. upwatch benutzt für die API einen eigenen `ServeMux` (Teil 08) und für den Debug-Listener einen zweiten, deshalb landet nichts davon auf dem öffentlichen Port.

### Prometheus und OpenTelemetry als nächste Schritte

`expvar` ist für einen kleinen Dienst ein guter Anfang: keine Abhängigkeit, ein JSON-Endpunkt, fertig. Es hat aber Grenzen. Es gibt keine Labels (also kein „Checks pro Ziel“ oder „pro Statuscode“), keine Histogramme für Antwortzeiten, und das Format ist nicht das, was Monitoring-Systeme erwarten.

Der übliche nächste Schritt ist der Prometheus-Client (`github.com/prometheus/client_golang`): Counter, Gauges und Histogramme mit Labels, ein `/metrics`-Endpunkt im Prometheus-Format, den Prometheus, Grafana Agent oder VictoriaMetrics abfragen. Die Alternative ist OpenTelemetry Metrics, das Metriken herstellerneutral an einen Collector schickt. upwatch baut beides bewusst nicht ein; die drei Zähler wären mit beiden Bibliotheken in wenigen Zeilen umgestellt, und der Debug-Listener ist schon der Ort, an dem `/metrics` hängen würde.

## Tracing und der Context

Tracing beantwortet eine Frage, die Logs und Metriken schlecht beantworten: Wohin ist bei diesem einen Request die Zeit gegangen? Ein Trace besteht aus Spans – „HTTP-Request“, darin „SQL-Abfrage“, darin vielleicht „ausgehender HTTP-Call“ –, die über Prozessgrenzen hinweg mit einer Trace-ID verbunden werden.

In Go ist OpenTelemetry (`go.opentelemetry.io/otel`) der Standard dafür. Und hier zahlt sich etwas aus, das in Teil 07 vielleicht pedantisch wirkte: Der aktuelle Span reist im `context.Context`. Eine Middleware wie `otelhttp` startet für jeden Request einen Span und legt ihn in `r.Context()`. Jede Funktion, die diesen Context weiterreicht – an `QueryRowContext`, an `http.NewRequestWithContext` –, kann darunter Kind-Spans anlegen, und instrumentierte Treiber und Clients tun das automatisch. Weil upwatch den Context konsequent durch alle Schichten reicht, wäre Tracing eine Frage von Middleware und Initialisierung, nicht von Umbauten.

Dieselbe Verbindung gibt es zu den Logs: Die Methoden `InfoContext`, `WarnContext` und so weiter geben den Context an den Handler weiter. Ein Handler, der den aktuellen Span kennt, kann Trace- und Span-ID automatisch an jeden Eintrag hängen. Dann führt dich eine Logzeile mit einem Klick zum zugehörigen Trace. upwatch benutzt die Context-Varianten derzeit nicht, weil sein Handler damit nichts anfangen würde; mit OpenTelemetry wäre das die erste Änderung.

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

```bash
curl -s localhost:18080/healthz; curl -s localhost:18080/readyz
```

```
{"status":"ok"}
{"status":"ready"}
```

Die Unterscheidung kommt aus Kubernetes, gilt aber für jeden Orchestrator und Load Balancer. **Liveness** (`/healthz`) fragt: Lebt der Prozess, oder hängt er? Schlägt sie fehl, wird der Prozess neu gestartet. **Readiness** (`/readyz`) fragt: Kann diese Instanz gerade Anfragen bedienen? Schlägt sie fehl, bekommt die Instanz vorübergehend keinen Traffic, wird aber nicht neu gestartet.

Deshalb prüft `healthz` absichtlich keine Abhängigkeiten. Wäre die Datenbank kurz nicht erreichbar und `healthz` würde das melden, würde der Orchestrator alle Instanzen gleichzeitig neu starten – was am Problem nichts ändert und es oft verschlimmert. `readyz` dagegen prüft die Datenbank mit einem eigenen Timeout von einer Sekunde, damit der Check selbst nicht hängt. Die Antwort enthält nur „database unavailable“; der eigentliche Fehler landet im Log (Teil 08 und 13).

## Graceful Shutdown

Ein Dienst wird ständig beendet: bei jedem Deployment, bei jedem Neustart durch systemd, wenn Kubernetes einen Pod verschiebt. Die Plattform schickt dafür `SIGTERM` und wartet eine Weile, bevor sie mit `SIGKILL` hart abbricht. Ein Dienst, der `SIGTERM` ignoriert, verliert laufende Requests und halb geschriebene Daten. `main.go` in upwatch ist im Wesentlichen eine sorgfältige Antwort auf dieses Signal. Gehen wir es durch.

### run() statt os.Exit

```go
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "upwatch:", err)
		os.Exit(1)
	}
}
```

`os.Exit` beendet den Prozess sofort, **ohne** `defer`-Anweisungen auszuführen. Stünde die ganze Logik in `main` und würde sie bei einem Fehler `os.Exit(1)` oder `log.Fatal` aufrufen, liefe `defer store.Close()` nie, und die SQLite-Datenbank würde nicht sauber geschlossen. Deshalb gibt `run` einen Fehler zurück, alle `defer` in `run` laufen beim Verlassen, und erst danach ruft `main` `os.Exit(1)`. Das ist ein verbreitetes Muster in Go-Programmen, und es macht `run` nebenbei testbar.

### signal.NotifyContext

```go
// ctx is canceled on Ctrl+C or SIGTERM (sent by systemd, Docker, k8s).
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()
```

`signal.NotifyContext` verbindet Signale mit dem Context-Mechanismus aus Teil 07: Kommt `SIGINT` (Ctrl+C) oder `SIGTERM`, wird `ctx` abgebrochen. Dieser Context geht an den Scheduler, und alles, was davon abgeleitet ist, sieht den Abbruch. Es gibt keinen globalen „shutting down“-Schalter, den jede Komponente abfragen müsste.

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

Der Scheduler läuft über `WaitGroup.Go` (seit Go 1.25), damit `main` später auf ihn warten kann. Der HTTP-Server läuft in einer eigenen Goroutine, weil `ListenAndServe` blockiert. Zwei Dinge können jetzt passieren, und das `select` wartet auf das erste.

Entweder der Server scheitert – typischerweise, weil der Port belegt ist. Dann bricht `stop()` den Context ab, der Scheduler fährt herunter, und `run` gibt den Fehler zurück. Der Channel hat Puffergröße 1, damit die Server-Goroutine ihren Fehler abliefern kann, auch wenn gerade niemand liest. `ListenAndServe` gibt nach einem `Shutdown` immer `http.ErrServerClosed` zurück; das ist kein Fehler und wird deshalb herausgefiltert.

Oder das Signal kommt, `ctx.Done()` wird geschlossen, und der geordnete Abbau beginnt.

### srv.Shutdown mit Timeout und Scheduler-Drain

```go
// Graceful shutdown: stop accepting connections and give in-flight
// requests up to 15 seconds to finish. The scheduler sees the same
// canceled ctx and drains its workers in parallel.
shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()
if err := srv.Shutdown(shutdownCtx); err != nil {
	return fmt.Errorf("shutdown: %w", err)
}
wg.Wait()

logger.Info("stopped")
return nil
```

`srv.Shutdown` schließt sofort alle Listener, sodass keine neuen Verbindungen angenommen werden, schließt untätige Verbindungen und wartet dann, bis alle laufenden Requests fertig sind. Der Timeout-Context begrenzt dieses Warten auf 15 Sekunden. Er basiert auf `context.Background()`, nicht auf `ctx` – der ist ja schon abgebrochen, und ein abgeleiteter Context wäre es sofort auch.

Parallel dazu fährt der Scheduler herunter, weil er denselben abgebrochenen `ctx` sieht. Wie das intern abläuft, zeigt Teil 06: Der Dispatcher hört auf zu verteilen, laufende Checks werden über den Context abgebrochen und verworfen, und der Writer speichert noch alle Ergebnisse, die schon fertig waren – mit `context.WithoutCancel`, damit der Abbruch das Speichern nicht verhindert. `wg.Wait()` wartet, bis das erledigt ist.

Danach kehrt `run` zurück, und die `defer`-Kette läuft rückwärts ab: `cancel()`, `store.Close()`, `stop()`. Die Datenbank wird also erst geschlossen, wenn weder API noch Scheduler sie mehr benutzen. Die Reihenfolge ergibt sich aus der Struktur des Codes, nicht aus Konfiguration.

Die Zeiten müssen zur Plattform passen. Die systemd-Unit gibt mit `TimeoutStopSec=30` doppelt so viel Zeit, wie upwatch maximal für die HTTP-Seite braucht. `docker stop` wartet standardmäßig nur zehn Sekunden, bevor es `SIGKILL` schickt; mit `docker stop -t 30` oder `stop_grace_period` in Compose passt das wieder. Kubernetes wartet standardmäßig 30 Sekunden (`terminationGracePeriodSeconds`).

### Ausprobieren

Das Beispiel [beispiele/12-logging-konfiguration-observability/shutdown](../beispiele/12-logging-konfiguration-observability/shutdown/main.go) reduziert das Muster auf einen Server mit einem langsamen Endpunkt, der zwei Sekunden braucht. Starte ihn, schicke einen Request und beende den Server, während der Request läuft:

```bash
go build -o /tmp/shutdown ./beispiele/12-logging-konfiguration-observability/shutdown
/tmp/shutdown -addr 127.0.0.1:8081 &
curl -s -i localhost:8081/slow &
sleep 0.5; kill -TERM %1
```

```
time=2026-10-10T22:33:07.822+02:00 level=INFO msg=listening addr=127.0.0.1:8081
time=2026-10-10T22:33:08.073+02:00 level=INFO msg="slow request started"
time=2026-10-10T22:33:08.550+02:00 level=INFO msg="shutting down" grace=10s
time=2026-10-10T22:33:10.074+02:00 level=INFO msg="slow request finished"
time=2026-10-10T22:33:10.132+02:00 level=INFO msg=stopped
```

Das Signal kommt eine halbe Sekunde nach Beginn des Requests, der Request läuft trotzdem zu Ende, und curl bekommt sein `HTTP/1.1 200 OK` – mit `Connection: close`, weil der Server keine weiteren Requests auf dieser Verbindung annimmt. Mit `-grace 500ms` ist die Frist kürzer als der Request:

```
time=2026-10-10T22:33:17.028+02:00 level=INFO msg="shutting down" grace=500ms
shutdown: graceful shutdown: context deadline exceeded
```

`Shutdown` gibt nach Ablauf der Frist `context.DeadlineExceeded` zurück, das Programm endet mit Exit-Code 1, und der Client verliert die Verbindung. Das ist die richtige Reaktion: lieber ein klar protokollierter Abbruch nach einer Frist als ein Prozess, der beliebig lange auf einen hängenden Client wartet.

Warum `go build` statt `go run`? `go run` startet das kompilierte Programm als Kindprozess. Ein `kill -TERM` auf den `go`-Prozess beendet nur diesen, das Signal kommt beim Programm nicht an, und der Server läuft verwaist weiter. Ctrl+C im Terminal funktioniert dagegen auch mit `go run`, weil das Terminal das Signal an die ganze Prozessgruppe schickt. Für Versuche mit `kill` startest du das Binary deshalb direkt.

### Zwei Verfeinerungen

Das Beispiel ruft nach dem Signal sofort `stop()` auf:

```go
// Restore default signal handling: a second Ctrl+C now kills the
// process instead of being swallowed while we wait.
stop()
```

Solange `stop` nicht aufgerufen ist, fängt `NotifyContext` weitere Signale ab, ohne etwas zu tun. In upwatch läuft `stop` erst als `defer` am Ende von `run`. Ein zweites Ctrl+C während des Herunterfahrens bleibt dort also wirkungslos, bis die bis zu 15 Sekunden vorbei sind. Für einen Dienst unter systemd ist das egal, weil systemd nach `TimeoutStopSec` ohnehin `SIGKILL` schickt; beim lokalen Entwickeln ist das sofortige `stop()` angenehmer.

Die zweite Verfeinerung betrifft Umgebungen mit Load Balancer und mehreren Instanzen. `Shutdown` schließt den Listener sofort. Ein Load Balancer, der die Instanz noch für gesund hält, schickt in den Sekunden danach weiter Anfragen, die abgewiesen werden. Das übliche Gegenmittel: nach dem Signal zuerst `/readyz` auf 503 umschalten, einige Sekunden warten, bis der Load Balancer das bemerkt hat, und erst dann `Shutdown` aufrufen. upwatch läuft als einzelne Instanz und braucht das nicht.

## Im Projekt

- [main.go](../cmd/upwatch/main.go) – `run`, `newLogger`, `slog.SetDefault`, `slog.NewLogLogger`, `signal.NotifyContext`, Server-Timeouts, Graceful Shutdown, `serveDebug`.
- [config.go](../internal/config/config.go) und [config_test.go](../internal/config/config_test.go) – `Load(getenv)`, Defaults, `errors.Join`.
- [scheduler.go](../internal/scheduler/scheduler.go) – expvar-Zähler, `Warn` für „target down“, `Error` für Speicherfehler, `context.WithoutCancel` im Writer.
- [middleware.go](../internal/httpapi/middleware.go) – eine strukturierte Logzeile pro Request.
- [httpapi.go](../internal/httpapi/httpapi.go) – `healthz` und `readyz`.
- [upwatch.service](../deploy/upwatch.service) und [Dockerfile](../Dockerfile) – JSON-Logs und Stop-Timeout im Betrieb.

## Übungen

1. Ergänze die Request-Middleware in upwatch um eine Request-ID: Erzeuge pro Request eine zufällige ID, setze sie als Response-Header `X-Request-Id` und gib sie als Attribut `request_id` in der Logzeile aus. Wie würdest du die ID an Handler weiterreichen, die selbst loggen?
2. Füge einen vierten expvar-Zähler hinzu, der zählt, wie viele Checks mit einem Timeout endeten (`r.Err == "timeout"`). Starte upwatch mit `UPWATCH_DEBUG_ADDR` und prüfe den Wert über `/debug/vars`.
3. Ändere `run` so, dass `stop()` direkt nach `<-ctx.Done()` aufgerufen wird, und probiere mit einem lokal gestarteten Binary aus, was ein zweites Ctrl+C während des Shutdowns bewirkt. Welche Auswirkung hat das auf `defer stop()`?
4. Schreib für `config.Load` einen Testfall, der prüft, dass `UPWATCH_LOG_JSON=yes` mit einer verständlichen Meldung abgelehnt wird. Schau dir dabei an, welche Werte `strconv.ParseBool` tatsächlich akzeptiert.

---

← [Teil 11: Testing](11-testing.md) · [Übersicht](../README.md) · [Teil 13: Security](13-security.md) →
