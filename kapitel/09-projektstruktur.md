# Teil 09: Projektstruktur und Architektur

Nach diesem Kapitel weißt du, wie man ein Go-Projekt in Packages aufteilt, warum upwatch genau so aussieht und woran du überkomplizierte Strukturen erkennst, bevor du sie übernimmst.

> **upwatch in diesem Teil:** die Aufteilung in [cmd/upwatch](../cmd/upwatch/main.go) und die Packages unter [internal/](../internal/), zusammengesteckt in [main.go](../cmd/upwatch/main.go)
> **Voraussetzungen:** Teil 02 und 04; Teil 06 bis 08 helfen, die Packages einzuordnen

## Das Problem mit den Vorlagen aus dem Internet

Wer „go project structure“ sucht, landet schnell bei Repositories mit zwanzig Top-Level-Verzeichnissen: `api/`, `build/`, `configs/`, `deployments/`, `pkg/`, `internal/app/`, `internal/pkg/`, `init/`, `scripts/`, `tools/`, `web/` und so weiter. Das bekannteste ist `golang-standards/project-layout`. Der Name klingt offiziell, ist es aber nicht: Das Repository gehört keiner Go-Organisation, die README räumt selbst ein, dass es kein offizieller Standard des Go-Teams ist, und Mitglieder des Go-Teams haben dem Anspruch öffentlich widersprochen. Es ist eine Sammlung von Mustern, die man in großen Projekten beobachten kann – nicht der Ausgangspunkt für einen Service mit sieben Packages.

Die offizielle Quelle ist kurz: Die Go-Dokumentation hat eine Seite „Organizing a Go module“ (go.dev/doc/modules/layout). Sie beschreibt im Wesentlichen drei Dinge, die du alle in upwatch wiederfindest: Befehle unter `cmd/`, nicht-öffentlicher Code unter `internal/`, und ansonsten Packages nach Bedarf.

Der Grund, warum sich so viele Vorlagen aufblähen, ist nachvollziehbar. Wer aus Java oder .NET kommt, ist eine Ordnerstruktur gewohnt, die das Framework vorgibt (Maven-Layout, ASP.NET-Projektvorlage, `src/main/java/com/firma/...`). In Go gibt es diese Vorgabe nicht, also füllt man die Lücke mit dem, was man kennt. Das Ergebnis sind Verzeichnisse, die leer bleiben oder genau eine Datei enthalten, und Import-Pfade wie `internal/app/upwatch/pkg/domain/model`.

## Die Struktur von upwatch

```
go-tutorial/
├── cmd/
│   └── upwatch/
│       └── main.go            # wiring only: config, logger, store, server, shutdown
├── internal/
│   ├── monitor/               # domain types, validation, sentinel errors
│   ├── check/                 # one HTTP check, SSRF guard
│   ├── sqlitestore/           # persistence, embedded migrations
│   │   └── migrations/
│   │       └── 001_init.sql
│   ├── scheduler/             # worker pool, periodic checks, metrics
│   ├── httpapi/               # JSON API, middleware, health checks
│   └── config/                # environment variables
├── deploy/upwatch.service
├── Dockerfile
├── Makefile
├── go.mod
└── go.sum
```

Mehr ist es nicht. Jedes Verzeichnis unter `internal/` ist genau ein Package, jedes Package hat eine Aufgabe, die man in einem Satz sagen kann. Die Tests liegen direkt neben dem Code (`monitor_test.go` neben `monitor.go`) – das ist in Go keine Stilfrage, sondern so funktioniert `go test`.

### cmd/

Unter `cmd/<name>/` liegt je ein `package main`, also je ein Programm. `go build ./cmd/upwatch` erzeugt eine Datei namens `upwatch`, weil der Binary-Name aus dem Verzeichnisnamen kommt. Kommt später ein zweites Programm dazu, etwa ein Kommandozeilen-Client oder ein Migrationswerkzeug, bekommt es ein eigenes Verzeichnis daneben.

`main.go` enthält in upwatch keine Logik. Der Kommentar am Anfang sagt es direkt: „main only wires dependencies together. All logic lives in internal/.“ Das ist mehr als Ordnungsliebe. `package main` kann man nicht importieren, also auch nicht aus einem anderen Package heraus testen. Alles, was in `main` steckt, ist nur über den Start des ganzen Programms prüfbar.

### internal/ – vom Compiler erzwungen

`internal` ist das einzige Verzeichnis mit einer Sonderbedeutung, und die setzt der Compiler durch: Ein Package unter `a/b/internal/...` darf nur von Code importiert werden, der unter `a/b/` liegt. Für upwatch heißt das: Nur das Modul `github.com/casoon/go-tutorial` selbst darf `internal/monitor` importieren. Ein fremdes Modul, das es trotzdem versucht, bekommt beim Bauen:

