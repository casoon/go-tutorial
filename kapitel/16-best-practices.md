# Teil 16: Go Best Practices für Umsteiger

Nach diesem Kapitel erkennst du die Muster, die Entwickler aus C#, Java oder TypeScript typischerweise nach Go mitbringen, weißt, warum sie dort schlecht passen, und findest für jedes die idiomatische Form in upwatch wieder. Am Ende steht ein Rückblick auf das ganze Tutorial und Ideen, wie es weitergehen kann.

> **upwatch in diesem Teil:** das ganze Projekt, vor allem [scheduler.go](../internal/scheduler/scheduler.go), [httpapi.go](../internal/httpapi/httpapi.go), [monitor.go](../internal/monitor/monitor.go) und [main.go](../cmd/upwatch/main.go)
> **Voraussetzungen:** Teile 01–15, besonders Teil 04 (Go-Denkweise) und Teil 09 (Projektstruktur)

## Das eigentliche Problem: gute Gewohnheiten am falschen Ort

Die meisten Fehler in diesem Kapitel sind keine Anfängerfehler. Es sind Muster, die in einem anderen Ökosystem gut begründet sind: `IUserService`, weil der DI-Container von ASP.NET Core Interfaces auflöst; `AbstractUserFactory`, weil Java keine freien Funktionen kennt; `BaseRepository<T>`, weil Entity Framework oder Spring Data die Basisklasse ohnehin mitbringen. In Go fehlen die Gründe, die diese Muster nötig gemacht haben – und übrig bleibt die Indirektion.

Die Tabelle ist die Kurzfassung des Kapitels. Danach folgen ausführlich nur die Muster, deren Go-Form nicht offensichtlich ist; der Rest bekommt ein paar Zeilen und einen Verweis.

| Mitgebrachtes Muster | Herkunft | Go-Form | Stelle in upwatch |
|---|---|---|---|
| `IUserService` + `UserServiceImpl`, Basisklassen | C#, Java | konkreter Typ, Name aus Package + Typ | `check.Checker`, `sqlitestore.Store` |
| großes Interface beim Anbieter | C#, Java | kleines Interface beim Verbraucher | `Store` in [scheduler.go](../internal/scheduler/scheduler.go) |
| Getter und Setter | C#-Properties, Java Beans | exportierte Felder, Konstruktor prüft | `monitor.Target`, `NewTarget` |
| Exceptions, `try`/`catch` | alle drei | `(T, error)`, `%w`, `errors.Is`/`As` | `handleError` in [httpapi.go](../internal/httpapi/httpapi.go) |
| `CancellationToken` im Feld, Request-Scope im Objekt | C#, Java | `ctx` als erster Parameter | `Scheduler.Run(ctx)`, `Checker.Check(ctx, …)` |
| Fire-and-forget `Task.Run`, unbeobachtete Promise | C#, TypeScript | jede Goroutine hat ein Ende und einen Wartenden | `WaitGroup.Go` im Scheduler, `wg.Wait()` in `run` |
| Referenzen überall | Java, C#-Klassen | Werte, Pointer nur mit Grund | `Check` nimmt und liefert Werte |
| statische Initialisierer, Service Locator | Java, C# | Abhängigkeiten in `main` bauen und übergeben | `run` in [main.go](../cmd/upwatch/main.go), `Load(getenv)` |
| `utils`, `common`, `helpers` | alle drei | Funktion neben ihrem Verbraucher | `isPublic` in `check`, `writeJSON` in `httpapi` |
| generisches Repository, `Result<T>` | C#, Java, TypeScript | konkrete Typen, Generics für Datenstrukturen | kein eigener generischer Typ |
| Framework vor der ersten Route | ASP.NET, Spring, NestJS | Standardbibliothek zuerst | eine direkte Abhängigkeit in [go.mod](../go.mod) |

## Kurz: Klassen, Interfaces, Getter

**Klassenhierarchien und Namen aus dem Enterprise-Katalog.** Go hat keine Vererbung, und ein Interface neben seiner einzigen Implementierung ist nur ein zweiter Name für denselben Typ; `I`-Präfixe und `Impl`-Suffixe tragen keine Information. In upwatch heißen die Typen `check.Checker`, `scheduler.Scheduler`, `sqlitestore.Store`, erzeugt von einfachen Funktionen statt Factories; Gemeinsames steckt in Middleware statt in einer Basisklasse. Die Begründung steht in [Teil 04](04-go-denkweise.md).

