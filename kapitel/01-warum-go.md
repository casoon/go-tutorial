# Teil 01: Warum gibt es Go?

Nach diesem Kapitel weißt du, welches Problem Go lösen soll, wo die Sprache passt und wo nicht – und du hast upwatch, das Projekt dieses Tutorials, einmal gestartet und mit `curl` ausprobiert.

> **upwatch in diesem Teil:** das ganze Projekt im Überblick, gestartet über [cmd/upwatch/main.go](../cmd/upwatch/main.go)
> **Voraussetzungen:** keine

## Das Problem, aus dem Go entstanden ist

Go wurde ab 2007 bei Google entworfen und 2009 veröffentlicht. Der Anlass war kein akademisches Interesse an Typsystemen, sondern Alltag in großen Codebasen: C++-Builds, die Minuten bis Stunden dauerten, Abhängigkeitsgraphen, die niemand mehr überblickte, und Serverprogramme, die viele Netzwerkverbindungen gleichzeitig bedienen mussten – mit Threads und Locks, die schwer richtig zu bekommen waren. Java löste einen Teil davon, brachte aber eine Laufzeitumgebung, Startzeiten und eine Kultur schwerer Frameworks mit.

Go ist die Antwort auf die Frage: Wie sieht eine Sprache aus, in der viele Leute über Jahre hinweg Serversoftware schreiben, lesen und betreiben können, ohne dass die Werkzeuge im Weg stehen? Was daraus folgt, siehst du am besten an einem laufenden Programm. Deshalb kommt zuerst das Projekt und danach die Liste der Eigenschaften.

## Das Projekt: upwatch

Statt Syntax an `foo` und `bar` zu üben, arbeitest du an einem Programm, das man so tatsächlich betreiben könnte: **upwatch**, einem URL-Monitoring-Dienst. Du legst über eine HTTP-API URLs an, die überwacht werden sollen. upwatch prüft sie regelmäßig – parallel, mit begrenzter Anzahl gleichzeitiger Prüfungen – und speichert für jede Prüfung Statuscode, Antwortzeit und Zeitpunkt. Über die API fragst du die Ergebnisse ab.

```text
┌──────────────────────────────────────────────────────────┐
│ main                                        cmd/upwatch  │
│ liest die Config, erzeugt alle Bausteine, verbindet sie, │
│ startet sie und fährt sie am Ende geordnet herunter      │
└──────────────────────────────────────────────────────────┘

               HTTP (JSON)
   curl ─────────────────────────►  ┌──────────────────────┐
                                    │ HTTP-API             │
                                    │ internal/httpapi     │
                                    └──────────┬───────────┘
                                               │ CreateTarget,
                                               │ Results …
                                               ▼
┌──────────────────────┐  ClaimDue  ┌──────────────────────┐
│ Scheduler + Worker   │◄──────────►│ Store (SQLite)       │
│ internal/scheduler   │ SaveResult │ internal/sqlitestore │
└──────────┬───────────┘            └──────────────────────┘
           │ fällige Ziele an die Worker (Channel)
           ▼
┌──────────────────────┐
│ Checker              │  ein HTTP-GET mit Timeout pro Ziel,
│ internal/check       │  Ergebnis zurück an den Scheduler
└──────────┬───────────┘
           ▼
   example.com, example.org …
```

Diese fünf Bausteine begegnen dir in jedem Kapitel wieder, und die Kapitel benennen sie genau so:

- **HTTP-API** ([httpapi](../internal/httpapi/httpapi.go)) nimmt Anfragen an, prüft Eingaben und antwortet mit JSON.
- **Scheduler und Worker** ([scheduler](../internal/scheduler/scheduler.go)) holen in festen Abständen die fälligen Ziele aus dem Store und verteilen sie auf eine feste Zahl paralleler Worker.
- **Checker** ([check](../internal/check/check.go)) führt eine einzelne HTTP-Prüfung aus und macht daraus ein Ergebnis.
- **Store** ([sqlitestore](../internal/sqlitestore/store.go)) speichert Ziele und Ergebnisse in einer SQLite-Datei.
- **main** ([main.go](../cmd/upwatch/main.go)) verdrahtet alles und kümmert sich um Start und Ende.

