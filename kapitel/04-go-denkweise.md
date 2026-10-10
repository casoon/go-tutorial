# Teil 04: Go-Denkweise statt OOP-Denkweise

Nach diesem Kapitel kannst du Go-Code so strukturieren, wie Go-Entwickler es erwarten: mit Komposition statt Vererbung, kleinen Interfaces beim Konsumenten, Funktionen als Bausteinen, Dependency Injection ohne Framework und Typen, deren Zero Value funktioniert.

Dieses Kapitel ist wichtiger als die Syntax. Die meisten Probleme, die Umsteiger mit Go haben, entstehen nicht an Sprachfeatures, sondern daran, dass sie die Struktur einer C#- oder Java-Anwendung eins zu eins übertragen.

## Nicht C# in Go nachbauen

Wer aus ASP.NET Core oder Spring kommt, hat ein bestimmtes Bild einer gut strukturierten Anwendung im Kopf: ein Interface für jede Klasse (`IUserService` neben `UserService`), generische Repositories (`IRepository<T>`), Verzeichnisse namens `Models`, `Services`, `Controllers`, `Helpers`, eine Basisklasse für gemeinsames Verhalten, Getter und Setter für jedes Feld und einen DI-Container, der alles per Reflection zusammensteckt.

In Go lässt sich all das nachbauen. Es fühlt sich dann aber an, als kämpfe man gegen die Sprache, und der Code wird für andere Go-Entwickler schwer lesbar. Der Grund ist nicht Geschmack, sondern dass Go andere Werkzeuge hat:

- Interfaces werden implizit erfüllt. Deshalb muss der Anbieter eines Typs kein Interface vorab definieren.
- Pakete sind die Einheit der Kapselung, nicht Klassen. Deshalb strukturiert man nach Verantwortung, nicht nach technischer Schicht.
- Funktionen sind vollwertige Werte. Deshalb ist für viele Aufgaben, für die man in C# ein Interface mit einer Methode anlegt, eine Funktion genug.
- `main` ist ein normales Programm. Deshalb ist es der natürliche Ort, Abhängigkeiten zu verdrahten.

Die folgenden Abschnitte gehen diese Punkte einzeln durch, jeweils am upwatch-Code.

## Komposition statt Vererbung

Go hat keine Vererbung. Es gibt kein `extends`, keine Basisklasse, kein `virtual` und kein `override`. Gemeinsames Verhalten entsteht durch Komposition: Ein Typ enthält einen anderen als Feld.

Für den häufigen Fall, dass der äußere Typ die Methoden des inneren direkt anbieten soll, gibt es **Embedding** – ein Feld ohne Namen:

```go
type HTTPCheck struct {
	URL string
}

func (c HTTPCheck) Name() string     { return "http" }
func (c HTTPCheck) Describe() string { return c.Name() + " check of " + c.URL }

type TLSCheck struct {
	HTTPCheck // embedded
	MinVersion string
}

func (c TLSCheck) Name() string { return "tls" }
```

Felder und Methoden von `HTTPCheck` werden zu `TLSCheck` „befördert“ (promoted): `tc.URL` und `tc.Describe()` funktionieren, als wären sie direkt in `TLSCheck` deklariert. Das sieht nach Vererbung aus. Der entscheidende Unterschied zeigt sich, wenn man `Describe` aufruft:

```bash
go run ./beispiele/04-go-denkweise/embedding
```

```
tls
http check of https://example.com
```

In C# oder Java würde `Describe` die überschriebene Methode `Name` des abgeleiteten Typs aufrufen und „tls check of …“ liefern. In Go hat `Describe` einen Receiver vom Typ `HTTPCheck`. Es weiß nicht, dass es in einen `TLSCheck` eingebettet ist, und ruft deshalb `HTTPCheck.Name` auf. `tc.Describe()` ist nur eine Abkürzung für `tc.HTTPCheck.Describe()`. Es gibt keine virtuelle Dispatch-Tabelle, kein `this`, das auf das äußere Objekt zeigt, und kein Template-Method-Pattern über Embedding. Der Test [TestEmbeddingIsNotInheritance](../beispiele/04-go-denkweise/embedding/main_test.go) hält das fest.

