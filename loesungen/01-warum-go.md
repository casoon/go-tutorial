# Lösungen zu Teil 01: Warum gibt es Go?

Zu den Aufgaben: [Teil 01](../kapitel/01-warum-go.md#übungen)

Alle vier Übungen sind Beobachtungsaufgaben: Du startest upwatch, rufst es auf und liest ab, was passiert. Patches gibt es deshalb keine. Die Ausgaben unten stammen aus echten Läufen auf einem Apple-M-Rechner mit Go 1.27.1; Zeitstempel, Dauern und IDs sehen bei dir anders aus. Für die Läufe lauschte upwatch auf `127.0.0.1:18200` bzw. `18201` statt auf Port 8080 – bei dir heißt es in den `curl`-Aufrufen einfach `localhost:8080`.

## Übung 1: Ein erreichbares und ein nicht existierendes Ziel (Aufwärmen)

```bash
UPWATCH_TICK=1s UPWATCH_DB=/tmp/upwatch.db go run ./cmd/upwatch
```

In einem zweiten Terminal:

```bash
curl -X POST localhost:8080/targets -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com","interval_seconds":10}'
curl -X POST localhost:8080/targets -H 'Content-Type: application/json' \
  -d '{"url":"https://does-not-exist.invalid","interval_seconds":10}'
# a few seconds later
curl localhost:8080/targets/1/results
curl localhost:8080/targets/2/results
```

```json
[{"id":2,"status_code":200,"duration_ms":302,"up":true,"checked_at":"2026-10-11T09:36:35.498Z"}]
[{"id":1,"status_code":0,"duration_ms":6,"error":"Get \"https://does-not-exist.invalid\": dial tcp: lookup does-not-exist.invalid: no such host","up":false,"checked_at":"2026-10-11T09:36:35.498Z"}]
```

Drei Felder unterscheiden sich. `status_code` ist beim nicht existierenden Ziel `0`, weil nie eine HTTP-Antwort kam – die DNS-Auflösung ist schon gescheitert. Dafür gibt es ein Feld `error` mit der Fehlermeldung des HTTP-Clients, das beim erfolgreichen Ziel ganz fehlt. Und `up` ist `true` bzw. `false`. Auch `duration_ms` ist sehr verschieden: Der DNS-Fehler kommt nach wenigen Millisekunden, der echte Abruf braucht ein paar hundert.

Im Log des ersten Terminals steht zum kaputten Ziel zusätzlich eine Warnung:

```
level=WARN msg="target down" target_id=2 status=0 err="Get \"https://does-not-exist.invalid\": dial tcp: lookup does-not-exist.invalid: no such host" duration=6.014041ms
```

Ein Detail, über das man stolpert: `id` in der Ergebnisliste ist die ID des Ergebnisses, nicht die des Ziels. Beide Prüfungen liefen im selben Tick parallel, die Prüfung des kaputten Ziels war zuerst fertig und bekam deshalb das Ergebnis mit ID 1.

**Warum so:** `up` wird nicht gespeichert, sondern bei jeder Antwort aus den anderen Feldern berechnet – über `Result.Up()` in [monitor.go](../internal/monitor/monitor.go): kein Fehler und ein Status zwischen 200 und 399. Ein Ergebnis ohne Statuscode, aber mit Fehler, ist also immer „down“. Dass `error` beim erfolgreichen Ziel fehlt, liegt am Struct-Tag `json:"error,omitempty"` in `resultJSON` ([httpapi.go](../internal/httpapi/httpapi.go)); `status_code` hat kein `omitempty` und erscheint deshalb auch als `0`. Für einen Client ist das die nützlichere Wahl: „kein Statuscode“ ist eine Information, ein fehlendes Fehlerfeld bei Erfolg dagegen nur Rauschen.

## Übung 2: Textlogs und JSON-Logs (Aufwärmen)

```bash
UPWATCH_LOG_JSON=true UPWATCH_TICK=1s UPWATCH_DB=/tmp/upwatch.db go run ./cmd/upwatch
```

Gekürzt, nach einem `POST /targets` mit dem kaputten Ziel aus Übung 1:

```
{"time":"2026-10-11T11:36:46.634271+02:00","level":"INFO","msg":"listening","addr":"127.0.0.1:18201"}
{"time":"2026-10-11T11:36:48.156232+02:00","level":"INFO","msg":"request","method":"POST","path":"/targets","status":201,"duration":403708}
{"time":"2026-10-11T11:36:48.638066+02:00","level":"WARN","msg":"target down","target_id":1,"status":0,"err":"Get \"https://does-not-exist.invalid\": …","duration":2285458}
```

Zum Vergleich die entsprechende Zeile aus dem Lauf mit Textlogs (Übung 1):

```
time=2026-10-11T11:36:34.753+02:00 level=INFO msg=request method=POST path=/targets status=201 duration=1.10125ms
```

Inhaltlich sind die Zeilen gleich: dieselben Schlüssel, dieselbe Reihenfolge. Zwei Unterschiede fallen auf. Jede Zeile ist ein vollständiges JSON-Objekt mit Strings in Anführungszeichen und Zahlen als Zahlen. Und `duration` ist im JSON eine ganze Zahl in Nanosekunden (`403708`), im Text dagegen `time.Duration.String()` (`403.708µs`). Auch der Zeitstempel ist im JSON genauer.

Lokal würde ich Text nehmen, in Produktion JSON. So ist es in upwatch auch vorgesehen: Der Default ist Text, das [Dockerfile](../Dockerfile) und die [systemd-Unit](../deploy/upwatch.service) setzen `UPWATCH_LOG_JSON=true`.

**Warum so:** Text liest ein Mensch im Terminal schneller, JSON liest eine Maschine zuverlässiger. Sobald Logs von einem System wie Loki, Elasticsearch oder CloudWatch eingesammelt werden, willst du nach `status=500` oder `target_id=2` filtern, ohne Regex über Freitext zu legen, und Dauern als Zahl summieren können. Die Umschaltung kostet in upwatch eine Zeile in `newLogger` in [main.go](../cmd/upwatch/main.go), weil beide Formate nur verschiedene `slog.Handler` hinter demselben `*slog.Logger` sind – der Code, der loggt, merkt nichts davon (Teil 12).

## Übung 3: Ergebnisse eines gelöschten Ziels (Kern)

```bash
curl -i -X DELETE localhost:8080/targets/1
curl -i localhost:8080/targets/1/results
```

```
HTTP/1.1 204 No Content
Cache-Control: no-store
X-Content-Type-Options: nosniff

HTTP/1.1 404 Not Found
Cache-Control: no-store
Content-Type: application/json
X-Content-Type-Options: nosniff
Content-Length: 29

{"error":"target not found"}
```

Das Löschen beantwortet die API mit `204 No Content`, die Abfrage danach mit `404 Not Found`. Der Statuscode entsteht in drei Schritten:

1. `listResults` in [httpapi.go](../internal/httpapi/httpapi.go) fragt vor den Ergebnissen erst das Ziel ab: `a.store.Target(r.Context(), id)`.
2. `Store.Target` in [store.go](../internal/sqlitestore/store.go) bekommt von SQLite `sql.ErrNoRows` und übersetzt das in `fmt.Errorf("target %d: %w", id, monitor.ErrNotFound)`.
3. `handleError` erkennt mit `errors.Is(err, monitor.ErrNotFound)` den eingewickelten Sentinel-Fehler und schreibt `writeError(w, http.StatusNotFound, "target not found")`.

Die Ergebnisse selbst sind nach dem `DELETE` ebenfalls weg: Die Tabelle `results` verweist mit `ON DELETE CASCADE` auf `targets` ([001_init.sql](../internal/sqlitestore/migrations/001_init.sql)).

**Warum so:** Ohne die Vorabfrage des Ziels käme für ein gelöschtes oder nie existierendes Ziel `200` mit leerer Liste zurück – nicht zu unterscheiden von „Ziel existiert, aber noch keine Prüfung“. Der Kommentar in `listResults` sagt genau das. Dass der Store `monitor.ErrNotFound` liefert statt `sql.ErrNoRows`, hält die HTTP-Schicht frei von SQL-Wissen: `handleError` kennt nur die Fehler der Domäne und bildet sie auf Statuscodes ab, und ein anderer Store müsste nur denselben Sentinel verwenden. Das Muster aus `%w` und `errors.Is` ist Thema von Teil 05.

## Übung 4: Cross-Compilation mit `make build-all` (Kern)

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

Vier Plattformen aus einem Aufruf auf einem Mac: macOS auf Apple Silicon (Mach-O, arm64), Linux auf x86-64 und auf ARM64 (ELF) und Windows auf x86-64 (PE32+). Die Linux-Binaries sind laut `file` statisch gelinkt und gestrippt. `ls -lh bin/` zeigt bei mir 11 bis 12 MB pro Datei.

**Warum so:** Der Go-Compiler bringt die Backends für alle unterstützten Plattformen mit; `GOOS` und `GOARCH` wählen nur das Ziel. Dass dabei keine C-Toolchain gebraucht wird, liegt an `CGO_ENABLED=0` im [Makefile](../Makefile) und daran, dass `modernc.org/sqlite` reines Go ist. „statically linked“ heißt: Das Linux-Binary braucht nicht einmal eine libc auf dem Zielsystem und läuft deshalb auch im Distroless-Image aus Teil 15. „stripped“ kommt von `-ldflags="-s -w"`, das Symboltabelle und DWARF-Debuginfos weglässt.