Dazu kommen zwei kleine Hilfspakete ohne eigenen Kasten im Bild: [monitor](../internal/monitor/monitor.go) mit den Domänentypen `Target` und `Result`, die alle Bausteine gemeinsam verwenden, und [config](../internal/config/config.go), das die Umgebungsvariablen liest.

### So arbeitest du mit diesem Tutorial

Der Code von upwatch ist fertig und getestet. Du musst ihn nicht abtippen und nicht Kapitel für Kapitel zusammenbauen. Jedes Kapitel erklärt einen Baustein oder ein Sprachmittel, das in diesen Bausteinen steckt, und der Kasten am Kapitelanfang sagt dir, welcher es ist. In den Abschnitten „Jetzt du“ und in den Übungen veränderst du den Code: Du startest upwatch mit anderen Einstellungen, baust einen Fehler ein, ergänzt eine Funktion und lässt die Tests laufen.

Die Kapitel 1 bis 7 legen die Grundlagen – Werkzeuge, Sprache, Denkweise, Fehler, Concurrency, Context – an kleinen Beispielen aus der Monitor-Domäne. Ab Teil 08 geht es Paket für Paket durch upwatch: zuerst die HTTP-API, dann Projektstruktur, Store, Tests, Logging, Security, Performance und Deployment.

## Jetzt du: upwatch starten

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

Du hast gerade alle fünf Bausteine benutzt: Die HTTP-API hat das Ziel angenommen, der Store hat es gespeichert, der Scheduler hat es als fällig erkannt und einem Worker gegeben, der Checker hat `example.com` abgerufen, und das Ergebnis liegt wieder im Store.

## upwatch ausprobieren

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

Eine Sache wird dir beim Experimentieren auffallen: `http://localhost:9000` als Ziel liefert kein Ergebnis mit Statuscode, sondern einen Fehler „forbidden address“. Der Grund: upwatch ruft URLs ab, die Nutzer vorgeben, und dürfte sonst als Werkzeug dienen, um interne Adressen abzufragen. Für lokale Tests schaltest du den Schutz mit `UPWATCH_ALLOW_PRIVATE_TARGETS=true` ab.

## Was Go ausmacht

Mit dem laufenden Programm im Kopf lassen sich die Eigenschaften einordnen, die dir in diesem Tutorial immer wieder begegnen:

| Eigenschaft | Go | C#/.NET | Node/TS |
|---|---|---|---|
| Ausführung | nativer Maschinencode, kein JIT, Start in Millisekunden | CLR mit JIT (Native AOT optional) | V8 mit JIT |
| Sprachumfang | klein: keine Klassen, Vererbung, Exceptions, Annotationen, Überladung, impliziten Konvertierungen | groß, wächst mit jeder Version | TypeScript-Typsystem sehr ausdrucksstark |
| Builds | Sekunden; keine Header, keine zyklischen Imports | MSBuild, inkrementell | `tsc` plus Bundler |
| Speicher | Garbage Collector auf kurze Pausen optimiert; Structs sind Werte | Garbage Collector; Werttypen über `struct` | Garbage Collector; Objekte immer Referenzen |
| Parallelität | Goroutines und Channels, kein `async`/`await` | Tasks mit `async`/`await` | Event Loop mit `async`/`await`, Worker Threads |
| Deployment | eine einzelne Datei | installiertes Runtime oder self-contained, Native AOT mit Einschränkungen | Node plus `node_modules` |
| Cross-Compilation | `GOOS=linux GOARCH=arm64 go build` | `dotnet publish -r linux-arm64` | entfällt, solange keine nativen Module im Spiel sind |
| Standardbibliothek | HTTP, JSON, TLS, SQL-Interface, Logging, Tests, Profiling | umfangreich, ergänzt durch NuGet | schmal, vieles kommt aus npm |
| Frameworks | selten; Verdrahtung von Hand in `main` | ASP.NET Core | Express, NestJS |

Drei Zeilen dieser Tabelle verdienen eine Erklärung.

