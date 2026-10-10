# Teil 01: Warum gibt es Go?

Nach diesem Kapitel weißt du, welches Problem Go lösen soll, wo die Sprache passt und wo nicht – und du hast upwatch, das Projekt dieses Tutorials, einmal gestartet und mit `curl` ausprobiert.

## Das Problem, aus dem Go entstanden ist

Go wurde ab 2007 bei Google entworfen und 2009 veröffentlicht. Der Anlass war kein akademisches Interesse an Typsystemen, sondern Alltag in großen Codebasen: C++-Builds, die Minuten bis Stunden dauerten, Abhängigkeitsgraphen, die niemand mehr überblickte, und Serverprogramme, die viele Netzwerkverbindungen gleichzeitig bedienen mussten – mit Threads und Locks, die schwer richtig zu bekommen waren. Java löste einen Teil davon, brachte aber eine Laufzeitumgebung, Startzeiten und eine Kultur schwerer Frameworks mit.

Go ist die Antwort auf die Frage: Wie sieht eine Sprache aus, in der viele Leute über Jahre hinweg Serversoftware schreiben, lesen und betreiben können, ohne dass die Werkzeuge im Weg stehen? Daraus ergeben sich die Eigenschaften, die dir in diesem Tutorial immer wieder begegnen.

**Kompiliert und schnell.** Go übersetzt in nativen Maschinencode. Es gibt keine VM wie die CLR oder die JVM und keinen JIT, der erst warmlaufen muss. Ein Go-Programm ist nach Millisekunden bereit. In der reinen Rechenleistung liegt Go meist in derselben Liga wie C# und Java, nicht ganz bei C oder Rust – der Unterschied entsteht vor allem im Speicherverbrauch und beim Start.

**Eine bewusst kleine Sprache.** Die Spezifikation passt auf einen Nachmittag. Es gibt keine Klassen, keine Vererbung, keine Exceptions, keine Annotationen, keine Überladung, keine impliziten Konvertierungen. Das ist Absicht: Code wird viel öfter gelesen als geschrieben, und eine kleine Sprache bedeutet, dass fremder Code ähnlich aussieht wie eigener. Der Preis ist, dass manches länger wird – am sichtbarsten bei der Fehlerbehandlung (Teil 05).

**Schnelle Builds.** Das Paketmodell ist darauf ausgelegt: keine Header, keine zyklischen Imports, ungenutzte Imports sind ein Compilerfehler. Der Compiler muss für jedes Paket nur die direkten Abhängigkeiten lesen. Ein mittelgroßer Dienst baut in Sekunden, und der Build-Cache macht inkrementelle Builds fast unmerklich.

**Garbage Collection.** Go verwaltet Speicher automatisch wie C# und TypeScript. Der Collector ist auf kurze Pausen optimiert statt auf maximalen Durchsatz – passend für Server, bei denen Latenz zählt. Anders als in C# hast du aber mehr Kontrolle darüber, was überhaupt auf dem Heap landet: Structs sind Werte, und vieles bleibt auf dem Stack (Teil 14).

**Einfache Parallelität.** Goroutines sind leichtgewichtige Ausführungsstränge, die die Laufzeitumgebung auf wenige Betriebssystem-Threads verteilt. Du kannst Zehntausende davon starten. Channels verbinden sie. Es gibt kein `async`/`await` und damit keine Trennung in „farbige“ Funktionen: Jede Funktion kann blockieren, und die Laufzeit kümmert sich darum, dass der Thread währenddessen etwas anderes tut (Teil 06).

**Einfache Deployments.** Ein Go-Programm wird zu einer einzigen Datei kompiliert, die alles enthält, was sie braucht. Kein installiertes Runtime, kein `node_modules`, kein Classpath. Kopieren, starten, fertig.

**Cross-Compilation.** Zwei Umgebungsvariablen genügen, um auf einem Mac ein Linux- oder Windows-Binary zu bauen: `GOOS=linux GOARCH=arm64 go build`. Solange kein C-Code im Spiel ist (cgo), braucht es dafür keine zusätzliche Toolchain.

**Eine starke Standardbibliothek.** HTTP-Server und -Client, JSON, Kryptografie, TLS, Templates, Datenbank-Interface, strukturiertes Logging, Tests, Benchmarks und Profiling sind dabei. upwatch kommt mit genau einer externen Abhängigkeit aus – dem SQLite-Treiber.

