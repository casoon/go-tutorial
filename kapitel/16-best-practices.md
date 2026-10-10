# Teil 16: Go Best Practices für Umsteiger

Nach diesem Kapitel erkennst du die Muster, die Entwickler aus C#, Java oder TypeScript typischerweise nach Go mitbringen, weißt, warum sie dort schlecht passen, und findest für jedes die idiomatische Form in upwatch wieder. Am Ende steht ein Rückblick auf das ganze Tutorial und Ideen, wie es weitergehen kann.

## Das eigentliche Problem: gute Gewohnheiten am falschen Ort

Die meisten Fehler in diesem Kapitel sind keine Anfängerfehler. Es sind Muster, die in einem anderen Ökosystem gut begründet sind: `IUserService`, weil der DI-Container von ASP.NET Core Interfaces auflöst; `AbstractUserFactory`, weil Java keine freien Funktionen kennt; `BaseRepository<T>`, weil Entity Framework oder Spring Data die Basisklasse ohnehin mitbringen; `UserServiceImpl`, weil es das Interface schon gibt und die Implementierung irgendwie heißen muss. In Go fehlen die Gründe, die diese Muster nötig gemacht haben – und übrig bleibt die Indirektion.

Die Go-Antwort ist in Kurzform: kleine Packages, kleine Interfaces, konkrete Structs, explizite Abhängigkeiten, explizite Fehler, wenig Abstraktion, Standardbibliothek zuerst. [Teil 04](04-go-denkweise.md) und [Teil 09](09-projektstruktur.md) haben die Begründungen ausführlich. Dieses Kapitel ist die Prüfliste dazu: jeweils was man sieht, warum es in Go schlecht passt und wie es in upwatch aussieht.

## Klassenhierarchien und Namen aus dem Enterprise-Katalog

**Was man sieht:** `type IUserService interface`, `type UserServiceImpl struct`, `NewUserServiceFactory()`, ein Package `services` mit einem Interface und genau einer Implementierung, `BaseHandler` als eingebettetes Struct, das allen Handlern gemeinsame Methoden „vererbt“.

**Warum es nicht passt:** Go hat keine Vererbung, und Interfaces werden implizit erfüllt. Ein Interface, das neben seiner einzigen Implementierung definiert ist, hat keinen Abnehmer, der von ihm abhängt – es ist nur ein zweiter Name für denselben Typ. Präfixe wie `I` und Suffixe wie `Impl` transportieren in Go keine Information. Eingebettete „Basisklassen“ führen zu Methoden, von denen man beim Lesen nicht weiß, wo sie herkommen.

**Idiomatisch:** Der Typ heißt nach dem, was er ist, und der Package-Name liefert den Kontext. In upwatch gibt es `check.Checker`, `scheduler.Scheduler` und `sqlitestore.Store`, erzeugt mit `check.New`, `scheduler.New` und `sqlitestore.Open`. Keine Factory, weil eine Funktion reicht. Gemeinsames Verhalten der Handler steckt nicht in einer Basisklasse, sondern in Middleware-Funktionen in [middleware.go](../internal/httpapi/middleware.go) und in kleinen Hilfsfunktionen wie `writeJSON` und `pathID` in [httpapi.go](../internal/httpapi/httpapi.go).

## Interfaces auf Vorrat, beim Produzenten

**Was man sieht:** Das Package `sqlitestore` exportiert ein `StoreInterface` mit zwanzig Methoden, und jeder Verbraucher hängt davon ab.

**Warum es nicht passt:** Ein großes Interface beim Anbieter koppelt jeden Verbraucher an alle Methoden, auch an die, die er nie aufruft. Für einen Test muss man dann zwanzig Methoden faken. In Java und C# ist das trotzdem üblich, weil eine Klasse ein Interface ausdrücklich implementieren muss und es deshalb beim Anbieter stehen muss. In Go muss es das nicht.

