# Teil 15: Deployment

Nach diesem Kapitel kannst du upwatch für jede gängige Plattform bauen, als systemd-Dienst oder als kleines Container-Image betreiben und weißt, wie Graceful Shutdown, Health Checks und ein Reverse Proxy zusammenspielen.

## Das Binary ist die Anwendung

Wer eine ASP.NET-Core-Anwendung ausliefert, liefert entweder eine Runtime mit oder setzt eine voraus, dazu einen Ordner mit DLLs und Konfigurationsdateien. Eine Node.js-Anwendung bringt `node_modules` mit, eine Java-Anwendung ein JAR und eine JVM. Self-contained Deployments, Native AOT oder GraalVM Native Image gibt es, aber sie sind Sonderwege mit Einschränkungen.

In Go ist das der Normalfall. `go build` erzeugt eine einzelne ausführbare Datei, die Laufzeit, Garbage Collector, alle Abhängigkeiten und in upwatch auch die SQL-Migrationen (über `embed`, siehe [Teil 10](10-datenbanken.md)) enthält. Auf dem Zielsystem braucht sie kein installiertes Go und, wenn man es richtig macht, nicht einmal eine C-Bibliothek.

```bash
GOOS=linux GOARCH=amd64 go build -o upwatch ./cmd/upwatch
```

Das Ergebnis kopierst du auf einen Linux-Server und startest es. Das ist das ganze Deployment-Modell; alles Weitere in diesem Kapitel sind Varianten davon, wie dieses eine Binary gestartet, überwacht und mit Konfiguration versorgt wird.

## Cross-Compilation

`GOOS` und `GOARCH` wählen Betriebssystem und Architektur. Die Toolchain bringt alles mit, was sie dafür braucht; es gibt keine separaten SDKs oder Cross-Compiler zu installieren. `go tool dist list` zeigt alle unterstützten Kombinationen.

Das [Makefile](../Makefile) baut upwatch für vier Ziele:

```make
build-all:
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/upwatch-linux-amd64   ./cmd/upwatch
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/upwatch-linux-arm64   ./cmd/upwatch
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/upwatch-darwin-arm64  ./cmd/upwatch
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bin/upwatch-windows-amd64.exe ./cmd/upwatch
```

Ausgeführt auf einem Mac mit Apple-Silicon-Prozessor:

```bash
make build-all
file bin/*
```

```
bin/upwatch-darwin-arm64:      Mach-O 64-bit executable arm64
bin/upwatch-linux-amd64:       ELF 64-bit LSB executable, x86-64, version 1 (SYSV), statically linked, Go BuildID=…, stripped
bin/upwatch-linux-arm64:       ELF 64-bit LSB executable, ARM aarch64, version 1 (SYSV), statically linked, Go BuildID=…, stripped
bin/upwatch-windows-amd64.exe: PE32+ executable (console) x86-64, for MS Windows
```

Vier Betriebssystem-/Architektur-Kombinationen von einem Rechner, ohne Docker, ohne virtuelle Maschine. Die Linux-Binaries sind „statically linked“: Sie hängen von keiner Bibliothek auf dem Zielsystem ab. Das `linux-arm64`-Binary läuft auf einem Raspberry Pi genauso wie auf einer ARM-Instanz bei einem Cloud-Anbieter.

### CGO_ENABLED=0 und warum SQLite trotzdem funktioniert

Go kann C-Code über cgo einbinden. Sobald cgo im Spiel ist, braucht der Build einen C-Compiler für die Zielplattform, und das Binary wird in der Regel dynamisch gegen die C-Bibliothek gelinkt. Damit ist die bequeme Cross-Compilation dahin, und das Binary läuft nur auf Systemen mit passender libc.

`CGO_ENABLED=0` schaltet cgo ab. Beim Cross-Compilieren ist das ohnehin der Default, beim nativen Build unter Linux aber nicht: Dort ist cgo aktiv, wenn ein C-Compiler gefunden wird, und Teile der Standardbibliothek (der DNS-Resolver in `net`, `os/user`) nutzen dann die libc. Das explizite `CGO_ENABLED=0` sorgt dafür, dass der Build überall dasselbe statische Ergebnis liefert – auch im Docker-Build-Stage, wo das `golang`-Image einen C-Compiler mitbringt.