**Interfaces auf Vorrat, beim Produzenten.** Ein `StoreInterface` mit zwanzig Methoden beim Anbieter koppelt jeden Verbraucher an alle Methoden. Weil Go-Interfaces implizit erfüllt werden, definiert der Verbraucher, was er braucht: Der Scheduler kennt ein `Store` mit zwei Methoden, die API ein eigenes mit sechs, und die Fakes in den Tests sind entsprechend kurz ([Teil 04](04-go-denkweise.md), [Teil 09](09-projektstruktur.md)).

**Getter und Setter für alles.** `func (t *Target) GetURL() string { return t.url }` ist in Go meist Zeremonie. Ein Getter setzt ein unexportiertes Feld voraus (`url`, klein geschrieben, außerhalb des Packages unsichtbar); `monitor.Target` exportiert seine Felder stattdessen (`URL`, groß geschrieben), und Invarianten sichert der Konstruktor `NewTarget`, nicht ein Setter. Wo es echte Logik gibt, gibt es eine Methode wie `Result.Up()`. Braucht man doch einen Getter, heißt er `URL()`, nicht `GetURL()`.

## Context in Structs

**Was man sieht:**

```go
type Scheduler struct {
	ctx   context.Context
	store Store
}
```

In C# ist das der `CancellationToken`, den man im Konstruktor entgegennimmt und in einem Feld ablegt; in Java der Request-Scope, der an einem Objekt hängt.

**Warum es nicht passt:** Ein Context gehört zu einem Aufruf: diese Anfrage, dieser Lauf, mit dieser Deadline. In einem Struct gespeichert, gilt er plötzlich für alle Methoden und die ganze Lebensdauer des Objekts, und niemand sieht beim Aufruf, welche Deadline oder welcher Abbruch gerade greift. Die Dokumentation von `context` sagt das ausdrücklich.

**Idiomatisch:** Context ist der erste Parameter jeder Funktion, die blockieren kann: `Scheduler.Run(ctx)`, `Checker.Check(ctx, target)`, alle Methoden von `sqlitestore.Store`. Wo eine Arbeit den Abbruch überleben soll, macht der Code das sichtbar: Der Writer im Scheduler speichert Ergebnisse während des Shutdowns mit `context.WithoutCancel(ctx)` ([Teil 07](07-context.md)).

## Goroutines ohne Ende

**Was man sieht:** `go doSomething()` an beliebigen Stellen, ohne dass klar ist, wann die Goroutine endet und wer darauf wartet.

**Warum es nicht passt:** Eine Goroutine ist billig zu starten, aber sie gehört niemandem. Läuft sie unbegrenzt weiter, hält sie Speicher und alles, worauf sie verweist; schreibt sie nach dem Shutdown noch in eine geschlossene Datenbank, gibt es Fehler, die nur manchmal auftreten. In C# hält ein `Task` wenigstens ein Objekt, auf das man warten kann, in TypeScript eine Promise. Ein `go`-Aufruf gibt nichts zurück – kein Handle, kein Ergebnis, keinen Fehler. Alles davon muss man selbst bauen.

**Idiomatisch:** Für jede Goroutine ist klar, wie sie endet und wer auf sie wartet. Im [Scheduler](../internal/scheduler/scheduler.go) startet `Run` die Worker mit `WaitGroup.Go`, schließt beim Shutdown erst `jobs`, wartet auf die Worker, schließt dann `results` und wartet auf den Writer. In [main.go](../cmd/upwatch/main.go) wartet `run` mit `wg.Wait()` auf den Scheduler, bevor es zurückkehrt. Die eine Goroutine ohne geordnetes Ende ist `serveDebug`: Der Debug-Listener läuft, bis der Prozess endet – eine Vereinfachung für ein Werkzeug, das nur lokal benutzt wird, und genau so eine Ausnahme sollte man begründen können.

## Pointer überall „aus Performance-Gründen“

**Was man sieht:** `func process(t *Target)`, `[]*Result`, `map[string]*Config`, auch für kleine Werte, die niemand ändert.