Ebenso wenig ist ein `TLSCheck` ein `HTTPCheck`: Eine Funktion, die `HTTPCheck` erwartet, nimmt keinen `TLSCheck` an. Austauschbarkeit gibt es in Go nur über Interfaces.

Wofür ist Embedding dann gut? Vor allem zum **Umhüllen**: einen Wert nehmen, ein, zwei Methoden ändern und den Rest unverändert durchreichen. Das prominenteste Beispiel in upwatch steht in [middleware.go](../internal/httpapi/middleware.go):

```go
// statusRecorder remembers the status code written by the wrapped handler.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}
```

`http.ResponseWriter` ist ein Interface mit drei Methoden. `statusRecorder` bettet es ein, ersetzt nur `WriteHeader` und erfüllt damit selbst `http.ResponseWriter`. Die Logging-Middleware gibt ihn an den eigentlichen Handler weiter und liest danach `rec.status` aus. Ohne Embedding müsste man `Header()` und `Write()` von Hand weiterleiten.

Dasselbe Muster zeigt [embedding/main.go](../beispiele/04-go-denkweise/embedding/main.go) mit einem Store, der Speichervorgänge zählt:

```go
type countingStore struct {
	Store
	saves int
}

func (c *countingStore) Save(url string, status int) error {
	c.saves++
	return c.Store.Save(url, status) // explicit call of the wrapped method
}
```

Der Aufruf der eingebetteten Methode ist ausdrücklich `c.Store.Save`. Es gibt kein `base.Save()`; man nennt das Feld beim Typnamen.

## Kleine Interfaces

Die Interfaces der Standardbibliothek sind fast alle winzig:

```go
type Reader interface { Read(p []byte) (n int, err error) }
type Writer interface { Write(p []byte) (n int, err error) }
type Stringer interface { String() string }
type Handler interface { ServeHTTP(ResponseWriter, *Request) }
```

Eine Methode, manchmal zwei oder drei. Rob Pike hat das als Go-Proverb formuliert: „The bigger the interface, the weaker the abstraction.“ Je weniger ein Interface verlangt, desto mehr Typen erfüllen es und desto leichter ist es, im Test etwas anderes einzusetzen. `io.Writer` wird von Dateien, Netzwerkverbindungen, HTTP-Antworten, Puffern, Hash-Funktionen und Kompressoren erfüllt – deshalb kann `fmt.Fprintf` in all das schreiben.

Größere Interfaces entstehen durch Kombination: `io.ReadWriter` bettet `Reader` und `Writer` ein. Auch das ist Embedding, diesmal bei Interfaces.

## Interfaces werden implizit erfüllt

In C# schreibt die Klasse `class SqliteStore : IStore`. Sie muss das Interface kennen, also muss das Interface vorher existieren, und meistens liegt es in derselben Assembly oder einer gemeinsamen Contracts-Assembly. In TypeScript ist die Typprüfung strukturell, aber die Interfaces werden trotzdem meist beim Anbieter definiert und exportiert.

In Go gibt es kein `implements`. Ein Typ erfüllt ein Interface, wenn er die Methoden hat. Der Compiler prüft das an der Stelle, an der ein Wert einem Interface zugewiesen wird – nicht bei der Deklaration des Typs. Das klingt nach einem Detail, verändert aber, wo Interfaces definiert werden.

## Interfaces beim Konsumenten definieren

Das ist der Kern dieses Kapitels, und upwatch zeigt ihn deutlich. Das Paket [sqlitestore](../internal/sqlitestore/store.go) exportiert einen konkreten Typ:

```go
// Store is safe for concurrent use; *sql.DB manages a connection pool.
type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error)
```

mit Methoden `CreateTarget`, `Target`, `Targets`, `DeleteTarget`, `ClaimDue`, `SaveResult`, `Results`, `Ping` und `Close`. Ein Interface definiert sqlitestore nicht.

Der Scheduler braucht davon genau zwei Methoden und sagt das in [scheduler.go](../internal/scheduler/scheduler.go):