Dass upwatch ohne cgo eine SQLite-Datenbank nutzen kann, liegt an der Wahl des Treibers. Der verbreitete Treiber `github.com/mattn/go-sqlite3` bindet die C-Bibliothek von SQLite über cgo ein. `modernc.org/sqlite` ist eine automatische Übersetzung des SQLite-C-Codes nach Go. Das Ergebnis ist etwas langsamer als das Original, aber reines Go und damit cross-kompilierbar wie jedes andere Package. Für upwatch, das pro Sekunde eine Handvoll Ergebnisse schreibt, ist der Unterschied irrelevant; für die Auslieferung ist er entscheidend.

### -trimpath und -ldflags

```make
LDFLAGS := -s -w -X main.version=$(VERSION)
```

`-trimpath` entfernt absolute Pfade des Build-Rechners aus dem Binary. Ohne die Option stehen in Stacktraces Pfade wie `/Users/name/src/...`, und zwei Builds desselben Codes auf verschiedenen Rechnern unterscheiden sich. Mit ihr werden die Builds reproduzierbar.

`-s -w` lässt Symboltabelle und DWARF-Debug-Informationen weg. Stacktraces bei einem Panic funktionieren weiter, weil Go die dafür nötigen Tabellen separat ablegt; nur Debugger wie Delve haben weniger Informationen. Der Effekt auf die Größe ist deutlich: Für `linux-amd64` waren es mit Go 1.27.1 18.408.988 Bytes ohne und 12.345.504 Bytes mit `-trimpath -ldflags="-s -w"`.

`-X main.version=…` setzt eine Paketvariable vom Typ `string` zur Link-Zeit. In [main.go](../cmd/upwatch/main.go) steht:

```go
// version is set at build time: go build -ldflags "-X main.version=v1.0.0"
var version = "dev"
```

Das Makefile füllt `VERSION` mit `git describe --tags --always --dirty`, also etwa `v1.2.0` oder `v1.2.0-3-gabc1234-dirty`. upwatch loggt die Version beim Start, sodass im Log steht, welcher Build läuft.

### Eingebettete Build-Informationen

Unabhängig von `-X` schreibt die Toolchain Informationen über den Build ins Binary. `go version -m` liest sie aus, auch von einem Binary für eine andere Plattform:

```bash
go version -m bin/upwatch-linux-amd64
```

```
bin/upwatch-linux-amd64: go1.27.1
	path	github.com/casoon/go-tutorial/cmd/upwatch
	mod	github.com/casoon/go-tutorial	(devel)
	dep	github.com/dustin/go-humanize	v1.0.1	h1:…
	dep	github.com/google/uuid	v1.6.0	h1:…
	dep	github.com/remyoudompheng/bigfft	v0.0.0-20230129092748-24d4a6f8daec	h1:…
	dep	golang.org/x/sys	v0.48.0	h1:…
	dep	modernc.org/libc	v1.77.1	h1:…
	dep	modernc.org/mathutil	v1.7.1	h1:…
	dep	modernc.org/memory	v1.12.1	h1:…
	dep	modernc.org/sqlite	v1.60.1	h1:…
	build	-buildmode=exe
	build	-compiler=gc
	build	-trimpath=true
	build	CGO_ENABLED=0
	build	GOARCH=amd64
	build	GOOS=linux
	build	GOAMD64=v1
	build	vcs=git
	build	vcs.modified=true
	…
```

Das ist eine vollständige Liste der Abhängigkeiten mit Versionen und Prüfsummen, dazu Go-Version und Build-Einstellungen. Bei einem Build aus einem Git-Checkout mit Commits kommen `vcs.revision` und `vcs.time` dazu. Für Sicherheitsfragen ist das wertvoll: `govulncheck` kann ein fertiges Binary prüfen (`govulncheck -mode=binary bin/upwatch-linux-amd64`), ohne den Quellcode. Zur Laufzeit liefert `runtime/debug.ReadBuildInfo()` dieselben Daten.

## Linux und systemd