**Warum es nicht passt:** Die Intuition aus Java und C#, dass Objekte Referenzen sind und Kopieren teuer ist, stimmt in Go nicht pauschal. Ein Struct mit ein paar Feldern zu kopieren ist billig. Ein Pointer dagegen führt oft dazu, dass der Wert auf den Heap muss ([Teil 14](14-performance.md)), und er macht beim Lesen unklar, ob die Funktion den Wert ändert. Ein `nil`-Pointer ist außerdem ein Zustand, den man sonst nicht hätte.

**Idiomatisch:** Werte, solange es keinen Grund für einen Pointer gibt. `Checker.Check` nimmt `monitor.Target` als Wert und gibt `monitor.Result` als Wert zurück; die Store-Methoden geben `[]monitor.Target` zurück, keine `[]*monitor.Target`. Pointer gibt es dort, wo sie etwas bedeuten: `*Checker`, `*Scheduler` und `*sqlitestore.Store` haben Zustand (einen HTTP-Client, einen Connection Pool), der nicht kopiert werden soll, und `*monitor.ValidationError` ist ein Pointer, weil Fehlertypen üblicherweise als Pointer implementiert werden und `errors.As` damit arbeitet. Die Entscheidung ist semantisch – „soll das geteilt oder kopiert werden?“ –, nicht eine Performance-Vermutung.

## init()-Magie und globale Variablen

**Was man sieht:** `func init()`, das Konfiguration liest, eine Datenbankverbindung öffnet oder sich in einer globalen Registry anmeldet. Package-Variablen wie `var DB *sql.DB` oder `var Logger = …`, auf die jede Funktion zugreift. Aus Java kennt man das als `static`-Initialisierer, aus .NET als statischen Konstruktor.

**Warum es nicht passt:** `init` läuft beim Import, in einer Reihenfolge, die man nicht am Aufruf sieht, und kann keinen Fehler zurückgeben. Globale Variablen sind versteckte Abhängigkeiten: Eine Funktion, die `DB` benutzt, sieht von außen aus wie eine, die keine Datenbank braucht, und zwei Tests, die `DB` unterschiedlich setzen, können nicht parallel laufen. Das ist das Service-Locator-Problem aus .NET und Java, nur ohne Container.

**Idiomatisch:** upwatch hat kein einziges eigenes `init`. Abhängigkeiten werden in `run` in [main.go](../cmd/upwatch/main.go) erzeugt und als Parameter weitergegeben. `config.Load` bekommt sogar die Funktion zum Lesen der Umgebung übergeben (`Load(getenv func(string) string)`), damit Tests keine echten Umgebungsvariablen setzen müssen. Die Package-Variablen, die es gibt, haben einen Grund: Sentinel-Fehler wie `monitor.ErrNotFound` und `check.ErrForbiddenAddress` sind unveränderliche Werte, `migrations` ist ein `embed.FS`, `version` setzt der Linker. Die expvar-Zähler im Scheduler sind globale Variablen, weil expvar eine globale Registry ist – eine Grenze des Packages, die man hinnimmt, statt sie zu verstecken. `slog.SetDefault` in `main` setzt zwar einen globalen Logger, aber die Packages bekommen ihren Logger trotzdem explizit übergeben.

Die anerkannte Ausnahme vom `init`-Verbot sind Treiber für `database/sql`: `modernc.org/sqlite` registriert sich beim Import unter dem Namen `"sqlite"`. Das ist ein Muster der Standardbibliothek, nicht eines, das man für eigenen Code übernehmen sollte.

## Kurz: Exceptions, util-Packages, Generics, Frameworks

**Exceptions mit panic nachbauen.** `panic(err)` tief unten und ein `recover` weiter oben ist ein selbstgebautes `try`/`catch`, das an keiner Signatur zu sehen ist. upwatch gibt Fehler zurück, reichert sie mit `%w` an und übersetzt sie an genau einer Stelle, `handleError`, in Statuscodes ([Teil 05](05-fehlerbehandlung.md)). Das einzige `recover` steht in `recoverPanics` als Sicherheitsnetz für echte Programmierfehler in Handlern.