```go
// Store is what the scheduler needs from persistence. It is defined here,
// at the consumer, and only lists the two methods actually used.
type Store interface {
	ClaimDue(ctx context.Context, now time.Time, limit int) ([]monitor.Target, error)
	SaveResult(ctx context.Context, r monitor.Result) error
}
```

Die HTTP-API braucht andere Methoden und definiert ihr eigenes Interface in [httpapi.go](../internal/httpapi/httpapi.go):

```go
// Store is the subset of persistence the API needs.
type Store interface {
	CreateTarget(ctx context.Context, t monitor.Target) (monitor.Target, error)
	Target(ctx context.Context, id int64) (monitor.Target, error)
	Targets(ctx context.Context) ([]monitor.Target, error)
	DeleteTarget(ctx context.Context, id int64) error
	Results(ctx context.Context, targetID int64, limit int) ([]monitor.Result, error)
	Ping(ctx context.Context) error
}
```

`*sqlitestore.Store` erfüllt beide, ohne eines davon zu kennen. sqlitestore importiert weder `scheduler` noch `httpapi` – es importiert nur `monitor`. In [main.go](../cmd/upwatch/main.go) wird derselbe Wert an beide übergeben, und dort prüft der Compiler, ob es passt:

```go
store, err := sqlitestore.Open(ctx, cfg.DBPath)
…
sched := scheduler.New(store, checker, logger, cfg.Workers, cfg.Tick)
…
Handler: httpapi.New(store, logger),
```

Was bringt das?

**Jeder Konsument dokumentiert seine Abhängigkeit genau.** Wer `scheduler.Store` liest, weiß, dass der Scheduler keine Ziele anlegt oder löscht. Ein `IStore` mit neun Methoden würde das verschleiern.

**Tests werden klein.** Der `fakeStore` in [scheduler_test.go](../internal/scheduler/scheduler_test.go) implementiert zwei Methoden, nicht neun. Es braucht keine Mocking-Bibliothek und keine leeren Methoden, die `NotImplementedException` werfen.

**Abhängigkeiten zeigen in die richtige Richtung.** Der Anbieter muss nichts über seine Konsumenten wissen. Kommt ein dritter Konsument dazu – etwa ein Export-Job –, definiert er sein Interface selbst, und sqlitestore bleibt unverändert.

**Keine Interfaces auf Vorrat.** Ein Interface entsteht erst, wenn es einen Konsumenten gibt, der Austauschbarkeit braucht. Gibt es nur eine Implementierung und keinen Test, der eine andere einsetzt, gibt es kein Interface.

Willst du trotzdem schon beim Anbieter sicherstellen, dass ein Typ ein Interface erfüllt, gibt es eine Zeile, die nichts kostet:

```go
var _ runnerStore = (*MemStore)(nil)
```

Sie steht so im Beispiel [interfaces/main.go](../beispiele/04-go-denkweise/interfaces/main.go), in dem ein `MemStore` zwei Konsumenten bedient – einen `Runner`, der Ziele prüft und Ergebnisse speichert, und eine Funktion `WriteReport`, die nur liest:

```go
type runnerStore interface {
	Targets() []Target
	SaveResult(targetID, status int)
}

type reportStore interface {
	Targets() []Target
	Results(targetID int) []int
}
```

In upwatch ist das nicht nötig, weil `main.go` die Prüfung ohnehin erzwingt.

### Wann das Interface doch beim Anbieter steht

Die Regel ist eine Faustregel, kein Dogma. Wenn ein Paket ein Interface als Teil seines Vertrags anbietet, das andere implementieren sollen, gehört es zum Anbieter: `http.Handler`, `io.Reader`, `sql.Driver`, `slog.Handler`. Der Unterschied: Diese Interfaces beschreiben eine Rolle, für die es viele Implementierungen gibt. Ein Interface, das genau eine Implementierung im selben Paket hat und nur dazu dient, „gegen eine Abstraktion zu programmieren“, ist dagegen ein Import aus einer anderen Sprache.

## Funktionen als Bausteine