**Idiomatisch:** Der Verbraucher definiert das Interface mit genau den Methoden, die er braucht. [scheduler.go](../internal/scheduler/scheduler.go) definiert `Store` mit zwei Methoden (`ClaimDue`, `SaveResult`) und `Checker` mit einer. [httpapi.go](../internal/httpapi/httpapi.go) definiert ein eigenes `Store` mit den sechs Methoden, die die API braucht. `sqlitestore.Store` erfüllt beide, ohne eines davon zu kennen. Die Fakes in [scheduler_test.go](../internal/scheduler/scheduler_test.go) sind dadurch ein paar Zeilen lang.

## Getter und Setter für alles

**Was man sieht:**

```go
func (t *Target) GetURL() string      { return t.url }
func (t *Target) SetURL(url string)   { t.url = url }
func (t *Target) GetInterval() time.Duration { return t.interval }
```

**Warum es nicht passt:** Properties in C# und Getter in Java sind dort auch deshalb Konvention, weil man später Logik einbauen kann, ohne die Schnittstelle zu brechen. In Go sind die meisten Typen Daten, die man nicht kapseln muss, und die Kapselung findet ohnehin am Package statt, nicht an der Klasse. Wenn ein Getter nötig ist, heißt er in Go `URL()`, nicht `GetURL()`.

**Idiomatisch:** `monitor.Target` und `monitor.Result` in [monitor.go](../internal/monitor/monitor.go) haben exportierte Felder. Wo es echte Logik gibt, gibt es eine Methode: `Result.Up()` entscheidet, ob ein Check als erfolgreich zählt. Invarianten sichert der Konstruktor `NewTarget`, der die Eingabe validiert, nicht ein Setter.

## Exceptions mit panic nachbauen

**Was man sieht:** `panic(err)` tief in einer Funktion und ein `recover` weiter oben, das daraus wieder einen Fehler macht – ein selbstgebautes `try`/`catch`. Oder Hilfsfunktionen wie `must(err)`, die überall aufgerufen werden.

**Warum es nicht passt:** In Go ist ein Fehler ein Rückgabewert, und an der Signatur `(T, error)` sieht jeder Leser, dass etwas schiefgehen kann. Ein Panic springt über alle Aufrufer hinweg, an denen niemand mit ihm rechnet, und Ressourcen, die nicht per `defer` freigegeben werden, bleiben liegen. Panic ist für Programmierfehler gedacht, für Zustände, die nicht eintreten dürfen.

**Idiomatisch:** upwatch gibt Fehler zurück und reichert sie mit `fmt.Errorf("…: %w", err)` an ([Teil 05](05-fehlerbehandlung.md)). Fehlerarten, auf die jemand reagieren muss, sind Werte (`monitor.ErrNotFound`, `monitor.ErrDuplicate`) oder Typen (`*monitor.ValidationError`), und `handleError` in [httpapi.go](../internal/httpapi/httpapi.go) übersetzt sie an genau einer Stelle in HTTP-Statuscodes. Selbst `main` bricht nicht mittendrin ab: `run()` gibt einen Fehler zurück, und erst `main` ruft `os.Exit(1)` auf, damit alle `defer` laufen. Das einzige `recover` steht in `recoverPanics` als Sicherheitsnetz für echte Programmierfehler in Handlern – nicht als Kontrollfluss.

## Context in Structs

**Was man sieht:**

```go
type Scheduler struct {
	ctx   context.Context
	store Store
}
```

**Warum es nicht passt:** Ein Context gehört zu einem Aufruf: diese Anfrage, dieser Lauf, mit dieser Deadline. In einem Struct gespeichert, gilt er plötzlich für alle Methoden und jede Lebensdauer des Objekts, und niemand sieht beim Aufruf, welche Deadline oder welcher Abbruch gerade greift. Das Package `context` sagt das ausdrücklich in seiner Dokumentation.

**Idiomatisch:** Context ist der erste Parameter jeder Funktion, die blockieren kann. `Scheduler.Run(ctx)`, `Checker.Check(ctx, target)`, alle Methoden von `sqlitestore.Store`. Wo eine Arbeit den Abbruch überleben soll, macht der Code das sichtbar: Der Writer im Scheduler speichert Ergebnisse während des Shutdowns mit `context.WithoutCancel(ctx)` ([Teil 07](07-context.md)).