**util-Packages.** Der Package-Name ist Teil jedes Aufrufs, und `util.Validate(x)` sagt nichts. Solche Packages wachsen unbegrenzt und werden zum Knoten im Abhängigkeitsgraph. In upwatch lebt jede Hilfsfunktion neben ihrem Verbraucher: `isPublic` in `check`, `writeJSON` und `pathID` in `httpapi`, `scanTarget` in `sqlitestore` ([Teil 09](09-projektstruktur.md)).

**Generics zu früh.** Ein `Repository[T]` verdeckt, dass Targets und Results ganz unterschiedliche Abfragen brauchen, und ein `Result[T]` konkurriert mit `(T, error)`, das jede Go-Bibliothek nutzt. upwatch definiert keinen generischen Typ und vermisst keinen; wo Generics glänzen, liefert die Standardbibliothek sie fertig (`slices`, `maps`, `atomic.Pointer[T]`). Faustregel: Wenn du dieselbe Funktion zum dritten Mal für einen anderen Typ schreibst, denk über einen Typparameter nach.

**Frameworks zuerst.** In .NET und Spring ist das Framework die Plattform, in Node.js füllen Express oder NestJS eine Lücke der Laufzeit. In Go deckt die Standardbibliothek den Großteil dessen ab, was ein kleiner Server braucht: Routing mit Methoden und Pfadparametern (`http.ServeMux`, seit Go 1.22), JSON, `database/sql`, `log/slog`, `expvar`, `net/http/pprof`, `testing` und `httptest`. upwatch hat genau eine direkte Abhängigkeit, den SQLite-Treiber. Ein Treiber wie `pgx`, ein Code-Generator wie `sqlc` oder ein Router mit mehr Funktionen kann sich lohnen – wenn ein konkretes Problem ihn verlangt.

## Was du gebaut hast

In sechzehn Teilen ist ein vollständiger Dienst entstanden. So sieht er im Betrieb aus, die Anwendung aus Teil 01 mit der Produktionsschicht aus Teil 15:

```text
                        Internet (HTTPS)
                               │
                 ┌─────────────▼──────────────┐
                 │ Reverse Proxy: TLS, HSTS   │  Teil 15
                 └─────────────┬──────────────┘
                               │ HTTP 127.0.0.1:8080
┌──────────────────────────────▼───────────────────────────────────┐
│ upwatch: ein statisches Binary, nonroot/DynamicUser      Teil 15 │
│ main: Config, slog, Signale, Graceful Shutdown           Teil 12 │
│                                                                  │
│  ┌────────────────────────┐          ┌────────────────────────┐  │
│  │ HTTP-API       Teil 08 │          │ Scheduler      Teil 06 │  │
│  │ Timeouts, Limits    13 │          │ Worker, Channels,      │  │
│  │ /healthz, /readyz   12 │          │ Context        Teil 07 │  │
│  └───────────┬────────────┘          └─────┬────────────┬─────┘  │
│              │ Targets          ClaimDue,  │            │ Jobs   │
│              │ lesen/schreiben  SaveResult │            ▼        │
│              │   ┌─────────────────────┐   │    ┌──────────────┐ │
│              └──►│ SQLite      Teil 10 │◄──┘    │ Checker      │─┼──► geprüfte URLs
│                  │ Migrationen (embed) │        │ SSRF-Schutz  │ │
│                  └──────────┬──────────┘        │ Teil 13      │ │
│  Debug 127.0.0.1:6060       │                   └──────────────┘ │
│  expvar, pprof  Teil 12, 14 │                                    │
└─────────────────────────────┼────────────────────────────────────┘
                              ▼
               /var/lib/upwatch bzw. Volume /data
```

upwatch nimmt URLs über eine JSON-API an, validiert sie, speichert sie in SQLite und prüft sie mit einem Pool von Workern in festen Abständen. Es misst Statuscode und Antwortzeit und unterscheidet zwischen „Ziel ist down“ und „Check wurde abgebrochen“ – alles mit einer einzigen externen Abhängigkeit und ohne Framework.

### Produktionsreif heißt in upwatch

