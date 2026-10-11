# Lösungen zu Teil 15: Deployment

Zu den Aufgaben: [Teil 15](../kapitel/15-deployment.md#übungen)

## Übung 1: Binary-Größe, Build-Informationen und Version (Aufwärmen)

Gebaut in einem frischen Klon von `main` auf einem Apple M4 Pro mit Go 1.27.1, Ziel `darwin/arm64`:

```bash
CGO_ENABLED=0 go build -trimpath -o upwatch-full ./cmd/upwatch
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o upwatch-sw ./cmd/upwatch
```

| Build | Größe in Bytes |
|---|---|
| `-trimpath` | 17.631.458 |
| `-trimpath -ldflags="-s -w"` | 11.798.642 |
| ohne `-trimpath` | 17.648.034 |
| ohne `-trimpath`, mit `-ldflags="-s -w"` | 11.815.154 |

`-s -w` spart rund ein Drittel, `-trimpath` nur ein paar Kilobyte. `go version -m` zeigt für beide `-trimpath`-Builds exakt dieselben Zeilen (der Unterschied liegt nur im Dateinamen):

```
upwatch-sw: go1.27.1
	path	github.com/casoon/go-tutorial/cmd/upwatch
	mod	github.com/casoon/go-tutorial	v0.0.0-20261010213251-836e744d21d2
	dep	github.com/dustin/go-humanize	v1.0.1	h1:…
	…
	dep	modernc.org/sqlite	v1.60.1	h1:…
	build	-buildmode=exe
	build	-compiler=gc
	build	-trimpath=true
	build	DefaultGODEBUG=tracebacklabels=0,x509sslcertoverrideplatform=0
	build	CGO_ENABLED=0
	build	GOARCH=arm64
	build	GOOS=darwin
	build	GOARM64=v8.0
	build	vcs=git
	build	vcs.revision=836e744d21d2238556f4763e78137b11b90ded39
	build	vcs.time=2026-10-10T21:32:51Z
	build	vcs.modified=false
```

Die Build-Informationen sind also unabhängig von `-s -w`: Sie stehen in einem eigenen Abschnitt des Binarys, nicht in der Symboltabelle. Auffällig ist, dass `-ldflags` hier gar nicht auftaucht. Ohne `-trimpath` steht dort `build	-ldflags="-s -w"`; mit `-trimpath` lässt Go die Linker-Flags weg, weil sie Pfade des Build-Rechners enthalten könnten. Die Zeile `mod` trägt seit einigen Go-Versionen eine Pseudo-Version aus dem Commit statt `(devel)`.

Den Tag habe ich nur im Klon angelegt, nicht im eigentlichen Repository:

```
$ make build          # before the tag
… -ldflags="-s -w -X main.version=836e744" …
$ git tag -a v0.1.0 -m "test tag"
$ make build
… -ldflags="-s -w -X main.version=v0.1.0" …
$ go version -m bin/upwatch | grep mod
	mod	github.com/casoon/go-tutorial	v0.1.0
$ ./bin/upwatch
… level=INFO msg=starting version=v0.1.0 …
$ echo "// x" >> cmd/upwatch/main.go && make build
… -ldflags="-s -w -X main.version=v0.1.0-dirty" …
	mod	github.com/casoon/go-tutorial	v0.1.0+dirty
```

Ohne Tag trägt `git describe --tags --always` den abgekürzten Commit ein, mit Tag den Tag, mit ungespeicherten Änderungen hängt `--dirty` die Markierung an. Die Toolchain kommt unabhängig davon zum selben Ergebnis und schreibt es in die `mod`-Zeile, mit `+dirty` statt `-dirty`.

**Warum so:** `-s -w` ist für ein Server-Binary fast immer richtig: Panics zeigen weiter vollständige Stacktraces, nur Debugger wie Delve verlieren Informationen – und wer debuggen will, baut ohne. Die Abhängigkeitsliste bleibt erhalten, deshalb funktioniert `govulncheck -mode=binary` auch mit gestrippten Binaries. Ein Tag im echten Repository wäre für diese Übung ein Seiteneffekt, der beim nächsten `git push --tags` mitwandert; ein Wegwerf-Klon zeigt dasselbe ohne Risiko.

## Übung 2: vcs.revision zur Laufzeit (Kern)

```go
// cmd/upwatch/main.go
logger.Info("starting", "version", version, "revision", revision(),
	"addr", cfg.Addr, "db", cfg.DBPath)

// revision returns the commit the binary was built from. The go command
// records it only for go build/install inside a VCS checkout with the vcs
// tool available; go run, go test and -buildvcs=false leave it out.
func revision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	return buildSetting(info, "vcs.revision")
}

func buildSetting(info *debug.BuildInfo, key string) string {
	for _, s := range info.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}
```

`buildSetting` ist getrennt, damit ein Test es mit einem selbst gebauten `debug.BuildInfo` prüfen kann. Ein zweiter Test hält fest, dass `revision()` in einem Test-Binary leer ist.

Wann der Wert leer ist, habe ich mit dem gepatchten Code ausprobiert:

```
go run ./cmd/upwatch                            revision=""
go build (Git-Checkout)                         revision=836e744d21d2238556f4763e78137b11b90ded39
go build -buildvcs=false                        revision=""
go build in einer Kopie ohne .git               revision=""
docker build (Dockerfile aus dem Repository)    "revision":""
```

```bash
git apply loesungen/15-deployment/uebung-2.patch
go test ./...
```

**Warum so:** Leer ist der Wert immer dann, wenn der Build keinen Zugriff auf die Versionsverwaltung hat oder ihn nicht nutzt: bei `go run` und `go test`, mit `-buildvcs=false`, außerhalb eines Checkouts und ohne installiertes `git`. Der praktisch wichtigste Fall ist der Docker-Build, denn die [.dockerignore](../.dockerignore) hält `.git` aus dem Build-Kontext heraus. Dort ist `-X main.version` mit `--build-arg VERSION` deshalb der zuverlässigere Weg; `vcs.revision` ergänzt ihn für lokale und CI-Builds, ohne dass jemand an das Flag denken muss. Wer im Image die Revision will, gibt sie ebenfalls als Build-Argument mit, statt `.git` in den Kontext zu nehmen. `vcs.modified` aus denselben Settings ließe sich genauso ins Log schreiben.

## Übung 3: Daten im Named Volume (Kern)

Gelaufen mit Docker 29.5 auf einem Apple M4 Pro. Image, Container und Volume heißen hier `loesung-…`, und der Port ist ein anderer als in der Aufgabe, damit nichts mit einer laufenden Instanz kollidiert:

```bash
docker build -t loesung-upwatch:dev .
docker run -d --name loesung-upwatch -v loesung-upwatch-data:/data \
  -p 127.0.0.1:18359:8080 loesung-upwatch:dev
curl -s -X POST -d '{"url":"https://example.com","interval_seconds":60}' localhost:18359/targets
docker stop loesung-upwatch && docker start loesung-upwatch
curl -s localhost:18359/targets
docker rm -f loesung-upwatch
docker run -d --name loesung-upwatch2 -v loesung-upwatch-data:/data \
  -p 127.0.0.1:18359:8080 loesung-upwatch:dev
curl -s localhost:18359/targets
```

```
{"id":1,"url":"https://example.com","interval_seconds":60,"created_at":"2026-10-11T09:47:56.041Z"}
nach stop/start:
[{"id":1,"url":"https://example.com","interval_seconds":60,"created_at":"2026-10-11T09:47:56.041Z"}]
neuer Container, gleiches Volume:
[{"id":1,"url":"https://example.com","interval_seconds":60,"created_at":"2026-10-11T09:47:56.041Z"}]
```

Aufgeräumt mit `docker rm`, `docker volume rm loesung-upwatch-data` und `docker rmi loesung-upwatch:dev`.

**Warum so:** `docker stop` und `docker start` allein beweisen wenig, denn der Container behält sein Dateisystem auch ohne Volume. Erst der zweite Schritt – Container löschen, einen neuen mit demselben Volume starten – zeigt, dass die Daten im Volume liegen und nicht im Container. Dass das als `nonroot` überhaupt funktioniert, liegt an den zwei Zeilen im Dockerfile, die `/data` mit dem richtigen Besitzer anlegen (siehe „Stolperstelle: Volume und nonroot“). Vorsicht beim Ausprobieren: Ein zweites `docker run` mit demselben Volume, während der erste Container noch läuft, startet eine zweite upwatch-Instanz auf derselben SQLite-Datei – genau das Betriebsmodell, das das Kapitel ausschließt.

## Übung 4: Readiness-Drain (Profi)

Die Frage zuerst: Der Handler von `/readyz` muss erfahren, dass der Shutdown begonnen hat, und erfahren kann er es nur von `main`, denn nur `main` empfängt das Signal. Die Lösung reicht dafür den Kanal `ctx.Done()` des Signal-Contexts bei der Konstruktion an die API weiter – denselben Context, den auch der Scheduler bekommt.

```go
// internal/httpapi/httpapi.go
type API struct {
	store    Store
	logger   *slog.Logger
	draining <-chan struct{}
}

// Once draining is closed, /readyz reports 503 so that load balancers
// stop sending traffic; all other routes keep working. A nil channel
// is never closed.
func New(store Store, logger *slog.Logger, draining <-chan struct{}) http.Handler {

func (a *API) readyz(w http.ResponseWriter, r *http.Request) {
	select {
	case <-a.draining:
		writeError(w, http.StatusServiceUnavailable, "shutting down")
		return
	default:
	}
	// ... database ping as before
}
```

```go
// cmd/upwatch/main.go
srv := &http.Server{
	Addr:    cfg.Addr,
	Handler: httpapi.New(store, logger, ctx.Done()),
	// ...
}
// ...
case <-ctx.Done():
	// Restore the default signal behavior: a second Ctrl+C or
	// SIGTERM during the delay ends the process immediately.
	stop()
	logger.Info("shutting down", "delay", cfg.ShutdownDelay)
}

// Readiness drain: /readyz already answers 503 because ctx is done,
// but the listener stays open. Give load balancers time to notice
// before Shutdown closes it. The platform's stop timeout must cover
// this delay plus the 15 seconds below.
time.Sleep(cfg.ShutdownDelay)
```

`config.Load` liest `UPWATCH_SHUTDOWN_DELAY` mit Default 0. Der vorhandene Helfer `dur` lehnt 0 ab, deshalb hat die Variable eine eigene Prüfung, die `0s` erlaubt und negative Werte ablehnt. `TestReadyzWhileDraining` schließt den Kanal und prüft: `/readyz` 503, `/healthz` und `/targets` weiter 200. Ein echter Lauf mit `UPWATCH_SHUTDOWN_DELAY=3s`:

```
$ curl localhost:18360/readyz            # before SIGTERM
{"status":"ready"} 200
$ kill -TERM $PID
$ curl localhost:18360/readyz
{"error":"shutting down"} 503
$ curl localhost:18360/targets
[] 200
```

```
…11:48:54.582 level=INFO msg=request method=GET path=/readyz status=200 …
…11:48:54.583 level=INFO msg="shutting down" delay=3s
…11:48:55.113 level=INFO msg=request method=GET path=/readyz status=503 …
…11:48:55.137 level=INFO msg=request method=GET path=/targets status=200 …
…11:48:57.585 level=INFO msg=stopped
```

Drei Sekunden nach dem Signal ist der Prozess beendet. Mit `UPWATCH_SHUTDOWN_DELAY=30s` und einem zweiten `SIGTERM` 0,3 Sekunden nach dem ersten war der Prozess sofort weg, ohne „stopped“ im Log – das Standardverhalten des Signals greift wieder.

```bash
git apply loesungen/15-deployment/uebung-4.patch
go test ./...
```

**Warum so:** Ein empfangender Kanal ist die kleinste Schnittstelle, die „etwas hat begonnen“ ausdrückt, und `ctx.Done()` gibt es schon. Die API bekommt damit keine Abhängigkeit auf Signale oder auf `main`, und ein `nil`-Kanal schaltet das Verhalten in Tests ab, weil ein Empfang darauf nie gelingt. Die Alternative, ein `atomic.Bool` mit einer Methode `SetDraining` am `API`-Typ, würde verlangen, dass `New` den konkreten Typ statt `http.Handler` zurückgibt, und `main` müsste ihn zur richtigen Zeit aufrufen. Der Scheduler stoppt in dieser Lösung sofort, nur die API arbeitet während der Wartezeit weiter; das ist gewollt, denn neue Checks in einer Instanz, die gleich endet, hat niemand bestellt. Der Sofort-Abbruch beim zweiten Signal überspringt `store.Close` – mit SQLite im WAL-Modus ist das unkritisch, aber es ist ein bewusster Notausgang, kein normaler Pfad. Und die Rechnung aus dem Kapitel bekommt einen Summanden: Wartezeit plus 15 Sekunden für `Shutdown` müssen unter `TimeoutStopSec`, `docker stop -t` oder `terminationGracePeriodSeconds` bleiben.