```
package example.com/other
	main.go:3:8: use of internal package github.com/casoon/go-tutorial/internal/monitor not allowed
```

Das ist echte Ausgabe – erzeugt mit einem kleinen Testmodul, das per `replace` auf upwatch zeigt. Für C#-Entwickler ist das am ehesten `internal` auf Assembly-Ebene, nur dass es für ganze Verzeichnisbäume gilt und keine `InternalsVisibleTo`-Ausnahme kennt. In Java kommt das Modulsystem am nächsten: Packages, die `module-info.java` nicht exportiert, sind für andere Module unsichtbar – in Go genügt dafür der Verzeichnisname.

Der praktische Nutzen: Alles unter `internal/` kannst du umbenennen, umbauen oder löschen, ohne dass irgendjemand außerhalb deines Moduls davon betroffen ist. Für eine Anwendung ist das der richtige Default. Ein `pkg/`-Verzeichnis, das in vielen Vorlagen auftaucht, hat dagegen keine Sonderbedeutung – es ist eine Konvention, die signalisieren soll „das hier dürft ihr importieren“. Wenn du keine Bibliothek für andere baust, brauchst du es nicht.

## Packages und Package-Namen

Ein Package ist in Go ein Verzeichnis. Alle `.go`-Dateien darin teilen sich einen Namensraum, unabhängig davon, wie sie heißen. Das ist der wichtigste Unterschied zu C# und Java, wo Namespaces und Dateien unabhängig voneinander sind und eine Klasse pro Datei üblich ist. In Go ist die Datei nur eine Organisationshilfe; die Einheit, über die du nachdenkst, ist das Package. `httpapi` besteht aus `httpapi.go` und `middleware.go`, und die Middleware greift ohne Umweg auf das unexportierte `writeError` zu.

Für Package-Namen gibt es klare Konventionen, und sie haben einen praktischen Grund: Der Name ist bei jeder Verwendung Teil des Aufrufs.

**Kurz, klein, ein Wort.** `monitor`, `check`, `config`, `httpapi`, `sqlitestore`. Keine Unterstriche, kein CamelCase. `sqlitestore` ist zusammengeschrieben, weil `sqlite_store` gegen die Konvention verstößt und `sqliteStore` als Package-Name merkwürdig aussieht.

**Kein Stottern.** Weil der Package-Name vor jedem Bezeichner steht, wiederholt man ihn im Bezeichner nicht. Der Typ heißt `monitor.Target`, nicht `monitor.MonitorTarget`. Der Konstruktor heißt `check.New`, nicht `check.NewChecker` – wobei `check.New` hier einen `*check.Checker` liefert, was beim Lesen trotzdem eindeutig ist. In der Standardbibliothek siehst du das überall: `http.Server`, nicht `http.HTTPServer`; `bytes.Buffer`, nicht `bytes.ByteBuffer`.

**Kein `util`, `common`, `helpers`, `base`, `shared`.** Diese Namen sagen nichts darüber, was drin ist, und ziehen deshalb alles an, was keinen anderen Platz findet. Nach einem Jahr importiert jedes Package `util`, und `util` importiert die halbe Anwendung. Wenn du eine Hilfsfunktion hast, gehört sie entweder in das Package, das sie benutzt (unexportiert), oder in ein Package, das nach seinem Inhalt benannt ist. Die Standardbibliothek hat `strings`, `strconv`, `slices` – nicht `stringutil`.

**Der Name beschreibt, was das Package bereitstellt, nicht was es enthält.** `monitor` stellt die Begriffe des Monitorings bereit. `models` oder `types` würde nur sagen, dass Typen drin sind.

## Abhängigkeitsrichtung

Die wichtigste Architekturentscheidung in upwatch steht nicht in der Ordnerstruktur, sondern in den Imports. Wer welches Package importiert, ergibt dieses Bild:

```text
                       cmd/upwatch
        ┌──────────┬───────┼─────────┬────────────┐
        ▼          ▼       ▼         ▼            ▼
     config     check  httpapi  scheduler  sqlitestore
                   │       │         │            │
                   └───────┴────┬────┴────────────┘
                                ▼
                             monitor
```

## Jetzt du: die Imports selbst ansehen

Das Bild musst du nicht glauben, `go list` zeigt es dir. Der Befehl gibt für jedes Package die direkten Imports aus; `sed` kürzt den Modulpfad weg:

```bash
go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./cmd/... ./internal/... \
  | sed 's|github.com/casoon/go-tutorial/||g'
```