Wo C# ein Interface mit einer Methode verwendet, reicht in Go oft eine Funktion. Die Standardbibliothek zeigt das mit `http.HandlerFunc`:

```go
type HandlerFunc func(ResponseWriter, *Request)

func (f HandlerFunc) ServeHTTP(w ResponseWriter, r *Request) { f(w, r) }
```

Ein Funktionstyp mit einer Methode, die die Funktion selbst aufruft. Damit wird jede passende Funktion zu einem `http.Handler`. `mux.HandleFunc("GET /targets", a.listTargets)` nutzt das: `a.listTargets` ist eine Methode, als Wert übergeben, und wird intern in eine `HandlerFunc` verwandelt.

Das Beispiel [interfaces/main.go](../beispiele/04-go-denkweise/interfaces/main.go) überträgt das Muster auf Checks:

```go
type Checker interface {
	Check(url string) int
}

type CheckFunc func(url string) int

func (f CheckFunc) Check(url string) int { return f(url) }
```

Im Test genügt dann eine Zeile statt eines Fake-Typs:

```go
always := CheckFunc(func(string) int { return 204 })
NewRunner(store, always).RunOnce()
```

### Middleware

Middleware ist in Go kein Framework-Konzept, sondern eine Funktion, die einen Handler nimmt und einen Handler zurückgibt. So steht es in [middleware.go](../internal/httpapi/middleware.go):

```go
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
```

Verkettet wird durch Verschachteln, in `httpapi.New`:

```go
return a.logRequests(a.recoverPanics(securityHeaders(mux)))
```

Gelesen von außen nach innen: Zuerst misst `logRequests` die Dauer, dann fängt `recoverPanics` Panics ab, dann setzt `securityHeaders` die Header, dann routet der Mux. Es gibt kein `app.UseXxx()`, keine Pipeline-Konfiguration und keine Reihenfolge, die irgendwo anders festgelegt wird – die Reihenfolge steht in dieser einen Zeile.

Das Beispiel enthält dasselbe für Checks: `WithRetry(next Checker, n int) Checker` wiederholt einen Check, der mit 5xx fehlschlug. Der zurückgegebene Checker ist eine `CheckFunc`, die `next` aufruft.

### Abhängigkeiten als Funktion übergeben

Ein unscheinbares, aber typisches Beispiel ist [config.Load](../internal/config/config.go):

```go
// Load reads the configuration via getenv, typically os.Getenv.
// Passing the lookup function in keeps Load free of global state and
// trivially testable.
func Load(getenv func(string) string) (Config, error)

// FromEnv is Load with the real process environment.
func FromEnv() (Config, error) { return Load(os.Getenv) }
```

Statt `os.Getenv` direkt aufzurufen, bekommt `Load` die Funktion übergeben. In C# würde man dafür `IConfiguration` injizieren oder ein `IEnvironment`-Interface erfinden. In Go ist die Abhängigkeit „eine Funktion von String nach String“, und der Test in [config_test.go](../internal/config/config_test.go) übergibt eine Map:

```go
func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

cfg, err := Load(env(map[string]string{"UPWATCH_WORKERS": "2"}))
```

Keine globalen Variablen, kein `t.Setenv`, und nichts hindert die Tests daran, mit `t.Parallel()` gleichzeitig zu laufen. Dasselbe Muster findest du im Feld `now func() time.Time` in [check.go](../internal/check/check.go): Die Uhr ist eine Funktion, die sich in einem Test im selben Paket ersetzen ließe, ohne ein `IClock`-Interface zu erfinden.

## Möglichst wenig Abstraktion

Go-Code wirkt auf Umsteiger oft „zu einfach“: Funktionen, die direkt SQL ausführen, Handler, die direkt JSON schreiben, kaum Schichten. Das ist Absicht. Jede Abstraktion hat Kosten – eine Indirektion beim Lesen, eine Datei mehr, ein Name mehr, den man kennen muss – und sie zahlt sich nur aus, wenn sie ein konkretes Problem löst.