**Wenig Framework-Abhängigkeit.** Daraus folgt eine Kultur, die dich als C#- oder TypeScript-Entwickler anfangs irritieren wird: Go-Projekte verwenden selten ein Framework. Es gibt kein ASP.NET Core, kein NestJS, kein Spring, das die Struktur vorgibt. Du verdrahtest die Teile selbst in `main` – und das ist weniger Arbeit, als es klingt (Teil 04 und Teil 09).

## Wo Go passt – und wo weniger

Go ist für eine bestimmte Klasse von Software gebaut: Programme, die über das Netzwerk sprechen, lange laufen, viele Dinge gleichzeitig tun und einfach zu betreiben sein sollen. Das trifft auf erstaunlich viel zu:

- HTTP- und gRPC-APIs, Microservices
- Netzwerkdienste, Proxies, Load Balancer, WebSocket-Server
- Kommandozeilen-Werkzeuge
- Hintergrund-Worker, Job-Queues, Scheduler
- Infrastruktur- und Cloud-Software

Für die letzte Kategorie sprechen die bekannten Namen: Docker, Kubernetes, Terraform, Prometheus, etcd und Caddy sind in Go geschrieben. Das ist kein Zufall. Diese Werkzeuge müssen auf vielen Plattformen laufen, als einzelne Datei ausgeliefert werden und viele Verbindungen parallel halten – genau Gos Stärken.

Weniger gut passt Go für klassische Desktop-Oberflächen. Es gibt GUI-Bibliotheken, aber keine, die mit WPF, WinUI, Qt oder SwiftUI mithalten kann. Im Browser hat TypeScript keine ernsthafte Konkurrenz; Go kann zwar nach WebAssembly kompilieren, aber die Binaries sind groß und der DOM-Zugriff umständlich. Und bei sehr umfangreichen Enterprise-Anwendungen mit tiefen Domänenmodellen, ORM, Workflow-Engines und Reporting ist ein Ökosystem wie .NET oder Spring oft die pragmatischere Wahl, weil dort fertige Bausteine bereitliegen, die du in Go selbst zusammensetzen müsstest. Go kann das alles technisch, aber der Vorteil gegenüber dem bestehenden Werkzeug ist dort klein.

Eine nützliche Faustregel: Wenn das Programm hauptsächlich Daten zwischen Netzwerk, Datenbank und anderen Diensten bewegt und dabei viel gleichzeitig passiert, ist Go ein sehr guter Kandidat. Wenn es hauptsächlich Benutzeroberfläche ist oder ein riesiges, bereits gelöstes Fachproblem, eher nicht.

## Das Projekt: upwatch

Statt Syntax an `foo` und `bar` zu üben, baust du gedanklich ein Programm, das man so tatsächlich betreiben könnte: **upwatch**, einen URL-Monitoring-Dienst. Der Code liegt vollständig im Repository und ist getestet. Ab Teil 08 gehen wir ihn Paket für Paket durch; bis dahin stammen die Beispiele schon aus seiner Domäne.

Die Idee: Du legst über eine HTTP-API URLs an, die überwacht werden sollen. upwatch prüft sie regelmäßig – parallel, mit begrenzter Anzahl gleichzeitiger Prüfungen – und speichert für jede Prüfung Statuscode, Antwortzeit und Zeitpunkt. Über die API kannst du die Ergebnisse abfragen.

```
              HTTP (JSON)
   curl ───────────────────►  ┌───────────────┐
                              │   HTTP-API    │  internal/httpapi
                              └───────┬───────┘
                                      │ CreateTarget, Results …
                                      ▼
┌───────────────┐  ClaimDue   ┌───────────────┐
│   Scheduler   │◄───────────►│   Datenbank   │  internal/sqlitestore
│   (Ticker)    │  SaveResult │   (SQLite)    │
└───────┬───────┘             └───────────────┘
        │ jobs (Channel)                ▲
        ▼                               │ results (Channel)
┌──────────┐ ┌──────────┐ ┌──────────┐  │
│  Worker  │ │  Worker  │ │  Worker  │ ─┘   internal/scheduler
└────┬─────┘ └────┬─────┘ └────┬─────┘
     │            │            │       HTTP GET mit Timeout
     ▼            ▼            ▼       internal/check
  example.com  example.org   …
```

