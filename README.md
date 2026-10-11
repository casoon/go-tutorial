# Go für echte Projekte

Ein Tutorial in 16 Teilen, das Go nicht als Syntax-Kurs vermittelt, sondern so, wie man es heute für echte Serveranwendungen einsetzt. Am Ende steht **upwatch**: ein kleiner, produktionsnaher URL-Monitor mit HTTP-API, parallelen Checks, SQLite, Tests, strukturiertem Logging, Graceful Shutdown und einem einzelnen Binary zum Deployen.

**Lesen:** [casoon.github.io/go-tutorial](https://casoon.github.io/go-tutorial/) oder direkt hier im Repository unter [`kapitel/`](kapitel/).

Das Tutorial richtet sich an Entwickler, die aus C#, Java oder TypeScript kommen. Es erklärt immer auch, warum Go an einigen Stellen bewusst anders funktioniert – und wo man vertraute Muster besser zu Hause lässt.

## Das Projekt: upwatch

Nutzer legen URLs an. upwatch prüft sie regelmäßig parallel und speichert Statuscode, Antwortzeit und Zeitpunkt.

```text
                 upwatch (ein Prozess, ein Binary)
   ┌─────────────────────────────────────────────────────┐
   │                                                     │
   │   HTTP-API ────────┐            Scheduler           │
   │   (net/http)       │         (Ticker, Claim)        │
   │                    │                │               │
   │                    │       ┌────────┼────────┐      │
   │                    │    Worker   Worker   Worker    │──► geprüfte URLs
   │                    │       └────────┼────────┘      │
   │                    │                │               │
   │                    └──────► SQLite ◄┘               │
   └─────────────────────────────────────────────────────┘
```

## So arbeitest du mit diesem Tutorial

Der Code von upwatch liegt fertig und getestet im Repository. Du schreibst ihn nicht Zeile für Zeile ab, sondern startest ihn in Teil 1 und lernst ihn danach Baustein für Baustein kennen: Die Teile 1–7 legen die Grundlagen an Beispielen aus upwatch, ab Teil 8 geht es Paket für Paket durch den echten Code. Jedes Kapitel hat einen kurzen „Jetzt du“-Moment zum Ausprobieren und Übungen in drei Stufen (Aufwärmen, Kern, Profi), in denen du upwatch veränderst und erweiterst. Zu fast jedem Kapitel gibt es lauffähige Beispiele unter [`beispiele/`](beispiele/). Musterlösungen zu allen Übungen – mit Begründung und als prüfbare Patches – liegen im Branch [`loesungen`](https://github.com/casoon/go-tutorial/tree/loesungen/loesungen).

## Inhalt

**Grundlagen**

1. [Warum gibt es Go?](kapitel/01-warum-go.md)
2. [Toolchain und Module](kapitel/02-toolchain-und-module.md)
3. [Die Sprache, kompakt](kapitel/03-die-sprache-kompakt.md)
4. [Go-Denkweise statt OOP-Denkweise](kapitel/04-go-denkweise.md)
5. [Fehlerbehandlung](kapitel/05-fehlerbehandlung.md)
6. [Concurrency](kapitel/06-concurrency.md)
7. [Context richtig verstehen](kapitel/07-context.md)

**Die Anwendung**

8. [Eine HTTP-API mit net/http](kapitel/08-http-api.md)
9. [Projektstruktur und Architektur](kapitel/09-projektstruktur.md)
10. [Datenbanken mit database/sql](kapitel/10-datenbanken.md)
11. [Testing](kapitel/11-testing.md)
12. [Logging, Konfiguration, Observability](kapitel/12-logging-konfiguration-observability.md)

**Produktion**

13. [Security](kapitel/13-security.md)
14. [Performance und Speicher](kapitel/14-performance.md)
15. [Deployment](kapitel/15-deployment.md)
16. [Go Best Practices für Umsteiger](kapitel/16-best-practices.md)

## Schnellstart

Voraussetzung: Go 1.26 oder neuer.

```bash
git clone https://github.com/casoon/go-tutorial
cd go-tutorial
make run
```

In einem zweiten Terminal legst du ein Ziel an und fragst ein paar Sekunden später die Ergebnisse ab:

```bash
curl -X POST localhost:8080/targets \
  -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com","interval_seconds":10}'
curl localhost:8080/targets/1/results
```

```json
{"id":1,"url":"https://example.com","interval_seconds":10,"created_at":"2026-10-10T20:27:55.634Z"}
[{"id":1,"status_code":200,"duration_ms":173,"up":true,"checked_at":"2026-10-10T20:27:55.77Z"}]
```

Ab hier übernimmt [Teil 1](kapitel/01-warum-go.md).

## Aufbau des Repositorys

| Pfad | Inhalt |
| --- | --- |
| [`kapitel/`](kapitel/) | Die 16 Teile des Tutorials |
| [`beispiele/`](beispiele/) | Lauffähige Beispiele zu den Kapiteln, je ein Verzeichnis pro Kapitel |
| [`cmd/upwatch/`](cmd/upwatch/) | `main`: Konfiguration, Verdrahtung, Graceful Shutdown |
| [`internal/monitor/`](internal/monitor/) | Domänentypen und Validierung |
| [`internal/check/`](internal/check/) | Ein einzelner HTTP-Check, mit SSRF-Schutz |
| [`internal/scheduler/`](internal/scheduler/) | Worker-Pool, der fällige URLs prüft |
| [`internal/sqlitestore/`](internal/sqlitestore/) | Persistenz mit SQLite und eingebetteten Migrationen |
| [`internal/httpapi/`](internal/httpapi/) | JSON-API, Middleware, Health Checks |
| [`internal/config/`](internal/config/) | Konfiguration aus Umgebungsvariablen |
| [`Dockerfile`](Dockerfile), [`deploy/`](deploy/) | Container-Image und systemd-Unit |
| [`site/`](site/) | Die GitHub-Pages-Seite; sie rendert die Kapitel aus `kapitel/` |

## Konfiguration

upwatch liest seine Einstellungen aus Umgebungsvariablen:

| Variable | Standard | Bedeutung |
| --- | --- | --- |
| `UPWATCH_ADDR` | `:8080` | Adresse der HTTP-API |
| `UPWATCH_DB` | `upwatch.db` | Pfad der SQLite-Datenbank |
| `UPWATCH_WORKERS` | `8` | Maximal gleichzeitige Checks |
| `UPWATCH_TICK` | `5s` | Wie oft nach fälligen URLs gesucht wird |
| `UPWATCH_CHECK_TIMEOUT` | `10s` | Timeout pro Check |
| `UPWATCH_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `UPWATCH_LOG_JSON` | `false` | JSON- statt Textlogs |
| `UPWATCH_DEBUG_ADDR` | leer | Adresse für `/debug/vars` und `/debug/pprof`, z. B. `127.0.0.1:6060` |
| `UPWATCH_ALLOW_PRIVATE_TARGETS` | `false` | Checks auf private Adressen erlauben – nur für lokale Tests |

## API

| Methode und Pfad | Beschreibung |
| --- | --- |
| `GET /targets` | Alle URLs |
| `POST /targets` | URL anlegen: `{"url": "...", "interval_seconds": 60}` |
| `GET /targets/{id}` | Eine URL |
| `DELETE /targets/{id}` | URL samt Ergebnissen löschen |
| `GET /targets/{id}/results?limit=50` | Letzte Ergebnisse, neueste zuerst |
| `GET /healthz` | Liveness: Prozess läuft |
| `GET /readyz` | Readiness: Datenbank erreichbar |

## Entwicklung

```bash
make check      # gofmt, go vet, Tests mit Race Detector, Beispiele
make build-all  # Linux, macOS, Windows aus einem Befehl
```

Die Pages-Seite baust du lokal in `site/` mit `pnpm install && pnpm build`.

## Lizenz

[MIT](LICENSE) · Ein Projekt von [CASOON](https://casoon.de)