## Goroutines ohne Ende

**Was man sieht:** `go doSomething()` an beliebigen Stellen, ohne dass klar ist, wann die Goroutine endet und wer darauf wartet.

**Warum es nicht passt:** Eine Goroutine ist billig zu starten, aber sie gehört niemandem. Läuft sie unbegrenzt weiter, hält sie Speicher und alles, worauf sie verweist; schreibt sie nach dem Shutdown noch in eine geschlossene Datenbank, gibt es Fehler, die nur manchmal auftreten. In C# hält der Task wenigstens ein Objekt, auf das man warten kann, in TypeScript eine Promise. In Go gibt es nichts dergleichen, wenn man es nicht selbst baut.

**Idiomatisch:** Für jede Goroutine ist klar, wie sie endet und wer auf sie wartet. Im [Scheduler](../internal/scheduler/scheduler.go) startet `Run` die Worker mit `WaitGroup.Go`, schließt beim Shutdown erst `jobs`, wartet auf die Worker, schließt dann `results` und wartet auf den Writer. In [main.go](../cmd/upwatch/main.go) wartet `run` mit `wg.Wait()` auf den Scheduler, bevor es zurückkehrt. Die eine Goroutine ohne geordnetes Ende ist `serveDebug`: Der Debug-Listener läuft, bis der Prozess endet. Das ist eine bewusste Vereinfachung für ein Werkzeug, das nur lokal benutzt wird – und genau so eine Ausnahme sollte man begründen können.

## Pointer überall „aus Performance-Gründen“

**Was man sieht:** `func process(t *Target)`, `[]*Result`, `map[string]*Config`, auch für kleine Werte, die niemand ändert.

**Warum es nicht passt:** Die Intuition aus Java und C#, dass Objekte Referenzen sind und Kopieren teuer ist, stimmt in Go nicht pauschal. Ein Struct mit ein paar Feldern zu kopieren ist billig. Ein Pointer dagegen führt oft dazu, dass der Wert auf den Heap muss ([Teil 14](14-performance.md)), und er macht beim Lesen unklar, ob die Funktion den Wert ändert. Ein `nil`-Pointer ist außerdem ein Zustand, den man sonst nicht hätte.

**Idiomatisch:** Werte, solange es keinen Grund für einen Pointer gibt. `Checker.Check` nimmt `monitor.Target` als Wert und gibt `monitor.Result` als Wert zurück; die Store-Methoden geben `[]monitor.Target` zurück, keine `[]*monitor.Target`. Pointer gibt es dort, wo sie etwas bedeuten: `*Checker`, `*Scheduler` und `*sqlitestore.Store` haben Zustand (einen HTTP-Client, einen Connection Pool), der nicht kopiert werden soll, und `*monitor.ValidationError` ist ein Pointer, weil Fehler als Pointer-Typen üblich sind und `errors.As` damit arbeitet. Die Entscheidung ist semantisch – „soll das geteilt oder kopiert werden?“ –, nicht eine Performance-Vermutung.

## init()-Magie und globale Variablen

**Was man sieht:** `func init()`, das Konfiguration liest, eine Datenbankverbindung öffnet oder sich in einer globalen Registry anmeldet. Package-Variablen wie `var DB *sql.DB` oder `var Logger = …`, auf die jede Funktion zugreift.

**Warum es nicht passt:** `init` läuft beim Import, in einer Reihenfolge, die man nicht am Aufruf sieht, und kann keinen Fehler zurückgeben. Globale Variablen sind versteckte Abhängigkeiten: Eine Funktion, die `DB` benutzt, sieht von außen aus wie eine, die keine Datenbank braucht, und zwei Tests, die `DB` unterschiedlich setzen, können nicht parallel laufen. Das ist das Service-Locator-Problem, das man auch aus .NET und Java kennt, nur ohne Container.