**Die kleine Sprache.** Die Spezifikation passt auf einen Nachmittag. Das ist Absicht: Code wird viel öfter gelesen als geschrieben, und eine kleine Sprache bedeutet, dass fremder Code ähnlich aussieht wie eigener. Der Preis ist, dass manches länger wird – am sichtbarsten bei der Fehlerbehandlung (Teil 05).

**Parallelität ohne `async`.** Goroutines sind leichtgewichtige Ausführungsstränge, die die Laufzeitumgebung auf wenige Betriebssystem-Threads verteilt; Zehntausende davon sind kein Problem. Weil es kein `async`/`await` gibt, gibt es auch keine Trennung in „farbige“ Funktionen: Jede Funktion darf blockieren, und die Laufzeit sorgt dafür, dass der Thread währenddessen etwas anderes tut. Die Worker im Bild oben sind genau solche Goroutines (Teil 06).

**Wenig Framework.** Weil die Standardbibliothek HTTP-Server, JSON und Datenbank-Interface mitbringt, verwenden Go-Projekte selten ein Framework, das die Struktur vorgibt. upwatch kommt mit genau einer externen Abhängigkeit aus, dem SQLite-Treiber. Die Teile verdrahtest du selbst in `main` – das ist weniger Arbeit, als es klingt (Teil 04 und Teil 09).

Bei der reinen Rechenleistung liegt Go meist in derselben Liga wie C# und Java, nicht ganz bei C oder Rust. Spürbar wird der Unterschied eher beim Speicherverbrauch und beim Start.

## Wo Go passt – und wo weniger

Go ist für eine bestimmte Klasse von Software gebaut: Programme, die über das Netzwerk sprechen, lange laufen, viele Dinge gleichzeitig tun und einfach zu betreiben sein sollen. Das trifft auf erstaunlich viel zu:

- HTTP- und gRPC-APIs, Microservices
- Netzwerkdienste, Proxies, Load Balancer, WebSocket-Server
- Kommandozeilen-Werkzeuge
- Hintergrund-Worker, Job-Queues, Scheduler
- Infrastruktur- und Cloud-Software

Für die letzte Kategorie sprechen die bekannten Namen: Docker, Kubernetes, Terraform, Prometheus, etcd und Caddy sind in Go geschrieben. Diese Werkzeuge müssen auf vielen Plattformen laufen, als einzelne Datei ausgeliefert werden und viele Verbindungen parallel halten – genau Gos Stärken.

Weniger gut passt Go für klassische Desktop-Oberflächen. Es gibt GUI-Bibliotheken, aber keine, die mit WPF, WinUI, Qt oder SwiftUI mithalten kann. Im Browser hat TypeScript keine ernsthafte Konkurrenz; Go kann zwar nach WebAssembly kompilieren, aber die Binaries sind groß und der DOM-Zugriff umständlich. Und bei sehr umfangreichen Enterprise-Anwendungen mit tiefen Domänenmodellen, ORM, Workflow-Engines und Reporting ist ein Ökosystem wie .NET oder Spring oft die pragmatischere Wahl, weil dort fertige Bausteine bereitliegen, die du in Go selbst zusammensetzen müsstest. Go kann das alles technisch, aber der Vorteil gegenüber dem bestehenden Werkzeug ist dort klein.

Eine nützliche Faustregel: Wenn das Programm hauptsächlich Daten zwischen Netzwerk, Datenbank und anderen Diensten bewegt und dabei viel gleichzeitig passiert, ist Go ein sehr guter Kandidat. Wenn es hauptsächlich Benutzeroberfläche ist oder ein riesiges, bereits gelöstes Fachproblem, eher nicht.

## Warum kein Todo-Service

Ein Todo-Service ist CRUD: Request rein, Datenbank, Response raus. Damit lernst du `net/http` und JSON, aber nicht das, wofür man Go eigentlich einsetzt. upwatch führt dich durch die ganze Kette, und jedes Glied ist ein Kapitel:

```
HTTP-API → JSON → Datenbank → Goroutines → Channels → Context
  → Worker → Tests → Logging → Graceful Shutdown → Single Binary
```