Fragen, die du dir vor jeder Abstraktion stellen kannst: Gibt es heute eine zweite Implementierung oder einen Test, der eine braucht? Wird Code wirklich dupliziert, oder sieht er nur ähnlich aus? Muss jemand, der den Code liest, die Abstraktion verstehen, bevor er das Eigentliche versteht? Die Go-Community hat dafür den Spruch „A little copying is better than a little dependency“ (ebenfalls ein Go-Proverb).

upwatch hat genau die Abstraktionen, die es braucht: zwei kleine Store-Interfaces (weil es Tests mit Fakes gibt und weil API und Scheduler unabhängig bleiben sollen), ein `Checker`-Interface im Scheduler (weil Tests nicht ins Netz gehen sollen) und eine Trennung zwischen Domänentypen und JSON-Typen in der API (weil sich die öffentliche API nicht mit dem Datenbankschema ändern soll). Ein Repository-Layer über sqlitestore, ein Service-Layer zwischen Handler und Store oder ein Interface für den Logger gibt es nicht, weil keines davon ein Problem löst, das upwatch hat.

## Packages sinnvoll schneiden

Ein Paket in Go ist ein Verzeichnis, und sein Name ist Teil jedes Aufrufs: `check.New`, `scheduler.New`, `monitor.NewTarget`. Daraus ergeben sich ein paar Regeln.

**Nach Verantwortung schneiden, nicht nach Schicht.** upwatch hat kein Paket `models`, `services` oder `utils`. Es hat `monitor` (was überwacht wird), `check` (wie eine Prüfung läuft), `scheduler` (wann geprüft wird), `sqlitestore` (wo gespeichert wird), `httpapi` (wie man von außen zugreift) und `config`. Jedes Paket kann man in einem Satz beschreiben.

**Namen kurz und ohne Wiederholung.** `check.Checker` statt `check.HTTPCheckerService`, `scheduler.New` statt `scheduler.NewScheduler`. Der Paketname ist schon Kontext. Pakete heißen im Singular und klein.

**Abhängigkeiten nur in eine Richtung.** Go verbietet zyklische Imports. Das zwingt zu einer klaren Schichtung: `monitor` importiert nichts aus dem Projekt; `check`, `sqlitestore`, `scheduler` und `httpapi` importieren `monitor`, aber nicht einander; nur `main` importiert alle. In C# lassen sich zyklische Abhängigkeiten zwischen Namespaces innerhalb einer Assembly leicht einschleichen, in Go kompiliert so etwas nicht.

**`internal/` schützt.** Pakete unter einem Verzeichnis `internal` dürfen nur von Code innerhalb des übergeordneten Verzeichnisses importiert werden. Damit ist alles unter [internal/](../internal/) für andere Module unsichtbar, und du kannst die APIs dort ändern, ohne jemanden zu brechen. Das ist das Gegenstück zu `internal` in C#, nur auf Ebene des Moduls.

Teil 09 geht ausführlicher auf Projektstruktur ein.

## Accept interfaces, return structs

Ein verbreiteter Leitsatz für Funktionssignaturen: **Parameter als Interface, Rückgabe als konkreter Typ.**

```go
func New(store Store, checker Checker, logger *slog.Logger, workers int, tick time.Duration) *Scheduler
func New(client *http.Client, timeout time.Duration) *Checker
func Open(ctx context.Context, path string) (*Store, error)
```

Parameter als Interface, weil die Funktion dann mit allem funktioniert, was die nötigen Methoden hat – inklusive Fakes. Rückgabe als konkreter Typ, weil der Aufrufer dann alles bekommt, was der Typ kann, und selbst entscheiden kann, welches Interface er daraus macht. Gäbe `sqlitestore.Open` ein `scheduler.Store` zurück, könnte die API ihn nicht verwenden. Gäbe es ein großes `sqlitestore.StoreInterface` zurück, wären wir wieder beim Interface auf Vorrat.

Auch diese Regel hat Ausnahmen, und upwatch enthält eine: `httpapi.New` gibt `http.Handler` zurück, kein `*API`.

```go
func New(store Store, logger *slog.Logger) http.Handler
```