**Idiomatisch:** upwatch hat kein einziges eigenes `init`. Abhängigkeiten werden in `run` in [main.go](../cmd/upwatch/main.go) erzeugt und als Parameter weitergegeben. `config.Load` bekommt sogar die Funktion zum Lesen der Umgebung übergeben (`Load(getenv func(string) string)`), damit Tests keine echten Umgebungsvariablen setzen müssen. Die Package-Variablen, die es gibt, haben einen Grund: Sentinel-Fehler wie `monitor.ErrNotFound` und `check.ErrForbiddenAddress` sind unveränderliche Werte, `migrations` ist ein `embed.FS`, `version` setzt der Linker. Die expvar-Zähler im Scheduler sind globale Variablen, weil expvar eine globale Registry ist – eine Grenze des Packages, die man hinnimmt, statt sie zu verstecken. `slog.SetDefault` in `main` setzt zwar einen globalen Logger, aber die Packages bekommen ihren Logger trotzdem explizit übergeben.

Die anerkannte Ausnahme vom `init`-Verbot sind Treiber für `database/sql`: `modernc.org/sqlite` registriert sich beim Import unter dem Namen `"sqlite"`. Das ist ein Muster der Standardbibliothek, nicht eines, das man für eigenen Code übernehmen sollte.

## util-Packages

**Was man sieht:** `pkg/util`, `common`, `helpers`, `shared` – ein Package mit `StringUtils`, `HTTPHelpers` und `DateFormat`, das jedes andere Package importiert.

**Warum es nicht passt:** Der Package-Name ist in Go Teil jedes Aufrufs. `util.Validate(x)` sagt nichts darüber, was validiert wird. Solche Packages wachsen unbegrenzt, weil alles hineinpasst, und weil jeder sie importiert, werden sie zum Knoten im Abhängigkeitsgraph, der Importzyklen erzeugt.

**Idiomatisch:** Eine Hilfsfunktion lebt neben dem Code, der sie braucht. `isPublic` steht in `check`, weil nur der Checker sie braucht. `writeJSON`, `writeError` und `pathID` stehen in `httpapi`. `scanTarget` steht in `sqlitestore`. Wenn eine Funktion wirklich von mehreren Packages gebraucht wird, bekommt sie ein Package mit einem Namen, der sagt, was es tut. Und ein bisschen Kopieren ist oft besser als eine neue Abhängigkeit.

## Generics zu früh

**Was man sieht:** `type Repository[T any] interface { Get(id int64) (T, error); … }`, `Result[T]`- und `Option[T]`-Typen aus der funktionalen Welt, generische Hilfsfunktionen für jeden Slice-Durchlauf.

**Warum es nicht passt:** Generics sind seit Go 1.18 da und für Datenstrukturen und Algorithmen sehr nützlich. Für Domänencode sind sie meist eine Abstraktion ohne zweiten Anwendungsfall. Ein generisches Repository verdeckt, dass Targets und Results ganz unterschiedliche Abfragen brauchen. Ein `Result[T]` konkurriert mit dem `(T, error)`-Muster, das jede Go-Bibliothek nutzt.

**Idiomatisch:** upwatch definiert keinen einzigen generischen Typ und keine generische Funktion, und es fehlt nichts. Wo Generics in Go glänzen, liefert die Standardbibliothek sie fertig: `slices` und `maps` für die üblichen Operationen auf Slices und Maps, `atomic.Pointer[T]` und ähnliche Typen für nebenläufigen Code. Die Faustregel: Schreib die konkrete Version, und wenn du dieselbe Funktion zum dritten Mal für einen anderen Typ schreibst, denk über einen Typparameter nach.

## Frameworks zuerst

**Was man sieht:** Ein neues Projekt beginnt mit der Auswahl von Web-Framework, ORM, DI-Container, Konfigurations-Bibliothek und Logging-Framework, bevor die erste Route existiert.

**Warum es nicht passt:** In .NET und Spring ist das Framework die Plattform. In Node.js liefert die Laufzeit wenig, und Express oder NestJS füllen die Lücke. In Go deckt die Standardbibliothek den Großteil dessen ab, was ein kleiner Server braucht, und jedes Framework bringt eigene Konzepte mit, die neben denen der Standardbibliothek stehen (eigene Context-Typen, eigene Handler-Signaturen, eigene Middleware).