- Der HTTP-Server hat Timeouts für jede Phase und begrenzte Header ([Teil 13](13-security.md)).
- Request-Bodies sind begrenzt, unbekannte JSON-Felder werden abgelehnt ([Teil 08](08-http-api.md), [Teil 13](13-security.md)).
- Ausgehende Checks haben einen Timeout und können keine privaten Adressen erreichen ([Teil 13](13-security.md)).
- Eingaben werden in der Domäne validiert, und Fuzzing hat die Validierung geprüft ([Teil 11](11-testing.md)).
- SQL ist durchgehend parametrisiert, Migrationen stecken im Binary ([Teil 10](10-datenbanken.md)).
- Ein Signal führt zu einem geordneten Shutdown mit Frist; der Scheduler speichert fertige Ergebnisse noch ([Teil 12](12-logging-konfiguration-observability.md), [Teil 06](06-concurrency.md)).
- Liveness und Readiness sind getrennt ([Teil 12](12-logging-konfiguration-observability.md)); ein Readiness-Drain wäre der nächste Schritt für mehrere Instanzen ([Teil 15](15-deployment.md)).
- Logs sind strukturiert und gehen als JSON nach stdout ([Teil 12](12-logging-konfiguration-observability.md)).
- Fehlkonfiguration bricht den Start mit allen Fehlern auf einmal ab ([Teil 12](12-logging-konfiguration-observability.md)).
- Metriken und Profile liegen auf einem separaten Listener nur für localhost ([Teil 12](12-logging-konfiguration-observability.md), [Teil 13](13-security.md), [Teil 14](14-performance.md)).
- Der Prozess läuft ohne Root-Rechte: distroless und nonroot im Container, `DynamicUser` und Hardening unter systemd ([Teil 15](15-deployment.md)).
- Das Binary ist statisch, für vier Plattformen gebaut und trägt seine Version ([Teil 15](15-deployment.md)).
- Tests laufen mit Race Detector, zeitabhängiger Code mit `synctest`, und `govulncheck` prüft die Abhängigkeiten ([Teil 06](06-concurrency.md), [Teil 11](11-testing.md), [Teil 13](13-security.md)).

Was fehlt, steht ebenso ehrlich da: Authentifizierung (Teil 13), mehr als eine Instanz (Teil 15) und Retention für alte Ergebnisse (unten).

### Das eigentliche Lernziel

Das Ziel war aber nicht upwatch, sondern: So baut man mit Go eine kleine, schnelle und wartbare Serveranwendung – und versteht dabei, warum Go an einigen Stellen bewusst anders funktioniert als C# oder TypeScript. Fehler als Werte statt Exceptions, weil der Kontrollfluss sichtbar bleiben soll. Interfaces beim Verbraucher, weil sie implizit erfüllt werden. Context als Parameter, weil Abbruch und Deadlines zu einem Aufruf gehören. Goroutines und Channels statt `async`/`await`, weil blockierender Code in Go billig ist. Ein Binary statt Runtime plus Paketen, weil Deployment einfach sein soll. Keine dieser Entscheidungen ist kostenlos – Go-Code ist an manchen Stellen länger und expliziter als das Äquivalent in C# –, aber sie zielen alle auf dasselbe: Code, den jemand anders in einem Jahr ohne Vorwissen lesen und ändern kann.

## Wie es weitergehen kann

Die beste Übung ist, upwatch zu erweitern. Jede der folgenden Aufgaben berührt mehrere Packages und zwingt dich, die Entscheidungen aus diesem Tutorial selbst zu treffen.

**Benachrichtigungen bei Statuswechsel.** Wenn ein Ziel von „up“ auf „down“ wechselt (oder zurück), soll upwatch eine Nachricht verschicken, zum Beispiel per Webhook. Dafür muss der Scheduler den letzten Status kennen. Wo gehört diese Information hin – in die Datenbank oder in den Speicher? Wie verhinderst du, dass ein langsamer Webhook die Worker blockiert? Wie testest du das mit `synctest` und einem Fake? Und denk an [Teil 13](13-security.md): Eine Webhook-URL, die Nutzer eintragen, ist wieder SSRF.

**Retention.** Die Tabelle `results` wächst unbegrenzt. Schreib eine Aufräumfunktion, die Ergebnisse älter als eine konfigurierbare Dauer löscht (`UPWATCH_RETENTION`), und lass sie periodisch laufen. Lösche in Batches, damit eine große Löschung SQLite nicht lange sperrt.