Das ist bewusst. Der Aufrufer braucht nur etwas, das HTTP-Anfragen beantwortet, und das Ergebnis ist nicht der `*API`-Wert selbst, sondern der Mux eingewickelt in drei Middlewares. Ein konkreter Typ dafür hätte keinen Nutzen. Wenn eine Funktion je nach Situation verschiedene Implementierungen liefert oder das Ergebnis eine Komposition ist, ist ein Interface als Rückgabe richtig.

## Dependency Injection ohne Framework

In ASP.NET Core registrierst du Dienste in einem Container (`services.AddScoped<IStore, SqliteStore>()`), und das Framework erzeugt sie per Reflection, wenn ein Konstruktor sie verlangt. In Go übergibst du Abhängigkeiten als Parameter. Das ist alles. Der Ort, an dem das geschieht, heißt **Composition Root**, und in upwatch ist es `run` in [main.go](../cmd/upwatch/main.go):

```go
cfg, err := config.FromEnv()
…
logger := newLogger(cfg)
…
store, err := sqlitestore.Open(ctx, cfg.DBPath)
…
checker := check.New(check.NewClient(cfg.AllowPrivate), cfg.CheckTimeout)
sched := scheduler.New(store, checker, logger, cfg.Workers, cfg.Tick)

srv := &http.Server{
	Addr:    cfg.Addr,
	Handler: httpapi.New(store, logger),
	…
}
```

Rund zwanzig Zeilen, und der gesamte Objektgraph der Anwendung ist sichtbar: welche Teile es gibt, wer wen bekommt und in welcher Reihenfolge sie entstehen. Lebensdauern sind normale Go-Lebensdauern – `store` lebt, bis `run` zurückkehrt und `defer store.Close()` läuft. Es gibt keine Scopes, keine Registrierung, die man vergessen kann, und keinen Laufzeitfehler „Unable to resolve service“. Fehlt eine Abhängigkeit, kompiliert das Programm nicht.

Für sehr große Anwendungen gibt es Werkzeuge wie Google Wire, das diesen Code zur Übersetzungszeit generiert, oder Ubers fx, das zur Laufzeit verdrahtet. Für Dienste in der Größe von upwatch – und für deutlich größere – ist ausgeschriebener Code in `main` klarer.

Das Beispiel [interfaces/main.go](../beispiele/04-go-denkweise/interfaces/main.go) folgt demselben Aufbau. `run` bekommt seine Umgebung als Parameter, verdrahtet Store, Checker mit Retry-Middleware und Runner, und `main` ruft `run` mit den echten Werten auf:

```go
func main() {
	if err := run(os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

```bash
go run ./beispiele/04-go-denkweise/interfaces
RETRIES=0 go run ./beispiele/04-go-denkweise/interfaces
```

```
https://example.com        [200]
https://flaky.example      [200]
```

```
https://example.com        [200]
https://flaky.example      [503]
```

Weil `run` weder `os.Getenv` noch `os.Stdout` direkt verwendet, testet `TestRun` das komplette Programm ohne Umgebungsvariablen und ohne Ausgabe umzuleiten. upwatch trennt `main` und `run` aus einem verwandten Grund: `run` gibt einen Fehler zurück statt `os.Exit` aufzurufen, damit alle `defer` laufen.

## Zero Values nutzbar machen

In Teil 03 ging es darum, dass jede Variable einen Zero Value hat. Gute Go-Typen nutzen das: Ihr Zero Value ist sofort verwendbar, ohne Konstruktor. Die Standardbibliothek macht es vor:

```go
var mu sync.Mutex      // an unlocked mutex
var wg sync.WaitGroup  // a WaitGroup with counter 0
var buf bytes.Buffer   // an empty buffer, ready to write
var sb strings.Builder // an empty builder
var s []int            // a nil slice, append works
```

Für eigene Typen heißt das: Felder so wählen, dass „alles null“ einen sinnvollen Zustand beschreibt. Das Beispiel [embedding/main.go](../beispiele/04-go-denkweise/embedding/main.go) enthält eine Statistik ohne Konstruktor:

```go
type Stats struct {
	mu   sync.Mutex
	up   int
	down int
}