Ein Monitoring-Dienst muss Dinge gleichzeitig tun (viele URLs prüfen, während die API Anfragen beantwortet). Er muss mit langsamen und toten Gegenstellen umgehen (Timeouts, Context). Er läuft dauerhaft und muss sauber beendet werden können, ohne Ergebnisse zu verlieren (Graceful Shutdown). Er nimmt URLs von Nutzern entgegen und ruft sie auf – ein echtes Sicherheitsproblem (SSRF, Teil 13). Und er soll als einzelne Datei auf einem Server oder in einem Container laufen (Teil 15). An diesen Themen zeigt sich, ob man Go verstanden hat.

## Vom Quellcode zur einzelnen Datei

`go run` ist für die Entwicklung. Für den Betrieb baust du ein Binary:

```bash
make build        # bin/upwatch für dein System
make build-all    # Linux amd64/arm64, macOS arm64, Windows amd64
```

`make build-all` erzeugt auf einem einzigen Rechner Binaries für vier Plattformen, ohne Docker und ohne zusätzliche Compiler. Möglich ist das, weil upwatch den SQLite-Treiber `modernc.org/sqlite` verwendet, der in reinem Go geschrieben ist; mit cgo wäre für jede Zielplattform ein C-Cross-Compiler nötig. Gemessen auf einem Apple-M-Rechner mit Go 1.27.1 (Voraussetzung bleibt Go 1.26 oder neuer) sind die Dateien jeweils etwa 12 MB groß – darin stecken der HTTP-Server, TLS, die komplette SQLite-Engine und die Go-Laufzeit. Die Datei für Linux kopierst du auf einen Server und startest sie. Mehr ist nicht nötig.

Zum Vergleich: Eine ASP.NET-Core-Anwendung braucht entweder ein installiertes .NET-Runtime oder wird als self-contained Deployment mit Runtime ausgeliefert; Native AOT kommt dem Go-Modell nahe, hat aber Einschränkungen bei Reflection. Eine Node.js-Anwendung braucht Node und ihr `node_modules`-Verzeichnis. In Go ist die einzelne Datei der Normalfall, nicht eine Option.

## Im Projekt

- [cmd/upwatch/main.go](../cmd/upwatch/main.go) – Einstiegspunkt, liest die Konfiguration, verdrahtet alle Pakete, startet HTTP-Server und Scheduler und fährt beides geordnet herunter.
- [internal/config/config.go](../internal/config/config.go) – alle Umgebungsvariablen mit ihren Defaults (`UPWATCH_ADDR`, `UPWATCH_DB`, `UPWATCH_TICK` …).
- [internal/httpapi/httpapi.go](../internal/httpapi/httpapi.go) – die Routen, die du oben mit `curl` aufgerufen hast.
- [Makefile](../Makefile) – `run`, `test`, `check`, `build`, `build-all`.
- [Dockerfile](../Dockerfile) und [deploy/upwatch.service](../deploy/upwatch.service) – die beiden Wege, upwatch zu betreiben (Teil 15).

## Übungen

Musterlösungen mit Begründung stehen im Branch [`loesungen`](https://github.com/casoon/go-tutorial/blob/loesungen/loesungen/01-warum-go.md) – versuch es zuerst selbst.

1. **Aufwärmen:** Starte upwatch mit `UPWATCH_TICK=1s` und lege zwei Ziele an, eines davon mit einer Domain, die es nicht gibt (`https://does-not-exist.invalid`). Vergleiche die Ergebnisse. Welche Felder unterscheiden sich, und welchen Wert hat `up`?
2. **Aufwärmen:** Starte upwatch mit `UPWATCH_LOG_JSON=true` und vergleiche die Logausgabe. Welche Variante würdest du lokal, welche in Produktion verwenden?
3. **Kern:** Lösche ein Ziel mit `curl -X DELETE localhost:8080/targets/1` und frage danach `/targets/1/results` ab. Welchen Statuscode bekommst du, und wo im Code wird er festgelegt? (Tipp: `handleError` in [httpapi.go](../internal/httpapi/httpapi.go).)
4. **Kern:** Führe `make build-all` aus und prüfe mit `file bin/*`, für welche Plattformen die Dateien gebaut wurden.

---

[Übersicht](../README.md) · [Teil 02: Toolchain und Module](02-toolchain-und-module.md) →