Die Pakete sind klein und haben je eine Aufgabe: [monitor](../internal/monitor/monitor.go) enthält die Domänentypen `Target` und `Result`, [check](../internal/check/check.go) führt eine einzelne HTTP-Prüfung aus, [sqlitestore](../internal/sqlitestore/store.go) speichert, [scheduler](../internal/scheduler/scheduler.go) verteilt die Arbeit auf Worker, [httpapi](../internal/httpapi/httpapi.go) stellt die API bereit, [config](../internal/config/config.go) liest Umgebungsvariablen. [main.go](../cmd/upwatch/main.go) verdrahtet alles.

### Warum kein Todo-Service

Ein Todo-Service ist CRUD: Request rein, Datenbank, Response raus. Damit lernst du `net/http` und JSON, aber nicht das, wofür man Go eigentlich einsetzt. upwatch zwingt dich durch die ganze Kette, und jedes Glied ist ein Kapitel:

```
HTTP-API → JSON → Datenbank → Goroutines → Channels → Context
  → Worker → Tests → Logging → Graceful Shutdown → Single Binary
```

Ein Monitoring-Dienst muss Dinge gleichzeitig tun (viele URLs prüfen, während die API Anfragen beantwortet). Er muss mit langsamen und toten Gegenstellen umgehen (Timeouts, Context). Er läuft dauerhaft und muss sauber beendet werden können, ohne Ergebnisse zu verlieren (Graceful Shutdown). Er nimmt URLs von Nutzern entgegen und ruft sie auf – ein echtes Sicherheitsproblem (SSRF, Teil 13). Und er soll als einzelne Datei auf einem Server oder in einem Container laufen (Teil 15). Das sind die Themen, an denen sich zeigt, ob man Go verstanden hat.

## upwatch starten

Du brauchst Go 1.26 oder neuer (Teil 02 zeigt, wie Go die passende Version notfalls selbst nachlädt). Im Wurzelverzeichnis des Repositorys:

```bash
make run
```

Das ist eine Abkürzung für `UPWATCH_LOG_LEVEL=debug go run ./cmd/upwatch`. `go run` kompiliert das Paket in ein temporäres Verzeichnis und startet es. Ohne `make` geht es direkt:

```bash
go run ./cmd/upwatch
```

upwatch lauscht auf Port 8080 und legt die Datenbank `upwatch.db` im aktuellen Verzeichnis an. Beides lässt sich über Umgebungsvariablen ändern. Der Scheduler sieht standardmäßig alle 5 Sekunden nach fälligen Prüfungen; zum Ausprobieren ist eine Sekunde angenehmer:

```bash
UPWATCH_TICK=1s UPWATCH_DB=/tmp/upwatch.db go run ./cmd/upwatch
```

Ist Port 8080 belegt, setzt du zusätzlich `UPWATCH_ADDR=127.0.0.1:8081`. Die Ausgabe sieht so aus:

```
time=2026-10-10T22:27:50.762+02:00 level=INFO msg=starting version=dev addr=:8080 db=/tmp/upwatch.db
time=2026-10-10T22:27:50.767+02:00 level=INFO msg=listening addr=:8080
```

## upwatch ausprobieren

Lege in einem zweiten Terminal ein Ziel an:

```bash
curl -i -X POST localhost:8080/targets \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com","interval_seconds":10}'
```

```
HTTP/1.1 201 Created
Cache-Control: no-store
Content-Type: application/json
Location: /targets/1
X-Content-Type-Options: nosniff

{"id":1,"url":"https://example.com","interval_seconds":10,"created_at":"2026-10-10T20:27:55.634Z"}
```

Das Intervall muss zwischen 10 Sekunden und 24 Stunden liegen; lässt du es weg, gilt eine Minute. Die erste Prüfung findet sofort beim nächsten Tick statt. Ein paar Sekunden später:

```bash
curl localhost:8080/targets/1/results
```

```json
[{"id":1,"status_code":200,"duration_ms":173,"up":true,"checked_at":"2026-10-10T20:27:55.77Z"}]
```

Die Liste aller Ziele, ein einzelnes Ziel und der Health-Check:

```bash
curl localhost:8080/targets
curl localhost:8080/targets/1
curl localhost:8080/healthz
```

Ungültige Eingaben beantwortet die API mit `400` und einer Fehlermeldung:

```bash
curl -X POST localhost:8080/targets -d '{"url":"ftp://example.com"}'
```