Prüfe in der Ausgabe zwei Dinge: `monitor` importiert kein anderes Package aus `internal/`, und keines der Packages in der mittleren Reihe importiert ein anderes aus derselben Reihe.

<details><summary>Ausgabe von <code>go list</code></summary>

```text
cmd/upwatch: context errors expvar fmt internal/check internal/config internal/httpapi internal/scheduler internal/sqlitestore log/slog net/http net/http/pprof os os/signal sync syscall time
internal/check: context errors fmt internal/monitor io net net/http net/netip syscall time
internal/config: errors fmt log/slog os strconv time
internal/httpapi: context encoding/json errors fmt internal/monitor log/slog net/http strconv time
internal/monitor: errors fmt net/url strings time unicode
internal/scheduler: context expvar internal/monitor log/slog sync time
internal/sqlitestore: context database/sql embed errors fmt internal/monitor io/fs modernc.org/sqlite modernc.org/sqlite/lib net/url sort time
```

</details>

Dann der Gegenversuch: Füge in [monitor.go](../internal/monitor/monitor.go) testweise den Import `_ "github.com/casoon/go-tutorial/internal/sqlitestore"` hinzu und starte `go build ./...`:

```text
package github.com/casoon/go-tutorial/cmd/upwatch
	imports github.com/casoon/go-tutorial/internal/check from main.go
	imports github.com/casoon/go-tutorial/internal/monitor from check.go
	imports github.com/casoon/go-tutorial/internal/sqlitestore from monitor.go
	imports github.com/casoon/go-tutorial/internal/monitor from store.go: import cycle not allowed
```

Der Compiler zeigt die ganze Kette bis zum Zyklus. Warum Go das verbietet, steht weiter unten. Nimm die Zeile danach wieder heraus.

## Wer was kennen darf

`monitor` ist ein Blatt: Es importiert nur die Standardbibliothek, und auch dort nichts aus `net/http` oder `database/sql`. Jedes andere Package darf `monitor` importieren, weil dadurch keine Infrastruktur mitkommt. Mit `go list -deps` siehst du den Unterschied in der transitiven Hülle:

```bash
go list -deps ./internal/monitor | wc -l     # 66
go list -deps ./cmd/upwatch | wc -l          # 261
```

Von den 66 Packages in der Liste für `monitor` ist bis auf `monitor` selbst jedes Teil der Standardbibliothek (inklusive ihrer internen Packages). Ein Test für `monitor` kompiliert deshalb schnell und braucht weder Datenbank noch Netzwerk.

Die mittlere Ebene – `check`, `httpapi`, `scheduler`, `sqlitestore` – kennt sich gegenseitig nicht. `scheduler` braucht etwas, das Ziele liefert und Ergebnisse speichert, importiert aber nicht `sqlitestore`. Stattdessen definiert es selbst, was es braucht:

```go
// Store is what the scheduler needs from persistence. It is defined here,
// at the consumer, and only lists the two methods actually used.
type Store interface {
	ClaimDue(ctx context.Context, now time.Time, limit int) ([]monitor.Target, error)
	SaveResult(ctx context.Context, r monitor.Result) error
}
```

`*sqlitestore.Store` erfüllt dieses Interface, ohne davon zu wissen – das ist das implizite Interface aus Teil 04. `httpapi` hat sein eigenes `Store`-Interface mit den sechs Methoden, die die API braucht. Zusammengesteckt wird alles in `main.go`:

```go
store, err := sqlitestore.Open(ctx, cfg.DBPath)
// ...
checker := check.New(check.NewClient(cfg.AllowPrivate), cfg.CheckTimeout)
sched := scheduler.New(store, checker, logger, cfg.Workers, cfg.Tick)

srv := &http.Server{
	Addr:    cfg.Addr,
	Handler: httpapi.New(store, logger),
	// ...
}
```

Das ist Dependency Injection ohne Container. Die Konstruktoren nehmen ihre Abhängigkeiten als Parameter, `main` ruft sie in der richtigen Reihenfolge auf. Wenn eine Abhängigkeit fehlt, kompiliert es nicht. Ein Framework wie der .NET-`ServiceCollection` oder Spring löst ein Problem, das bei sieben Packages nicht existiert: Du kannst die gesamte Verdrahtung auf einem Bildschirm lesen. Es gibt für Go Werkzeuge wie Wire (Codegenerierung) oder Fx (Laufzeit-Container), aber sie lohnen sich erst, wenn `main` tatsächlich unübersichtlich wird – und das passiert später, als man denkt.

## Keine zyklischen Imports