var stats Stats // no New, no init
stats.Record(200)
stats.Record(503)
```

Das funktioniert, weil `sync.Mutex` und `int` beide als Zero Value bereit sind und `Ratio` den Fall „noch keine Daten“ behandelt. Eine Map als Feld würde das brechen, weil das Schreiben in eine `nil`-Map eine Panic auslöst – dann muss die Methode sie bei Bedarf anlegen oder es braucht doch einen Konstruktor.

Nicht jeder Typ kann so gebaut werden, und dann sagt man es. [check.go](../internal/check/check.go) dokumentiert es ausdrücklich:

```go
// Checker issues HTTP requests and turns them into monitor.Results.
// The zero value is not usable; create one with New.
type Checker struct {
	client  *http.Client
	timeout time.Duration
	now     func() time.Time
}
```

Ein `Checker{}` hätte keinen Client, keinen Timeout und keine Uhr. Ein Zero Value mit Standardwerten wäre hier möglich, aber gefährlich: Ein Checker ohne SSRF-Schutz oder ohne Timeout sollte nicht versehentlich entstehen können. Darum gibt es `New`, und die Felder sind unexportiert, sodass niemand außerhalb des Pakets einen halb initialisierten Checker zusammenbauen kann.

Ähnlich bei `monitor.NewTarget`: Ein Intervall von `0` bedeutet „Standardintervall“. Der Zero Value des Parameters hat eine nützliche Bedeutung, statt ein Fehler zu sein. Auch das ist ein Fall von „Zero Values nutzbar machen“, nur bei Parametern statt bei Typen.

Zum Vergleich: In C# hat eine Klasse ohne Konstruktor-Aufruf gar keinen Wert, sondern `null`. In Go gibt es den Wert immer – die Frage ist nur, ob er etwas taugt.

## Im Projekt

- [internal/scheduler/scheduler.go](../internal/scheduler/scheduler.go) – `Store` und `Checker` als Interfaces beim Konsumenten, `New` nimmt Interfaces und gibt `*Scheduler` zurück.
- [internal/httpapi/httpapi.go](../internal/httpapi/httpapi.go) – ein zweites, anderes `Store`-Interface für denselben Store, `New` mit `http.Handler` als bewusster Interface-Rückgabe.
- [internal/httpapi/middleware.go](../internal/httpapi/middleware.go) – Middleware als Funktionen, `statusRecorder` mit eingebettetem `http.ResponseWriter`.
- [internal/sqlitestore/store.go](../internal/sqlitestore/store.go) – konkreter Typ ohne eigenes Interface; importiert nur `monitor`.
- [internal/config/config.go](../internal/config/config.go) – `Load(getenv)` als Funktion als Abhängigkeit.
- [cmd/upwatch/main.go](../cmd/upwatch/main.go) – Composition Root.

## Übungen

1. Schreib einen Test für den Scheduler, der einen Store verwendet, dessen `ClaimDue` immer einen Fehler zurückgibt. Wie viele Methoden muss dein Fake haben? Vergleiche mit dem, was du in C# mit einem `IStore` mit neun Methoden schreiben müsstest.
2. Ergänze in [middleware.go](../internal/httpapi/middleware.go) gedanklich eine Middleware `requireJSON`, die `POST`-Anfragen ohne `Content-Type: application/json` mit `415` ablehnt. Wo in der Kette aus `httpapi.New` würdest du sie einhängen, und warum?
3. Baue `countingStore` aus [embedding/main.go](../beispiele/04-go-denkweise/embedding/main.go) so um, dass er `Store` nicht einbettet, sondern als benanntes Feld hält. Welche Methoden musst du jetzt zusätzlich schreiben?
4. Angenommen, upwatch soll Ergebnisse zusätzlich als CSV exportieren. Skizziere ein Paket `export` mit einer Funktion, die die nötigen Daten aus dem Store liest. Welches Interface definiert es, und muss sqlitestore dafür geändert werden?

---

← [Teil 03: Die Sprache, kompakt](03-die-sprache-kompakt.md) · [Übersicht](../README.md) · [Teil 05: Fehlerbehandlung](05-fehlerbehandlung.md) →