```json
{"error":"invalid url: scheme must be http or https"}
```

Im ersten Terminal protokolliert upwatch jede Anfrage als strukturierte Logzeile. Mit `Ctrl+C` beendest du den Dienst. Achte auf die letzten beiden Zeilen:

```
time=… level=INFO msg="shutting down"
time=… level=INFO msg=stopped
```

Zwischen diesen Zeilen beendet upwatch laufende HTTP-Anfragen, wartet auf die Worker und speichert Ergebnisse, die schon unterwegs waren. Wie das funktioniert, ist Thema von Teil 07 und Teil 15.

Eine Sache wird dir beim Experimentieren auffallen: `http://localhost:9000` als Ziel liefert kein Ergebnis mit Statuscode, sondern einen Fehler „forbidden address“. Das ist Absicht. Wer URLs von Nutzern abruft, darf nicht zulassen, dass damit interne Adressen abgefragt werden. Für lokale Tests schaltest du den Schutz mit `UPWATCH_ALLOW_PRIVATE_TARGETS=true` ab.

## Vom Quellcode zur einzelnen Datei

`go run` ist für die Entwicklung. Für den Betrieb baust du ein Binary:

```bash
make build        # bin/upwatch für dein System
make build-all    # Linux amd64/arm64, macOS arm64, Windows amd64
```

`make build-all` erzeugt auf einem einzigen Rechner Binaries für vier Plattformen, ohne Docker und ohne zusätzliche Compiler. Möglich ist das, weil upwatch den SQLite-Treiber `modernc.org/sqlite` verwendet, der in reinem Go geschrieben ist; mit cgo wäre für jede Zielplattform ein C-Cross-Compiler nötig. Auf einem Apple-M-Rechner mit Go 1.27 sind die Dateien jeweils etwa 12 MB groß – darin stecken der HTTP-Server, TLS, die komplette SQLite-Engine und die Go-Laufzeit. Die Datei für Linux kopierst du auf einen Server und startest sie. Mehr ist nicht nötig.

Zum Vergleich: Eine ASP.NET-Core-Anwendung braucht entweder ein installiertes .NET-Runtime oder wird als self-contained Deployment mit Runtime ausgeliefert; Native AOT kommt dem Go-Modell nahe, hat aber Einschränkungen bei Reflection. Eine Node.js-Anwendung braucht Node und ihr `node_modules`-Verzeichnis. In Go ist die einzelne Datei der Normalfall, nicht eine Option.

## Im Projekt

- [cmd/upwatch/main.go](../cmd/upwatch/main.go) – Einstiegspunkt, liest die Konfiguration, verdrahtet alle Pakete, startet HTTP-Server und Scheduler und fährt beides geordnet herunter.
- [internal/config/config.go](../internal/config/config.go) – alle Umgebungsvariablen mit ihren Defaults (`UPWATCH_ADDR`, `UPWATCH_DB`, `UPWATCH_TICK` …).
- [internal/httpapi/httpapi.go](../internal/httpapi/httpapi.go) – die Routen, die du oben mit `curl` aufgerufen hast.
- [Makefile](../Makefile) – `run`, `test`, `check`, `build`, `build-all`.
- [Dockerfile](../Dockerfile) und [deploy/upwatch.service](../deploy/upwatch.service) – die beiden Wege, upwatch zu betreiben (Teil 15).

## Übungen

1. Starte upwatch mit `UPWATCH_TICK=1s` und lege zwei Ziele an, eines davon mit einer Domain, die es nicht gibt (`https://does-not-exist.invalid`). Vergleiche die Ergebnisse. Welche Felder unterscheiden sich, und welchen Wert hat `up`?
2. Lösche ein Ziel mit `curl -X DELETE localhost:8080/targets/1` und frage danach `/targets/1/results` ab. Welchen Statuscode bekommst du, und wo im Code wird er festgelegt? (Tipp: `handleError` in [httpapi.go](../internal/httpapi/httpapi.go).)
3. Starte upwatch mit `UPWATCH_LOG_JSON=true` und vergleiche die Logausgabe. Welche Variante würdest du lokal, welche in Produktion verwenden?
4. Führe `make build-all` aus und prüfe mit `file bin/*`, für welche Plattformen die Dateien gebaut wurden.

---

[Übersicht](../README.md) · [Teil 02: Toolchain und Module](02-toolchain-und-module.md) →