Der einfachste Produktionsbetrieb ist ein Linux-Server mit systemd. Kein Container-Runtime, keine Orchestrierung – das Binary und eine Unit-Datei. [deploy/upwatch.service](../deploy/upwatch.service) enthält oben die Installationsschritte:

```bash
sudo cp upwatch /usr/local/bin/
sudo cp deploy/upwatch.service /etc/systemd/system/
sudo systemctl daemon-reload && sudo systemctl enable --now upwatch
```

Die Unit im Einzelnen:

```ini
[Service]
ExecStart=/usr/local/bin/upwatch
Environment=UPWATCH_ADDR=127.0.0.1:8080
Environment=UPWATCH_DB=/var/lib/upwatch/upwatch.db
Environment=UPWATCH_LOG_JSON=true
```

Die Konfiguration kommt über Umgebungsvariablen, genau wie in [Teil 12](12-logging-konfiguration-observability.md) beschrieben. upwatch bindet nur an `127.0.0.1`, weil davor ein Reverse Proxy steht. JSON-Logs gehen nach stdout, und systemd sammelt sie im Journal (`journalctl -u upwatch -o cat`). Eine eigene Logdatei, Logrotation oder ein Log-Framework mit Appendern braucht es nicht.

```ini
DynamicUser=yes
StateDirectory=upwatch
```

`DynamicUser=yes` lässt systemd beim Start einen temporären Benutzer anlegen, den es nirgends sonst gibt. Du musst keinen Service-Account pflegen, und der Prozess hat keine Rechte auf Dateien anderer Benutzer. `StateDirectory=upwatch` legt `/var/lib/upwatch` an und gibt es diesem Benutzer; das ist der einzige Ort, an dem upwatch schreiben darf, und genau dort liegt die Datenbank. `DynamicUser` schaltet außerdem einige Schutzoptionen implizit ein, etwa ein schreibgeschütztes Dateisystem außerhalb der freigegebenen Verzeichnisse.

```ini
KillSignal=SIGTERM
TimeoutStopSec=30
Restart=on-failure
RestartSec=2
```

`systemctl stop` schickt `SIGTERM`. upwatch fängt das mit `signal.NotifyContext` ab ([Teil 07](07-context.md)), beendet den HTTP-Server mit `srv.Shutdown` und einer Frist von 15 Sekunden und lässt den Scheduler seine Worker leeren. `TimeoutStopSec=30` gibt dafür mehr Zeit, als der Shutdown maximal braucht; erst danach folgt `SIGKILL`. Diese beiden Zahlen gehören zusammen: Ist das Timeout des Prozessmanagers kürzer als das des Programms, wird der Graceful Shutdown regelmäßig abgeschnitten. `Restart=on-failure` startet den Dienst neu, wenn er mit einem Fehler endet, etwa weil `run()` einen Fehler zurückgibt und `main` mit `os.Exit(1)` beendet.

```ini
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
PrivateDevices=yes
ProtectKernelTunables=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX
LockPersonality=yes
MemoryDenyWriteExecute=yes
```

Das Hardening folgt einem Prinzip: upwatch braucht Netzwerk und sein Datenverzeichnis, sonst nichts. `ProtectSystem=strict` macht das gesamte Dateisystem schreibgeschützt (bis auf `StateDirectory`), `ProtectHome` blendet Home-Verzeichnisse aus, `PrivateDevices` die meisten Geräte. `RestrictAddressFamilies` erlaubt nur IP- und Unix-Sockets. `MemoryDenyWriteExecute` verbietet Speicherseiten, die gleichzeitig beschreibbar und ausführbar sind – das funktioniert mit Go, weil Go keinen JIT hat. Mit einer .NET- oder Java-Anwendung, deren JIT genau solche Seiten braucht, würde dieser Schalter den Start verhindern.

`systemd-analyze security upwatch` bewertet die Unit und zeigt, welche Optionen noch fehlen. Einen Teil der Liste wirst du nicht übernehmen wollen, aber sie ist eine gute Prüfliste.

## Docker

Das [Dockerfile](../Dockerfile) hat zwei Stufen:

```dockerfile
# Build stage: full Go toolchain, discarded afterwards.
FROM golang:1.27 AS build
WORKDIR /src

# Download modules first so this layer is cached while only code changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
# CGO_ENABLED=0 yields a static binary that runs without libc.
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/upwatch ./cmd/upwatch
# The runtime image has no shell for mkdir. Create the data directory here
# and copy it with the right owner, see the COPY below.
RUN mkdir /out/data
```

Die Build-Stufe nutzt das offizielle `golang`-Image mit vollständiger Toolchain. Die Reihenfolge der `COPY`-Befehle ist Absicht: Docker cached jede Stufe, solange sich ihre Eingaben nicht ändern. `go.mod` und `go.sum` ändern sich selten, also bleibt die Schicht mit `go mod download` im Cache, und ein Build nach einer Codeänderung lädt keine Module neu. Erst danach kommt der restliche Code. Die [.dockerignore](../.dockerignore) hält lokale Datenbanken, `bin/` und die Tutorial-Texte aus dem Build-Kontext.

```dockerfile
# Runtime stage: no shell, no package manager, runs as non-root.
# Contains only CA certificates, tzdata and our binary.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/upwatch /upwatch
# Without this, Docker creates /data for the volume as root, and the
# nonroot process cannot create its database there.
COPY --from=build --chown=nonroot:nonroot /out/data /data

ENV UPWATCH_ADDR=:8080 \
    UPWATCH_DB=/data/upwatch.db \
    UPWATCH_LOG_JSON=true
VOLUME /data
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/upwatch"]
```

Die zweite Stufe übernimmt nur das Binary. `distroless/static` enthält keine Shell, keinen Paketmanager und keine C-Bibliothek, sondern im Wesentlichen CA-Zertifikate, Zeitzonendaten, eine `/etc/passwd` mit dem Benutzer `nonroot` und ein `/tmp`. Das Image ist klein, und wer es angreift, findet darin kein Werkzeug. `UPWATCH_ADDR=:8080` lauscht im Container auf allen Interfaces, weil Docker die Verbindungen von außen über ein eigenes Netzwerk-Interface hereinreicht – anders als in der systemd-Unit, wo `127.0.0.1` richtig ist.

Gebaut wird mit `make docker`, das `VERSION` als Build-Argument übergibt:

```bash
docker build --build-arg VERSION=dev -t upwatch:dev .
docker image ls upwatch:dev
```

Mit Docker 29.5 auf einem Apple M4 Pro (Image für `linux/arm64`) zeigte `docker image ls` für `upwatch:dev` eine Content Size von 5,76 MB und einen Platzbedarf auf der Platte von 23 MB. Die Schicht mit dem Binary ist 11,7 MB groß, alles aus dem distroless-Basisimage zusammen weniger als 1 MB. Zum Vergleich: Das `golang:1.27`-Image der Build-Stufe belegt 1,32 GB. Ohne Multi-Stage-Build würde all das mit ausgeliefert.

### Stolperstelle: Volume und nonroot

`VOLUME /data` deklariert das Datenverzeichnis als Volume, damit die Datenbank einen Container-Neustart überlebt. Die erste Fassung des Dockerfiles hatte nur diese Zeile, ohne das `mkdir` und das zweite `COPY`. Beim Test für dieses Kapitel brach ein einfaches `docker run upwatch:dev` damit sofort ab:

```
{"time":"…","level":"INFO","msg":"starting","version":"dev","addr":":8080","db":"/data/upwatch.db"}
upwatch: create schema_migrations: unable to open database file (14)
```

Die Ursache: Das Verzeichnis `/data` existiert im Image nicht. Docker legt es beim Start für das Volume an, und zwar mit Besitzer root. Der Prozess läuft als `nonroot` (UID 65532) und darf dort nicht schreiben. Bei einem Named Volume ist es dasselbe, denn Docker übernimmt Besitzer und Rechte nur von einem Verzeichnis, das im Image bereits existiert.

Die Abhilfe sind die zwei Zeilen, die jetzt im Dockerfile stehen: In der Build-Stufe ein leeres Verzeichnis anlegen und es mit dem richtigen Besitzer ins Laufzeit-Image kopieren. In distroless gibt es kein `RUN mkdir`, weil keine Shell da ist.