In Go dürfen Packages sich nicht gegenseitig importieren, auch nicht über Umwege. Die Meldung des Compilers hast du in „Jetzt du“ gesehen.

In C# kann Klasse A in Namespace X Klasse B in Namespace Y benutzen und umgekehrt, solange beide in derselben Assembly liegen. Java erlaubt Zyklen zwischen Packages ebenfalls. Go verbietet sie, weil das Package die Einheit für Kompilierung und Initialisierung ist: Jedes Package wird einmal kompiliert, und seine Abhängigkeiten müssen vorher fertig sein. Ohne Zyklen lässt sich das parallelisieren und cachen, und die Reihenfolge der `init`-Funktionen ist eindeutig.

Am Anfang empfindet man das Verbot als Hindernis. In der Praxis ist es eine Architekturprüfung, die immer läuft. Ein Zyklus bedeutet fast immer, dass zwei Packages eigentlich eins sind, oder dass ein gemeinsamer Begriff in ein drittes Package gehört. In upwatch ist `monitor` genau dieses dritte Package: `sqlitestore` und `httpapi` reden beide über `Target`, also liegt `Target` in keinem von beiden. Die zweite typische Lösung ist ein Interface beim Konsumenten, wie `scheduler.Store` – dann muss der Konsument den Anbieter gar nicht importieren.

## Controller, Service, Repository?

Wer aus ASP.NET oder Spring kommt, erwartet drei Schichten: Controller nehmen Requests an, Services enthalten die Geschäftslogik, Repositories reden mit der Datenbank. In upwatch fehlt die mittlere Schicht. `httpapi` ruft direkt den Store auf:

```go
// in createTarget, after decoding the request:
target, err := monitor.NewTarget(req.URL, interval)
// ...
created, err := a.store.CreateTarget(r.Context(), target)
```

Den vollständigen Handler zeigt Teil 08.

Das ist Absicht. Eine `TargetService.Create`-Methode würde `monitor.NewTarget` aufrufen und dann `store.CreateTarget` – also exakt das, was der Handler schon tut, nur mit einer zusätzlichen Indirektion, einem zusätzlichen Interface und einem zusätzlichen Test, der nichts prüft, was nicht schon geprüft ist. Die Validierung liegt bereits im Domänen-Package (`NewTarget`), die Persistenz im Store. Dazwischen gibt es keine Logik, die einen Ort bräuchte.

Eine Service-Schicht lohnt sich, sobald es Logik gibt, die mehrere Schritte koordiniert und von mehreren Eingängen genutzt wird. Für upwatch wäre das zum Beispiel ein Alarm-Feature: „Wenn ein Ziel dreimal hintereinander down ist, schicke eine Mail, aber höchstens einmal pro Stunde.“ Das braucht Zugriff auf Ergebnisse, Zustand und einen Mailversand, wird vom Scheduler ausgelöst und vielleicht von der API getestet. Dann entsteht ein Package `alert` mit genau dieser Logik – nicht ein generisches `service`-Package, durch das alles durchgereicht wird.

Der Unterschied zur Schichtenarchitektur ist also nicht „Go mag keine Schichten“, sondern: Schichten entstehen in Go, wenn sie etwas tun, nicht vorsorglich.

## Package by feature oder package by layer

Es gibt zwei grundsätzliche Arten, Code zu gruppieren. **Package by layer** heißt: `controllers/`, `services/`, `repositories/`, `models/`. **Package by feature** heißt: `targets/`, `alerts/`, `users/`, jeweils mit allem, was dazugehört.

In Go funktioniert package by layer schlechter als in Java oder C#, und zwar aus einem technischen Grund: Sichtbarkeit hängt am Package. Wenn `controllers` auf etwas aus `services` zugreifen will, muss es exportiert sein – also für die ganze Anwendung sichtbar. Ein Package `models`, das alle Typen enthält, wird von allen importiert und importiert bald selbst Dinge, und dann bist du beim Zyklus.

upwatch ist eine Mischung, und das ist bei kleinen Services üblich. `monitor` ist das Domänen-Package, die anderen sind nach Verantwortung geschnitten: HTTP rein (`httpapi`), HTTP raus (`check`), Speicher (`sqlitestore`), Zeitsteuerung (`scheduler`). Würde upwatch wachsen – Benutzer, Teams, Alarme, Statusseiten –, wären Feature-Packages wie `alert` oder `statuspage` der natürliche nächste Schritt, nicht ein Unterordner `services/`.

## Wann Unterpackages sinnvoll sind

upwatch hat ein einziges Unterverzeichnis: `internal/sqlitestore/migrations/`. Das ist kein Package, sondern ein Ordner mit SQL-Dateien, die per `embed` ins Binary kommen (Teil 10). Echte Unterpackages gibt es nicht, und das ist bei dieser Größe richtig.