**PostgreSQL mit pgx.** Schreib ein Package `pgstore` mit denselben Methoden wie `sqlitestore`, mit `github.com/jackc/pgx/v5` als Treiber. `ClaimDue` lässt sich mit `SELECT … FOR UPDATE SKIP LOCKED` so formulieren, dass mehrere Instanzen sich die Arbeit teilen. Weder Scheduler noch API sollten sich dafür ändern müssen – wenn doch, ist das ein Hinweis auf eine undichte Abstraktion.

**Prometheus-Endpunkt.** Gib die Zähler aus dem Scheduler im Prometheus-Textformat unter `/metrics` aus. Das Format ist einfach genug, um es ohne Bibliothek zu schreiben; probier beides und vergleiche. Ergänze ein Histogramm für die Check-Dauer.

**Web-Oberfläche mit html/template.** Eine Seite, die alle Ziele mit ihrem letzten Status zeigt, und ein Formular zum Anlegen. Eingebettete Templates mit `embed`, automatisches Escaping durch `html/template`, CSRF-Schutz mit `http.CrossOriginProtection`. Danach brauchst du auch die Authentifizierung aus den Übungen in Teil 13.

### Quellen

Die offiziellen Quellen sind gut und reichen weit:

- [Effective Go](https://go.dev/doc/effective_go) – der klassische Text zu idiomatischem Go. Teile sind älter als Modules und Generics, die Grundgedanken gelten weiter.
- [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments) – eine Sammlung häufiger Review-Anmerkungen, von Benennung bis Context. Liest sich wie eine längere Fassung dieses Kapitels.
- [The Go Blog](https://go.dev/blog/) – Hintergrundartikel des Go-Teams, unter anderem zu Fehlerbehandlung, Context, Generics und zu jeder neuen Version.
- [Dokumentation der Standardbibliothek](https://pkg.go.dev/std) – die Package-Dokumentation ist oft das beste Lehrmaterial, mit lauffähigen Beispielen. `go doc` zeigt sie auch offline.
- [The Go Programming Language Specification](https://go.dev/ref/spec) – kurz genug, um sie einmal ganz zu lesen.
- [Organizing a Go module](https://go.dev/doc/modules/layout) – die offizielle Empfehlung zur Projektstruktur.
- [Go Vulnerability Database](https://pkg.go.dev/vuln/) – die Datenbank hinter `govulncheck`.

## Im Projekt

- [internal/scheduler/scheduler.go](../internal/scheduler/scheduler.go): Interfaces beim Verbraucher, Goroutines mit klarem Ende, Context als Parameter.
- [internal/httpapi/httpapi.go](../internal/httpapi/httpapi.go): eigenes kleines `Store`-Interface, `handleError` als einzige Übersetzung von Fehlern in Statuscodes.
- [internal/monitor/monitor.go](../internal/monitor/monitor.go): exportierte Felder statt Getter, `NewTarget` statt Setter, Sentinel-Fehler und Fehlertyp.
- [internal/config/config.go](../internal/config/config.go): `Load(getenv)` statt globalem Zustand.
- [cmd/upwatch/main.go](../cmd/upwatch/main.go): Composition Root ohne Container, `run()` mit Fehlerrückgabe.
- [go.mod](../go.mod): eine direkte Abhängigkeit.

## Übungen

1. **Aufwärmen:** Such dir ein Go-Projekt, das du selbst geschrieben hast oder bei der Arbeit pflegst, und geh es mit der Tabelle am Anfang dieses Kapitels durch. Notiere für jeden Fund, welches Muster aus welcher Sprache dahintersteckt.
2. **Kern:** Schreib `httpapi` testweise auf ein großes `StoreInterface` beim Anbieter um, das alle Methoden von `sqlitestore.Store` enthält. Was ändert sich an den Tests in `httpapi_test.go`, insbesondere an `failingStore`? Mach die Änderung danach rückgängig.
3. **Profi (empfohlen):** Wähl eine der Erweiterungen aus „Wie es weitergehen kann“ und setz sie um, inklusive Tests. Achte darauf, ob du ein neues Interface, ein neues Package oder eine neue Abhängigkeit brauchst – und begründe jede davon.

---

← [Teil 15: Deployment](15-deployment.md) · [Übersicht](../README.md)