```dockerfile
# build stage
RUN mkdir /out/data

# runtime stage
COPY --from=build --chown=nonroot:nonroot /out/data /data
```

Mit dieser Änderung startete der Container, beantwortete `/readyz` mit 200 und beendete sich bei `docker stop` sauber:

```
{"time":"…","level":"INFO","msg":"listening","addr":":8080"}
{"time":"…","level":"INFO","msg":"request","method":"GET","path":"/readyz","status":200,"duration":765140}
{"time":"…","level":"INFO","msg":"shutting down"}
{"time":"…","level":"INFO","msg":"stopped"}
```

Wenn du einen Bind-Mount (`-v /srv/upwatch:/data`) benutzt, muss das Verzeichnis auf dem Host UID 65532 gehören. Das ist der Preis für einen Container, der nicht als root läuft – und er ist es wert.

### scratch oder distroless

Weil das Binary statisch ist, würde es auch in einem `FROM scratch`-Image laufen, das buchstäblich leer ist. Für upwatch fehlen dort aber drei Dinge, die distroless mitbringt:

- CA-Zertifikate. upwatch ruft `https://`-URLs ab und muss deren Zertifikate prüfen. Ohne `/etc/ssl/certs` scheitert jeder HTTPS-Check mit „certificate signed by unknown authority“. Mit `scratch` müsstest du die Zertifikate aus der Build-Stufe kopieren oder sie mit `golang.org/x/crypto/x509roots/fallback` ins Binary einbetten.
- Zeitzonendaten. upwatch rechnet in UTC und braucht sie nicht, aber sobald jemand `time.LoadLocation("Europe/Berlin")` aufruft, schlägt das ohne `/usr/share/zoneinfo` fehl. Die Alternative ist `import _ "time/tzdata"`, das die Datenbank ins Binary einbettet.
- Ein Benutzer. `USER nonroot` braucht einen Eintrag in `/etc/passwd`; in `scratch` müsstest du eine numerische UID angeben.

Distroless kostet dafür unter einem Megabyte. Für einen Dienst, der HTTPS spricht, ist das die bessere Wahl.

## Konfiguration über die Umgebung

Alle Einstellungen von upwatch kommen aus Umgebungsvariablen ([config.go](../internal/config/config.go)):

| Variable | Default | Bedeutung |
|---|---|---|
| `UPWATCH_ADDR` | `:8080` | Adresse der API |
| `UPWATCH_DEBUG_ADDR` | leer | pprof und expvar, nur auf localhost |
| `UPWATCH_DB` | `upwatch.db` | Pfad der SQLite-Datei |
| `UPWATCH_WORKERS` | `8` | gleichzeitige Checks |
| `UPWATCH_TICK` | `5s` | wie oft nach fälligen Zielen gesucht wird |
| `UPWATCH_CHECK_TIMEOUT` | `10s` | Timeout pro Check |
| `UPWATCH_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `UPWATCH_LOG_JSON` | `false` | JSON statt Text-Logs |
| `UPWATCH_ALLOW_PRIVATE_TARGETS` | `false` | nur für lokale Tests, siehe [Teil 13](13-security.md) |

Dieselben Variablen funktionieren auf dem Laptop (`UPWATCH_LOG_LEVEL=debug go run ./cmd/upwatch`), in der systemd-Unit (`Environment=`), im Dockerfile (`ENV`), mit `docker run -e` und in einem Kubernetes-Deployment (`env:`). Ein ungültiger Wert lässt upwatch beim Start mit allen Fehlern auf einmal abbrechen, statt mit einem Default weiterzulaufen. In einem Rollout ist das gewollt: Die neue Version wird nie bereit, und die alte bleibt im Dienst.

Zwei Variablen, die nicht zu upwatch gehören, sondern zur Go-Laufzeit, sind in Containern einen Blick wert. `GOMEMLIMIT` (siehe [Teil 14](14-performance.md)) sollte etwas unter dem Speicherlimit des Containers liegen. `GOMAXPROCS` musst du seit Go 1.25 unter Linux meist nicht mehr setzen, weil die Laufzeit ein CPU-Limit der cgroup berücksichtigt.

## Graceful Shutdown und Health Checks

Ein Deployment mit Docker, Kubernetes oder einem anderen Orchestrator ersetzt laufende Instanzen. Damit dabei keine Requests verloren gehen, müssen zwei Dinge zusammenpassen: wie der Orchestrator eine Instanz beendet und wie er erkennt, dass eine neue bereit ist.

Zum Beenden schickt jeder Orchestrator zuerst `SIGTERM`, wartet eine Frist ab und schickt dann `SIGKILL`. Bei systemd ist die Frist `TimeoutStopSec`, bei Kubernetes `terminationGracePeriodSeconds` (Default 30 Sekunden), bei `docker stop` sind es 10 Sekunden, einstellbar mit `docker stop -t` oder in Compose mit `stop_grace_period`. upwatch nimmt sich bis zu 15 Sekunden für `srv.Shutdown`. Bei Docker solltest du die Frist also erhöhen, sonst kann ein langsamer Request beim Stoppen abgeschnitten werden. Im Normalfall ist upwatch deutlich schneller fertig: Im Test oben lagen zwischen „shutting down“ und „stopped“ weniger als eine Millisekunde.

Zum Erkennen der Bereitschaft hat upwatch zwei Endpunkte in [httpapi.go](../internal/httpapi/httpapi.go):

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
		// ...
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
```