Ein Unterpackage lohnt sich, wenn ein Teil eigenständig ist, eine eigene kleine API hat und von außen nur über diese API benutzt werden soll. Die Standardbibliothek zeigt das: `net/http/httptest` und `net/http/pprof` sind Unterpackages von `net/http`, weil sie optional sind und nicht jeder sie mitkompilieren soll. Würde upwatch zum Beispiel mehrere Check-Arten bekommen (HTTP, TCP, DNS), wäre `check/http`, `check/tcp` eine Überlegung wert – aber erst dann.

Was du vermeiden solltest: Unterpackages nur zur optischen Gliederung, etwa `httpapi/handlers` und `httpapi/middleware`. Die beiden müssten sich gegenseitig exportierte Bezeichner zeigen, die vorher privat sein konnten, und gewinnen dabei nichts. Zwei Dateien im selben Package leisten dieselbe Gliederung ohne diese Kosten.

Eine Faustregel, die sich bewährt: Ein Package darf ruhig mehrere tausend Zeilen und viele Dateien haben, wenn es eine Sache tut. `net/http` hat Dutzende Dateien.

## Klein anfangen

Die beste Struktur für ein neues, kleines Go-Programm ist meistens:

```
mytool/
├── go.mod
└── main.go
```

Für ein Kommandozeilenwerkzeug mit ein paar hundert Zeilen ist das vollkommen in Ordnung, und es ist auch in Go-Kreisen keine Anfängerlösung, sondern üblich. Wenn `main.go` unübersichtlich wird, teilst du sie in mehrere Dateien im selben Package (`main.go`, `check.go`, `store.go`). Erst wenn du einen Teil isoliert testen oder in einem zweiten Programm wiederverwenden willst, ziehst du ihn in ein eigenes Package unter `internal/` – und `main.go` wandert nach `cmd/<name>/`, sobald es ein zweites Binary gibt.

Diese Reihenfolge ist umkehrbar und billig. Das Umziehen eines Packages ist in Go ein Verschieben von Dateien plus ein Anpassen der Import-Pfade, das `gopls` bei Bedarf für dich erledigt. Es gibt keine Projektdateien, keine Assembly-Referenzen, keine Build-Konfiguration pro Ordner. Deshalb musst du die Endstruktur nicht am ersten Tag kennen.

## Im Projekt

- [main.go](../cmd/upwatch/main.go) – die einzige Stelle, an der alle Packages zusammenkommen.
- [monitor.go](../internal/monitor/monitor.go) – das Blatt-Package; der Paketkommentar sagt ausdrücklich, dass es keine Infrastruktur importiert.
- [scheduler.go](../internal/scheduler/scheduler.go) und [httpapi.go](../internal/httpapi/httpapi.go) – je ein eigenes `Store`-Interface beim Konsumenten.
- [store.go](../internal/sqlitestore/store.go) – erfüllt beide Interfaces, ohne sie zu kennen.

## Übungen

Musterlösungen mit Begründung stehen im Branch [`loesungen`](https://github.com/casoon/go-tutorial/blob/loesungen/loesungen/09-projektstruktur.md) – versuch es zuerst selbst.

1. **Aufwärmen:** Lege außerhalb des Repos ein Modul `example.com/other` an, das per `replace`-Direktive auf deinen Checkout von upwatch zeigt, und versuche in seinem `main.go`, `internal/monitor` zu importieren. Vergleiche die Meldung mit der im Abschnitt über `internal/`.
2. **Kern:** Skizziere das oben beschriebene Alarm-Feature als Package `internal/alert`: Welche Interfaces würde es beim Konsumenten definieren, wer würde es aufrufen, und welche Imports bräuchte `main.go` zusätzlich? Schreib nur die Typen und Signaturen, keine Implementierung, und prüfe mit `go list` wie in „Jetzt du“, dass kein Zyklus entsteht.
3. **Kern:** Überlege, was sich ändern müsste, damit `httpapi` und `scheduler` dasselbe `Store`-Interface teilen. Wo müsste es liegen, und warum ist das in upwatch schlechter als zwei kleine Interfaces?
4. **Profi:** Nimm ein eigenes Projekt aus C# oder Java mit klassischer Schichtenarchitektur und markiere die Services, die nur durchreichen. Wie würde die Struktur in Go aussehen?

---

← [Teil 08: Eine HTTP-API mit net/http](08-http-api.md) · [Übersicht](../README.md) · [Teil 10: Datenbanken mit database/sql](10-datenbanken.md) →