**Idiomatisch:** upwatch hat genau eine direkte Abhängigkeit in [go.mod](../go.mod): den SQLite-Treiber. Routing mit Methoden und Pfadparametern macht `http.ServeMux` (seit Go 1.22), JSON `encoding/json`, Datenbankzugriff `database/sql`, Logging `log/slog`, Metriken `expvar`, Profiling `net/http/pprof`, Tests `testing` und `httptest`. Das heißt nicht, dass Bibliotheken schlecht sind. Ein Treiber für PostgreSQL (`pgx`), ein Code-Generator wie `sqlc` oder ein Router mit mehr Funktionen kann sich lohnen – wenn ein konkretes Problem ihn verlangt, nicht weil man es aus einem anderen Ökosystem gewohnt ist.

## Was du gebaut hast

In sechzehn Teilen ist ein vollständiger Dienst entstanden. upwatch nimmt URLs über eine JSON-API an, validiert sie, speichert sie in SQLite und prüft sie mit einem Pool von Workern in festen Abständen. Es misst Statuscode und Antwortzeit, unterscheidet zwischen „Ziel ist down“ und „Check wurde abgebrochen“, schützt sich gegen SSRF und gegen langsame oder übergroße Requests, loggt strukturiert, stellt Metriken und Profile auf einem separaten Port bereit, beendet sich auf Signal geordnet und kommt als ein statisches Binary von etwa 12 MB, das als systemd-Dienst oder in einem Container ohne Shell läuft. Getestet ist das mit Tabellentests, Fuzzing, `httptest` gegen eine echte Datenbank und `testing/synctest` für die zeitabhängigen Teile.

Das alles mit einer einzigen externen Abhängigkeit und ohne Framework.

Das eigentliche Lernziel war aber nicht upwatch. Es war: So baut man mit Go eine kleine, schnelle und wartbare Serveranwendung – und versteht dabei, warum Go an einigen Stellen bewusst anders funktioniert als C# oder TypeScript. Fehler als Werte statt Exceptions, weil der Kontrollfluss sichtbar bleiben soll. Interfaces beim Verbraucher, weil sie implizit erfüllt werden. Context als Parameter, weil Abbruch und Deadlines zu einem Aufruf gehören. Goroutines und Channels statt `async`/`await`, weil blockierender Code in Go billig ist. Ein Binary statt Runtime plus Paketen, weil Deployment einfach sein soll. Keine dieser Entscheidungen ist kostenlos – Go-Code ist an manchen Stellen länger und expliziter als das Äquivalent in C# –, aber sie zielen alle auf dasselbe: Code, den jemand anders in einem Jahr ohne Vorwissen lesen und ändern kann.

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

1. Such dir ein Go-Projekt, das du selbst geschrieben hast oder bei der Arbeit pflegst, und geh es mit den Abschnitten dieses Kapitels durch. Notiere für jeden Fund, welches Muster aus welcher Sprache dahintersteckt.
2. Schreib `httpapi` testweise auf ein großes `StoreInterface` beim Anbieter um, das alle Methoden von `sqlitestore.Store` enthält. Was ändert sich an den Tests in `httpapi_test.go`, insbesondere an `failingStore`? Mach die Änderung danach rückgängig.
3. Gib `serveDebug` in [main.go](../cmd/upwatch/main.go) ein geordnetes Ende: Der Debug-Server soll beim Shutdown ebenfalls mit `Shutdown` beendet werden, und `run` soll auf ihn warten. Lohnt sich der zusätzliche Code?
4. Wähl eine der Erweiterungen aus „Wie es weitergehen kann“ und setz sie um, inklusive Tests. Achte darauf, ob du ein neues Interface, ein neues Package oder eine neue Abhängigkeit brauchst – und begründe jede davon.

---

← [Teil 15: Deployment](15-deployment.md) · [Übersicht](../README.md)