In Kubernetes gehört `/healthz` an die Liveness-Probe und `/readyz` an die Readiness-Probe. Die Trennung ist wichtig. Schlägt die Liveness-Probe fehl, startet Kubernetes den Container neu. Würde sie die Datenbank prüfen, würde ein kurzer Datenbankausfall alle Instanzen gleichzeitig neu starten lassen, was nichts repariert. Schlägt die Readiness-Probe fehl, nimmt Kubernetes die Instanz nur aus dem Load Balancing, bis sie wieder bereit ist. Bei einem Rolling Update wartet Kubernetes, bis die neue Instanz bereit meldet, bevor es die alte beendet.

```yaml
livenessProbe:
  httpGet: { path: /healthz, port: 8080 }
readinessProbe:
  httpGet: { path: /readyz, port: 8080 }
```

Eine Feinheit beim Beenden: Kubernetes schickt `SIGTERM` und entfernt die Instanz aus den Endpunkten ungefähr gleichzeitig, nicht nacheinander. Für einen kurzen Moment können also noch neue Verbindungen ankommen, während `srv.Shutdown` den Listener schon geschlossen hat. Üblich ist ein `preStop`-Hook mit einigen Sekunden Wartezeit, oder die Anwendung lässt `/readyz` nach `SIGTERM` zuerst 503 melden und schließt den Listener verzögert. upwatch macht beides nicht – bei einer einzelnen Instanz, wie gleich erklärt, spielt das keine Rolle.

Docker selbst kennt die Anweisung `HEALTHCHECK`. In einem distroless-Image gibt es aber weder `curl` noch eine Shell, um sie auszuführen. Wenn du sie brauchst, wäre ein Unterbefehl im Binary selbst der Weg (`/upwatch healthcheck`, der `/healthz` abfragt). Orchestratoren wie Kubernetes prüfen ohnehin von außen per HTTP.

## Reverse Proxy für TLS

upwatch spricht nur HTTP. TLS übernimmt ein Reverse Proxy auf derselben Maschine. Mit Caddy reicht eine Caddyfile, Zertifikate holt und erneuert Caddy selbst bei Let’s Encrypt:

```
upwatch.example.com {
	reverse_proxy 127.0.0.1:8080
}
```

Mit nginx sieht der entsprechende Teil so aus, die Zertifikate kommen etwa von certbot:

```nginx
server {
    listen 443 ssl;
    server_name upwatch.example.com;
    ssl_certificate     /etc/letsencrypt/live/upwatch.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/upwatch.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Am Proxy ist auch der richtige Ort für `Strict-Transport-Security`, Zugriffslogs und, solange upwatch selbst keine hat, eine Authentifizierung. Die Timeouts im Go-Server bleiben trotzdem wichtig: Der Proxy schützt vor vielem, aber nicht vor allem, und upwatch soll auch ohne ihn nicht angreifbar sein.

## Eine Instanz, ehrlich eingeordnet

upwatch speichert in SQLite, und SQLite ist eine Datei auf einem lokalen Dateisystem. Das bestimmt das Betriebsmodell: eine Instanz, auf einer Maschine, mit einem Volume. Zwei Instanzen auf verschiedenen Rechnern können nicht auf dieselbe Datei schreiben; SQLite auf einem Netzwerkdateisystem ist ausdrücklich keine gute Idee, weil die Sperren dort nicht zuverlässig funktionieren. In Kubernetes hieße das: ein Replica, ein PersistentVolume, Update-Strategie `Recreate` statt Rolling Update.

Bei einem Update gibt es damit eine kurze Unterbrechung von wenigen Sekunden. Für einen Monitoring-Dienst ist das vertretbar: Der Scheduler holt beim Start alle überfälligen Ziele über `ClaimDue` nach. Für eine API, die rund um die Uhr ohne Unterbrechung antworten muss, nicht.

Wer horizontal skalieren will, braucht eine Datenbank, die mehrere Prozesse über das Netz bedienen kann, typischerweise PostgreSQL. Die Architektur von upwatch macht diesen Umstieg überschaubar: Der Scheduler und die API kennen nur ihre kleinen `Store`-Interfaces, `ClaimDue` ist schon als Transaktion formuliert und ließe sich in PostgreSQL mit `SELECT … FOR UPDATE SKIP LOCKED` so umsetzen, dass mehrere Instanzen sich die fälligen Ziele teilen. Aber das ist ein eigenes Projekt, kein Konfigurationsschalter. Für die allermeisten Einsätze eines solchen Dienstes ist eine Instanz mit SQLite genau richtig – einfach zu betreiben, einfach zu sichern (`sqlite3 upwatch.db ".backup backup.db"` oder ein Snapshot des Volumes) und schnell genug.

## Im Projekt

- [Makefile](../Makefile): `build` und `build-all` mit `CGO_ENABLED=0`, `-trimpath` und `-ldflags`, `docker` mit `VERSION`.
- [cmd/upwatch/main.go](../cmd/upwatch/main.go): `var version`, `signal.NotifyContext`, `srv.Shutdown` mit 15 Sekunden Frist, `run()` statt `os.Exit` mitten im Code.
- [deploy/upwatch.service](../deploy/upwatch.service): systemd-Unit mit `DynamicUser`, `StateDirectory`, Stop-Timeout und Hardening.
- [Dockerfile](../Dockerfile), [.dockerignore](../.dockerignore): Multi-Stage-Build, distroless, nonroot.
- [internal/httpapi/httpapi.go](../internal/httpapi/httpapi.go): `/healthz` und `/readyz`.
- [internal/sqlitestore/store.go](../internal/sqlitestore/store.go): reiner Go-Treiber, eingebettete Migrationen.
- [.github/workflows/ci.yml](../.github/workflows/ci.yml): `make build-all` als letzter CI-Schritt, damit Cross-Compilation nicht unbemerkt kaputtgeht.

## Übungen

1. Baue upwatch einmal mit und einmal ohne `-ldflags="-s -w"` und vergleiche die Größen. Prüfe mit `go version -m`, welche Build-Informationen in beiden Fällen enthalten sind. Lege einen Git-Tag an und schau, was `make build` jetzt als Version einträgt.
2. Lies die Version zur Laufzeit zusätzlich aus `debug.ReadBuildInfo()` und gib `vcs.revision` beim Start im Log aus. Wann ist der Wert leer?
3. Starte den Container mit einem Named Volume (`docker run -v upwatch-data:/data -p 8080:8080 upwatch:dev`), lege ein Ziel an, stoppe und starte den Container neu und prüfe, dass das Ziel noch da ist.
4. Lass `/readyz` nach `SIGTERM` mit 503 antworten, bevor `srv.Shutdown` aufgerufen wird, und warte dazwischen eine konfigurierbare Zeit (`UPWATCH_SHUTDOWN_DELAY`). Welche Komponente muss dafür von wem erfahren, dass der Shutdown begonnen hat?

---

← [Teil 14: Performance und Speicher](14-performance.md) · [Übersicht](../README.md) · [Teil 16: Go Best Practices für Umsteiger](16-best-practices.md) →
