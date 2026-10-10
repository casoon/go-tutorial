# Teil 11: Testing

Nach diesem Kapitel schreibst du Tests im Go-Stil: table-driven, ohne Assertion- und Mock-Bibliothek, mit `httptest`, echten Datenbanken, einer Fake-Uhr für zeitabhängigen Code, Benchmarks, Fuzzing und Race Detector.

## Der kleinste Test

```go
func TestAdd(t *testing.T) {
	got := Add(2, 3)
	if got != 5 {
		t.Fatalf("got %d, want 5", got)
	}
}
```

Mehr Infrastruktur braucht ein Go-Test nicht. Die Regeln sind kurz: Tests liegen in Dateien, die auf `_test.go` enden, direkt neben dem Code. Eine Testfunktion beginnt mit `Test`, gefolgt von einem Großbuchstaben, und nimmt ein `*testing.T`. `go test ./...` findet und startet alle. Es gibt kein Testprojekt, keinen Test-Runner als Abhängigkeit, keine Attribute wie `[Fact]` oder `@Test` und keine Konfigurationsdatei.

Die Testdateien gehören normalerweise zum selben Package wie der Code (`package monitor`) und sehen deshalb auch unexportierte Bezeichner. Alternativ kannst du `package monitor_test` schreiben; dann testest du nur die öffentliche API, wie ein externer Nutzer. upwatch nutzt durchgehend die erste Variante, weil etwa `httpapi_test.go` den unexportierten Typ `targetJSON` zum Dekodieren benutzt.

## Keine Assertion-Bibliothek

Wer von xUnit, NUnit oder Jest kommt, sucht zuerst nach `Assert.Equal` oder `expect(...).toBe(...)`. Go hat so etwas in der Standardbibliothek bewusst nicht. Eine Prüfung ist ein normales `if`, und die Fehlermeldung schreibst du selbst.

Das wirkt zuerst umständlich, hat aber zwei Vorteile. Erstens ist Testcode normaler Go-Code: Schleifen, Hilfsfunktionen, Vergleiche – alles, was du sonst auch schreibst, ohne eine zweite Sprache aus Matchern zu lernen. Zweitens zwingt es zu guten Fehlermeldungen. Die Konvention dafür ist:

```go
t.Errorf("NormalizeURL(%q) = %q, want %q", tt.in, got, tt.want)
```

Erst der Aufruf mit Eingabe, dann das tatsächliche Ergebnis, dann das erwartete – in dieser Reihenfolge, mit „got“ vor „want“. Wer die Meldung liest, sieht, was aufgerufen wurde und was falsch ist, ohne in den Testcode zu schauen. `%q` setzt Strings in Anführungszeichen, damit Leerzeichen und leere Strings sichtbar sind.

Für Vergleiche von Slices und Maps gibt es `slices.Equal` und `maps.Equal`, für beliebige Strukturen `reflect.DeepEqual`. Wenn du Diffs größerer Strukturen brauchst, ist `github.com/google/go-cmp` die verbreitete Ergänzung.

**testify** (`github.com/stretchr/testify`) ist die bekannteste Assertion-Bibliothek für Go und in vielen Projekten zu finden. `assert.Equal(t, want, got)` ist kürzer als ein `if` mit `Errorf`, und wer aus Jest oder xUnit kommt, fühlt sich sofort zu Hause. Die Gegenargumente: eine Abhängigkeit für etwas, das die Sprache schon kann, generische Meldungen ohne den Aufruf-Kontext, und das Paket `testify/mock` verführt zu Mock-lastigen Tests. upwatch kommt ohne aus. Wenn dein Team testify benutzt, ist das kein Fehler – nur nicht nötig.

## Errorf oder Fatalf

`t.Errorf` markiert den Test als fehlgeschlagen und läuft weiter. `t.Fatalf` markiert ihn als fehlgeschlagen und beendet ihn sofort (genauer: die aktuelle Goroutine des Tests über `runtime.Goexit`).

Die Faustregel: `Fatal`, wenn der Rest des Tests ohne diese Bedingung keinen Sinn ergibt; sonst `Error`. Im Store-Test wird nach einem fehlgeschlagenen `Results` mit `Fatalf` abgebrochen, weil der nächste Zugriff `results[0]` sonst panicken würde. Danach werden zwei Eigenschaften desselben Ergebnisses mit `Errorf` geprüft, damit du beide Abweichungen auf einmal siehst:

```go
if len(results) != 2 {
	t.Fatalf("len(results) = %d, want 2", len(results))
}
if want := base.Add(2 * time.Minute); !results[0].CheckedAt.Equal(want) {
	t.Errorf("newest CheckedAt = %s, want %s", results[0].CheckedAt, want)
}
if results[0].Duration != 30*time.Millisecond {
	t.Errorf("Duration = %s, want 30ms", results[0].Duration)
}
```

Eine Einschränkung: `Fatal` darf nur aus der Goroutine des Tests aufgerufen werden, nicht aus einer Goroutine, die der Test selbst gestartet hat. Dort benutzt du `Error` oder schickst den Fehler über einen Channel zurück.

## Table-driven Tests

Das wichtigste Muster in Go-Tests: Die Fälle stehen als Daten in einer Tabelle, die Prüflogik steht einmal in einer Schleife. So sieht der Test für `NormalizeURL` aus:

```go
func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "https", in: "https://example.com", want: "https://example.com"},
		{name: "trims space", in: "  http://example.com/a  ", want: "http://example.com/a"},
		{name: "drops fragment", in: "https://example.com/#top", want: "https://example.com/"},
		{name: "empty", in: "", wantErr: true},
		{name: "no scheme", in: "example.com", wantErr: true},
		{name: "ftp", in: "ftp://example.com", wantErr: true},
		{name: "no host", in: "https://", wantErr: true},
		{name: "credentials", in: "https://user:pw@example.com", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeURL(tt.in)
			if tt.wantErr {
				var ve *ValidationError
				if !errors.As(err, &ve) {
					t.Fatalf("NormalizeURL(%q) error = %v, want *ValidationError", tt.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeURL(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("NormalizeURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
```

Die Tabelle ist eine Slice anonymer Structs. Einen neuen Fall hinzuzufügen ist eine Zeile, und beim Lesen siehst du alle Fälle auf einen Blick – Eingabe neben erwarteter Ausgabe. Das entspricht `[Theory]` mit `[InlineData]` in xUnit oder `test.each` in Jest, nur ohne Sonderkonstrukt: Es ist eine Schleife.

Der Fehlerfall prüft nicht nur „irgendein Fehler“, sondern mit `errors.As` den Typ `*ValidationError`. Nur dieser Typ wird in der API zu einem 400; ein anderer Fehler wäre ein 500 und damit ein Bug.

Seit Go 1.22 hat jede Schleifeniteration ihre eigene Variable `tt`. Das früher übliche `tt := tt` vor `t.Run` brauchst du nicht mehr.

### Subtests mit t.Run

`t.Run` startet für jeden Fall einen Subtest mit eigenem Namen. Ein `Fatalf` beendet dann nur diesen Fall, nicht die ganze Tabelle. Und jeder Subtest ist einzeln adressierbar; Leerzeichen im Namen werden zu Unterstrichen:

```bash
go test -run 'TestNormalizeURL/no_scheme' -v ./internal/monitor
```

```
=== RUN   TestNormalizeURL
=== RUN   TestNormalizeURL/no_scheme
--- PASS: TestNormalizeURL (0.00s)
    --- PASS: TestNormalizeURL/no_scheme (0.00s)
PASS
ok  	github.com/casoon/go-tutorial/internal/monitor	0.205s
```

Wenn ein Fall scheitert, sieht die Ausgabe so aus (ein absichtlich fehlerhaftes `Classify` in einem Testmodul):

```
--- FAIL: TestClassify (0.00s)
    --- FAIL: TestClassify/redirect (0.00s)
        classify_test.go:19: Classify(301) = "down", want "up"
    --- FAIL: TestClassify/too_small (0.00s)
        classify_test.go:19: Classify(99) = "up", want "invalid"
FAIL
```

Weil `Errorf` benutzt wird und jeder Fall ein eigener Subtest ist, siehst du beide Fehler in einem Lauf.

### Tabellen als Szenario: httpapi_test.go

Der API-Test benutzt dieselbe Form für etwas anderes. Er legt erst ein Ziel an und spielt dann eine Folge von Requests gegen denselben Server durch:

```go
tests := []struct {
	name       string
	method     string
	path       string
	body       string
	wantStatus int
	wantBody   string
}{
	{"list", "GET", "/targets", "", 200, `"url":"https://example.com"`},
	{"get unknown", "GET", "/targets/99", "", 404, "not found"},
	{"duplicate", "POST", "/targets", `{"url":"https://example.com"}`, 409, "already exists"},
	{"unknown field", "POST", "/targets", `{"url":"https://a.de","admin":true}`, 400, "unknown field"},
	// ...
	{"delete", "DELETE", "/targets/1", "", 204, ""},
	{"delete again", "DELETE", "/targets/1", "", 404, "not found"},
}
```

Das ist kompakt und deckt in 17 Zeilen das ganze Fehler-Mapping ab. Es hat aber eine Eigenschaft, die du kennen solltest: Die Fälle hängen voneinander ab. „delete again“ erwartet 404, weil „delete“ vorher gelaufen ist. Startest du ihn allein, schlägt er fehl:

```bash
go test -run 'TestTargetsAPI/delete_again' ./internal/httpapi
```

```
--- FAIL: TestTargetsAPI (0.02s)
    --- FAIL: TestTargetsAPI/delete_again (0.00s)
        httpapi_test.go:102: status = 204, want 404 (body: )
FAIL
```

Für einen Szenario-Test ist das in Ordnung, solange man es weiß. Unabhängige Fälle – wie in `TestNormalizeURL` – könnten zusätzlich `t.Parallel()` aufrufen und gleichzeitig laufen; abhängige dürfen das nicht.

## Hilfsfunktionen: t.Helper, t.Cleanup, t.TempDir, t.Context

Tests in upwatch teilen Aufbau-Code über normale Funktionen, die `*testing.T` nehmen. Der Store-Test hat diese:

```go
// newStore opens a fresh database in a per-test temp directory.
// This is an integration test: it talks to a real SQLite database.
func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.Context(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
```

Vier Bausteine stecken darin:

**`t.Helper()`** markiert die Funktion als Hilfsfunktion. Schlägt darin ein `Fatalf` zu, zeigt die Meldung die Zeile im aufrufenden Test, nicht die Zeile in `newStore`. Ohne `Helper` würden alle Fehler auf dieselbe Zeile im Helper zeigen, und du wüsstest nicht, welcher Test betroffen ist.

**`t.TempDir()`** gibt ein frisches Verzeichnis zurück, das nach dem Test automatisch gelöscht wird. Jeder Test bekommt seine eigene Datenbankdatei, also gibt es keine Abhängigkeiten zwischen Tests und keine Aufräumarbeit.

**`t.Cleanup(f)`** registriert eine Funktion, die nach dem Test läuft, auch wenn er fehlschlägt. Mehrere Cleanups laufen in umgekehrter Reihenfolge, wie `defer`. Der Unterschied zu `defer`: `Cleanup` funktioniert auch aus einer Hilfsfunktion heraus. Ein `defer s.Close()` in `newStore` würde die Datenbank schließen, bevor der Test sie benutzt.

**`t.Context()`** (seit Go 1.24) liefert einen Context, der kurz vor den Cleanup-Funktionen abgebrochen wird. Für alles, was einen Context braucht, ist er im Test die richtige Wahl statt `context.Background()`: Goroutinen, die daran hängen, enden mit dem Test.

Das xUnit-Gegenstück wäre ein Konstruktor plus `IDisposable` oder eine Fixture-Klasse. In Go ist es eine Funktion, die du am Anfang des Tests aufrufst – explizit, ohne Lebenszyklus-Magie.

## httptest: echte HTTP-Server ohne Netzwerk-Setup

`net/http/httptest` bietet zwei Werkzeuge.

**`httptest.NewServer`** startet einen echten HTTP-Server auf einem freien Loopback-Port und gibt dir seine URL. Der API-Test benutzt das, um die komplette Handler-Kette mit Middleware zu testen, über echtes HTTP:

```go
func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	store, err := sqlitestore.Open(t.Context(), filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := httptest.NewServer(New(store, logger))
	t.Cleanup(srv.Close)
	return srv
}
```

Der Check-Test in [check_test.go](../internal/check/check_test.go) dreht das um: Dort ist der Testserver das Ziel, das upwatch prüft. Jeder Tabellenfall bringt seinen eigenen Handler mit – einer antwortet 503, einer leitet um, einer ist absichtlich langsam, um den Timeout zu testen.

**`httptest.NewRecorder`** ist ein `http.ResponseWriter`, der alles aufzeichnet. Damit rufst du einen Handler direkt wie eine Funktion auf, ganz ohne Netzwerk. Aus dem Beispiel zu diesem Kapitel:

```go
// serve calls h directly, without a network, and returns the recording.
func serve(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}
```

`NewServer` ist näher an der Realität (Header-Verarbeitung, Verbindungsabbruch, Client-Timeouts), `NewRecorder` ist schneller und einfacher. Für Handler-Logik reicht der Recorder; sobald der HTTP-Client oder Middleware-Verhalten über echte Verbindungen eine Rolle spielt, nimm den Server.

## Fakes statt Mocks

Der Scheduler braucht einen Store und einen Checker. Im Test bekommt er weder SQLite noch echtes HTTP, sondern zwei kleine selbstgeschriebene Typen:

```go
// fakeStore hands out every target exactly once and records saved results.
// A fake is simpler and more honest than a mock: it has real behavior.
type fakeStore struct {
	mu      sync.Mutex
	pending []monitor.Target
	saved   []monitor.Result
}

func (f *fakeStore) ClaimDue(_ context.Context, _ time.Time, limit int) ([]monitor.Target, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := min(limit, len(f.pending))
	due := f.pending[:n]
	f.pending = f.pending[n:]
	return due, nil
}

func (f *fakeStore) SaveResult(_ context.Context, r monitor.Result) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, r)
	return nil
}
```

Ein **Mock** prüft Interaktionen: „`SaveResult` wurde genau zehnmal mit diesen Argumenten aufgerufen.“ Ein **Fake** ist eine echte, vereinfachte Implementierung: Er verteilt Ziele und merkt sich Ergebnisse, und der Test prüft am Ende den Zustand. Fakes machen Tests weniger brüchig, weil sie nicht festschreiben, wie oft oder in welcher Reihenfolge der Code seine Abhängigkeiten aufruft – nur, dass am Ende das Richtige passiert ist.

Dass ein Fake in Go so billig ist, liegt an zwei Entscheidungen aus Teil 04 und Teil 09. Interfaces werden beim Konsumenten definiert und enthalten nur, was er braucht: `scheduler.Store` hat zwei Methoden, nicht dreißig. Und ein Typ erfüllt ein Interface implizit, also braucht `fakeStore` keine Deklaration und keinen Generator. In C# braucht man Moq oder NSubstitute oft, weil Interfaces breit sind und das Nachbauen mühsam wäre; in Java ist es Mockito, in TypeScript `jest.fn()`. In Go gibt es mit gomock und mockery ebenfalls Mock-Generatoren, aber bei kleinen Interfaces ist ein handgeschriebener Fake meist kürzer als die Mock-Konfiguration.

Noch kleiner wird ein Fake, wenn das Interface nur eine Methode hat. Dann reicht ein Funktionstyp, wie `http.HandlerFunc` in der Standardbibliothek:

```go
// pingFunc turns a function into a Pinger: the smallest possible fake.
type pingFunc func(context.Context) error

func (f pingFunc) Ping(ctx context.Context) error { return f(ctx) }

up := pingFunc(func(context.Context) error { return nil })
down := pingFunc(func(context.Context) error { return errors.New("database is locked") })
```

### Der Embedding-Trick: failingStore

Manchmal willst du für einen Test genau eine Methode eines großen Interfaces überschreiben. `httpapi.Store` hat sechs Methoden; der Test, der prüft, dass interne Fehler nicht an den Client gelangen, braucht nur eine fehlschlagende `Targets`:

```go
// failingStore makes Targets fail, to check that internal errors are
// not leaked to the client.
type failingStore struct{ Store }

func (failingStore) Targets(context.Context) ([]monitor.Target, error) {
	return nil, errors.New("secret: open /var/lib/upwatch/upwatch.db: permission denied")
}
```

`failingStore` bettet das Interface `Store` ein. Dadurch hat der Typ alle sechs Methoden und erfüllt das Interface. Das eingebettete Feld ist aber `nil`. `Targets` ist überschrieben, jede andere Methode würde an das nil-Interface weiterleiten und panicken. Der Test `TestPanicBecomes500` nutzt genau das absichtlich: Er ruft `GET /targets/1` auf, `Target` ist nicht überschrieben, der Handler panickt, und die Recover-Middleware muss daraus ein 500 machen.

Der Trick ist praktisch, hat aber einen Haken: Ruft der Code unter Test unerwartet eine andere Methode auf, merkst du es erst zur Laufzeit an einer Panic. Für gezielte Fehlerfälle ist das akzeptabel, für einen allgemeinen Fake nicht.

## Integrationstests gegen echte SQLite

upwatch testet den Store nicht mit einem Fake, sondern gegen echte SQLite-Datenbanken. `newStore` legt für jeden Test eine Datei in `t.TempDir()` an, führt die Migrationen aus und schließt sie am Ende. Auch der API-Test läuft gegen echte SQLite.

Das ist möglich, weil SQLite eingebettet ist: kein Server, kein Docker, keine Ports. Das Öffnen einer frischen Datenbank inklusive Migration dauert Millisekunden, und die ganze Store-Suite läuft in etwa einer Sekunde. Der Gewinn ist groß: Diese Tests prüfen das echte SQL, die echten Constraints (`UNIQUE`, `ON DELETE CASCADE`) und die echte Fehlerübersetzung. Ein Fake-Store hätte den Duplicate-Fehler nur nachgespielt.

Bei PostgreSQL ist der Aufwand höher. Übliche Wege sind eine Test-Datenbank in CI, `testcontainers-go` für einen Container pro Testlauf, oder eine Datenbank pro Test mit Template-Datenbanken. Langsame Integrationstests kannst du mit `if testing.Short() { t.Skip(...) }` überspringbar machen; `go test -short` lässt sie dann aus.

## Zeitabhängiger Code: testing/synctest

Der Scheduler arbeitet mit einem Ticker und echten Zeitabständen. Ein klassischer Test müsste echte Zeit warten – langsam – oder mit großzügigen Sleeps arbeiten – langsam und trotzdem gelegentlich falsch. Das Package `testing/synctest` (seit Go 1.25) löst das grundsätzlich:

```go
func TestRunChecksAllTargets(t *testing.T) {
	// synctest runs the test in a bubble with a fake clock: time only
	// advances when every goroutine in the bubble is blocked. The test is
	// fast and deterministic even though the code uses real timers.
	synctest.Test(t, func(t *testing.T) {
		store := &fakeStore{}
		for i := range 10 {
			store.pending = append(store.pending, monitor.Target{ID: int64(i + 1)})
		}

		ctx, cancel := context.WithCancel(t.Context())
		s := New(store, fakeChecker{}, discardLogger(), 3, time.Second)

		done := make(chan struct{})
		go func() {
			s.Run(ctx)
			close(done)
		}()

		// 10 checks of 100ms on 3 workers need 4 rounds = 400ms.
		time.Sleep(399 * time.Millisecond)
		synctest.Wait()
		if got := store.savedCount(); got != 9 {
			t.Fatalf("after 399ms saved = %d, want 9", got)
		}

		time.Sleep(time.Millisecond)
		synctest.Wait()
		if got := store.savedCount(); got != 10 {
			t.Fatalf("after 400ms saved = %d, want 10", got)
		}

		cancel()
		<-done
	})
}
```

`synctest.Test` führt die Funktion in einer **Bubble** aus. Alle Goroutinen, die darin gestartet werden – hier der Scheduler mit drei Workern, dem Writer und dem Dispatcher –, gehören dazu. Innerhalb der Bubble benutzt das `time`-Package eine **Fake-Uhr**, die bei Mitternacht UTC am 1. Januar 2000 beginnt. Diese Uhr steht still, solange irgendeine Goroutine in der Bubble noch arbeiten kann. Erst wenn alle „durably blocked“ sind – sie warten auf einen Channel der Bubble, auf `time.Sleep`, einen Timer oder eine `WaitGroup` –, springt die Uhr direkt zum nächsten Zeitpunkt, an dem etwas aufwacht.

Für den Test heißt das: `fakeChecker` wartet per `time.After` 100 ms. Die drei Worker blockieren, die Uhr springt um 100 ms, die drei Checks sind fertig, die Ergebnisse gehen an den Writer, die nächsten drei Ziele werden verteilt. Nach drei Runden (300 ms) sind neun Ergebnisse gespeichert, das zehnte entsteht in der vierten Runde bei genau 400 ms. Der Test prüft deshalb bei 399 ms auf neun und bei 400 ms auf zehn – eine Präzision, die mit echter Zeit unmöglich wäre.

**`synctest.Wait()`** wartet, bis alle anderen Goroutinen der Bubble blockiert sind. Ohne diesen Aufruf könnte der Test den Zähler lesen, während der Writer gerade noch speichert. Mit ihm ist garantiert, dass alles, was bis zu diesem Zeitpunkt passieren konnte, auch passiert ist.

Die Laufzeit des Tests ist dabei praktisch null:

```
=== RUN   TestRunChecksAllTargets
--- PASS: TestRunChecksAllTargets (0.00s)
=== RUN   TestRunStopsOnCancel
--- PASS: TestRunStopsOnCancel (0.00s)
```

Ein paar Regeln aus der Dokumentation, die in der Praxis zählen: Ein `sync.Mutex` zählt nicht als dauerhaft blockiert, Netzwerk-I/O auch nicht. Deshalb benutzt der Scheduler-Test einen Fake-Checker mit `time.After` statt eines `httptest.Server` – mit echtem Netzwerk in der Bubble würde die Uhr nicht zuverlässig weiterlaufen. Und Channels müssen innerhalb der Bubble erzeugt werden; der Scheduler erzeugt seine in `Run`, also passt das.

Für C#-Entwickler: Das Gegenstück wäre ein `TimeProvider` mit `FakeTimeProvider`, das man in den Code hineinreichen muss. synctest braucht keine Änderung am Produktionscode – `scheduler.go` ruft ganz normal `time.NewTicker` auf.

## Benchmarks mit b.Loop

Benchmarks stehen in denselben Testdateien, beginnen mit `Benchmark` und nehmen ein `*testing.B`:

```go
func BenchmarkNormalizeURL(b *testing.B) {
	for b.Loop() {
		_, _ = NormalizeURL("https://example.com/some/path?query=1#fragment")
	}
}
```

`b.Loop` (seit Go 1.24) ersetzt die ältere Form `for i := 0; i < b.N; i++`. Es startet die Zeitmessung erst beim ersten Aufruf, sodass Aufbau-Code davor nicht mitgezählt wird, und es verhindert, dass der Compiler den Aufruf im Schleifenrumpf wegoptimiert, weil das Ergebnis nicht benutzt wird. Beides musste man früher von Hand erledigen (`b.ResetTimer`, eine globale Senke für das Ergebnis).

```bash
go test -run=^$ -bench=. -benchmem ./internal/monitor
```

```
goos: darwin
goarch: arm64
pkg: github.com/casoon/go-tutorial/internal/monitor
cpu: Apple M4 Pro
BenchmarkNormalizeURL-14    	 7994814	       153.7 ns/op	     192 B/op	       2 allocs/op
PASS
```

`-run=^$` schaltet die normalen Tests ab, `-bench=.` startet alle Benchmarks, `-benchmem` zeigt Speicher pro Aufruf. Gemessen auf einem Apple-M-Rechner mit Go 1.27: rund 150 ns und zwei Allokationen pro Aufruf. Die Zahl hinter dem Namen (`-14`) ist `GOMAXPROCS`. Einzelne Läufe schwanken; für Vorher-nachher-Vergleiche führt man mehrere Läufe mit `-count=10` aus und vergleicht sie mit `benchstat` (`golang.org/x/perf/cmd/benchstat`). Mehr dazu in Teil 14.

## Fuzzing

Table-driven Tests prüfen die Fälle, an die du gedacht hast. Fuzzing (seit Go 1.18 eingebaut) erzeugt Eingaben, an die du nicht gedacht hast. Statt eines konkreten Ergebnisses formulierst du eine **Eigenschaft**, die für jede Eingabe gelten muss. Für `NormalizeURL` ist das Idempotenz: Was einmal normalisiert wurde, muss eine zweite Normalisierung unverändert überstehen.

```go
func FuzzNormalizeURL(f *testing.F) {
	f.Add("https://example.com")
	f.Add("http://[::1]:8080/path?q=1")
	f.Add("javascript:alert(1)")
	f.Fuzz(func(t *testing.T, raw string) {
		got, err := NormalizeURL(raw)
		if err != nil {
			return
		}
		// Whatever we accept must survive a second round unchanged.
		again, err := NormalizeURL(got)
		if err != nil {
			t.Fatalf("normalized URL %q rejected on second pass: %v", got, err)
		}
		if again != got {
			t.Fatalf("not idempotent: %q -> %q", got, again)
		}
	})
}
```

`f.Add` legt Startwerte fest, den **Seed-Korpus**. Bei einem normalen `go test` laufen nur diese Seeds, wie ein Tabellentest. Erst mit `-fuzz` beginnt der Fuzzer, sie zu verändern – abdeckungsgesteuert, also bevorzugt in Richtungen, die neue Codepfade erreichen:

```bash
go test -run=^$ -fuzz=FuzzNormalizeURL -fuzztime=10s ./internal/monitor
```

Als dieses Kapitel entstand, fand der Fuzzer auf einem Apple-M-Rechner mit Go 1.27 innerhalb dieser zehn Sekunden einen echten Fehler in upwatch (Ausgabe gekürzt, die URL ist über 2000 Zeichen lang):

```
        monitor_test.go:102: normalized URL "http://0/%21%A1%A1%A1…%A1%21%21%21%20%20%20%21" rejected on second pass: invalid url: too long

    Failing input written to testdata/fuzz/FuzzNormalizeURL/4b9a2fad74729647
    To re-run:
    go test -run=FuzzNormalizeURL/4b9a2fad74729647
FAIL
```

Die Ursache: `NormalizeURL` prüfte nur die Länge der **Eingabe** auf höchstens 2048 Bytes. Zurückgegeben wird aber `u.String()`, und das kodiert jedes Byte, das in einer URL nicht roh stehen darf, als `%XX` – aus einem Byte werden drei. Eine Eingabe aus `https://example.com/` und 700 Bytes `\xa1` ist 720 Bytes lang und wurde akzeptiert; die normalisierte URL ist 2120 Bytes lang und wurde beim zweiten Durchlauf als „too long“ abgelehnt. upwatch hätte also URLs gespeichert, die es selbst als ungültig ablehnt. Die Korrektur prüft die Länge dort, wo sie zählt – am Ergebnis:

```go
normalized := u.String()
if len(normalized) > MaxURLLength {
	return "", &ValidationError{Field: "url", Reason: "too long"}
}
return normalized, nil
```

Der nächste Fuzz-Lauf fand nach wenigen Sekunden den zweiten Fehler:

```
        monitor_test.go:108: not idempotent: "http://00? " -> "http://00?"
```

Eine Eingabe wie `http://00? #x` endet nicht auf Leerraum, `TrimSpace` lässt sie also in Ruhe. Erst das Entfernen des Fragments legt das Leerzeichen am Ende frei, und der zweite Durchlauf schneidet es ab. Die Korrektur ist grundsätzlicher: Eine gültige URL enthält nie rohen Leerraum, `url.Parse` lässt ihn in der Query aber durch. `NormalizeURL` lehnt solche Eingaben jetzt ab. Danach lief der Fuzzer 90 Sekunden ohne Fund.

Beide Fälle sind typisch für Fuzzing: Kein Mensch schreibt einen Tabellenfall mit 700 Bytes `\xa1` oder einem Leerzeichen vor einem Fragment. Die Eigenschaft „idempotent“ hat beide trotzdem gefunden.

Der Fuzzer schreibt jede fehlschlagende Eingabe nach `testdata/fuzz/FuzzNormalizeURL/`. Ab dann gehört sie zum Seed-Korpus und läuft bei jedem normalen `go test` mit. Beide Dateien liegen im Repository unter [`internal/monitor/testdata/fuzz/`](../internal/monitor/testdata/fuzz/FuzzNormalizeURL/) – sie sind jetzt Regressionstests. Zusätzlich hat `TestNormalizeURL` zwei lesbare Tabellenfälle bekommen („too long after encoding“, „inner whitespace“), damit der Fehler auch ohne kryptische Korpusdatei dokumentiert ist.

Gute Fuzz-Eigenschaften sind: Idempotenz (wie hier), Round-Trips (`Parse(Format(x)) == x`, siehe `FuzzParseInterval` im Beispiel), „stürzt nicht ab“ für Parser von Benutzereingaben, und Vergleich mit einer einfachen, langsamen Referenzimplementierung.

## Race Detector

Der Race Detector instrumentiert jeden Speicherzugriff und meldet, wenn zwei Goroutinen ohne Synchronisation auf dieselbe Variable zugreifen und mindestens eine schreibt. Du aktivierst ihn mit einem Flag:

```bash
go test -race ./internal/...
```

```
ok  	github.com/casoon/go-tutorial/internal/check	1.416s
ok  	github.com/casoon/go-tutorial/internal/config	1.320s
ok  	github.com/casoon/go-tutorial/internal/httpapi	1.464s
ok  	github.com/casoon/go-tutorial/internal/monitor	1.212s
ok  	github.com/casoon/go-tutorial/internal/scheduler	1.341s
ok  	github.com/casoon/go-tutorial/internal/sqlitestore	1.434s
```

Er findet nur Races, die während des Laufs tatsächlich auftreten, und macht Programme langsamer und speicherhungriger. Deshalb läuft er in Tests und CI, nicht in Produktion. In upwatch ist `-race` Teil von `make test` und `make check`, und die CI-Pipeline führt es bei jedem Pull Request aus. Wie ein Race-Report aussieht und wie man ihn liest, zeigt Teil 06. Hier nur der Hinweis, warum `fakeStore` einen Mutex hat: Der Dispatcher ruft `ClaimDue`, der Writer `SaveResult` und der Test selbst `savedCount` auf – drei Goroutinen, dieselben Felder. Ohne Mutex würde `-race` den Test zu Recht scheitern lassen.

## Coverage

```bash
go test -cover ./internal/...
```

```
ok  	github.com/casoon/go-tutorial/internal/check	0.489s	coverage: 93.0% of statements
ok  	github.com/casoon/go-tutorial/internal/config	0.379s	coverage: 92.3% of statements
ok  	github.com/casoon/go-tutorial/internal/httpapi	0.413s	coverage: 91.1% of statements
ok  	github.com/casoon/go-tutorial/internal/monitor	0.338s	coverage: 84.6% of statements
ok  	github.com/casoon/go-tutorial/internal/scheduler	0.346s	coverage: 89.5% of statements
ok  	github.com/casoon/go-tutorial/internal/sqlitestore	0.377s	coverage: 82.6% of statements
```

Für Details schreibst du ein Profil und wertest es aus:

```bash
go test -coverprofile=cover.out ./internal/monitor
go tool cover -func=cover.out
```

```
github.com/casoon/go-tutorial/internal/monitor/monitor.go:46:	Up		100.0%
github.com/casoon/go-tutorial/internal/monitor/monitor.go:56:	Error		0.0%
github.com/casoon/go-tutorial/internal/monitor/monitor.go:62:	NewTarget	87.5%
github.com/casoon/go-tutorial/internal/monitor/monitor.go:80:	NormalizeURL	87.5%
total:								(statements)	84.6%
```

`go tool cover -html=cover.out` öffnet den Quelltext im Browser, grün und rot eingefärbt. Die Zahlen sind ein Hinweis, kein Ziel. `ValidationError.Error` steht auf 0 %, weil die Tests den Fehlertyp prüfen, nicht den Text – das ist eine bewusste Entscheidung, keine Lücke. Interessanter ist die Frage, welche Zweige in `NormalizeURL` nicht abgedeckt sind; das HTML zeigt es sofort (der Zweig für zu lange URLs und der für nicht parsebare URLs).

## Das Beispiel zu diesem Kapitel

[beispiele/11-testing/uptime](../beispiele/11-testing/uptime/) enthält kleine Funktionen aus der Monitor-Domäne und eine Testdatei, die die Muster dieses Kapitels zeigt: `TestAdd`, Tabellentests für `Classify` und `ParseInterval`, `httptest.NewServer` für `Probe`, `httptest.NewRecorder` mit einem Funktions-Fake für `Readyz`, synctest für `WaitUntilUp`, einen Benchmark und einen Fuzz-Test mit Round-Trip-Eigenschaft.

```bash
go test -v ./beispiele/11-testing/uptime
go test -run=^$ -bench=. -benchmem ./beispiele/11-testing/uptime
go test -run=^$ -fuzz=FuzzParseInterval -fuzztime=5s ./beispiele/11-testing/uptime
```

Der synctest-Fall dort ist bewusst einfach. `WaitUntilUp` prüft jede Minute, ob ein Ziel erreichbar ist; der Test lässt es beim dritten Versuch gelingen und prüft, dass auf der Fake-Uhr exakt zwei Minuten vergangen sind:

```go
func TestWaitUntilUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		check := func(context.Context) bool {
			calls++
			return calls == 3 // up on the third attempt
		}

		start := time.Now()
		attempts, err := WaitUntilUp(t.Context(), time.Minute, check)
		if err != nil {
			t.Fatal(err)
		}
		if attempts != 3 {
			t.Errorf("attempts = %d, want 3", attempts)
		}
		if elapsed := time.Since(start); elapsed != 2*time.Minute {
			t.Errorf("elapsed = %s, want exactly 2m0s", elapsed)
		}
	})
}
```

```
=== RUN   TestWaitUntilUp
--- PASS: TestWaitUntilUp (0.00s)
```

Zwei Minuten Wartezeit, null Sekunden Laufzeit. Der Benchmark des Beispiels zeigt nebenbei, wie sich eine allokationsfreie Funktion darstellt:

```
BenchmarkParseInterval-14    	52976916	        22.64 ns/op	       0 B/op	       0 allocs/op
```

## Im Projekt

- [monitor_test.go](../internal/monitor/monitor_test.go) – Tabellentests, `FuzzNormalizeURL`, `BenchmarkNormalizeURL`.
- [httpapi_test.go](../internal/httpapi/httpapi_test.go) – Szenario-Tabelle über `httptest.NewServer` gegen echte SQLite, `failingStore` mit Interface-Embedding.
- [scheduler_test.go](../internal/scheduler/scheduler_test.go) – `fakeStore`, `fakeChecker`, synctest.
- [store_test.go](../internal/sqlitestore/store_test.go) – Integrationstests, `newStore` mit `t.Helper`, `t.TempDir`, `t.Cleanup`, `t.Context`.
- [check_test.go](../internal/check/check_test.go) – Testserver als Prüfziel, Timeout-Fall.
- [config_test.go](../internal/config/config_test.go) – `Load` mit einer Map statt echter Umgebungsvariablen (Teil 12).
- [Makefile](../Makefile) – `make test` und `make check` laufen mit `-race`.

## Übungen

1. Mach die Korrektur der Längenprüfung in `NormalizeURL` testweise rückgängig und starte `go test ./internal/monitor` – ohne `-fuzz`. Welcher Test schlägt fehl, und warum findet ihn ein normaler Testlauf? Schreib danach eine zweite Fuzz-Eigenschaft für `NormalizeURL`, etwa „das Ergebnis ist nie länger als `MaxURLLength`“ oder „das Ergebnis lässt sich mit `url.Parse` parsen und hat Schema http oder https“, und lass den Fuzzer eine Minute laufen.
2. Ergänze `TestNormalizeURL` um `t.Parallel()` in Test und Subtests und prüfe mit `go test -race -count=5`, dass alles grün bleibt. Warum wäre dasselbe in `TestTargetsAPI` falsch?
3. Schreib einen Scheduler-Test mit synctest, in dem `SaveResult` des Fakes einen Fehler zurückgibt. Prüfe, dass der Scheduler weiterläuft und alle Ergebnisse versucht zu speichern. Wie würdest du den expvar-Zähler `upwatch_save_failures_total` in diesem Test prüfen, und was ist daran problematisch?
4. Ersetze in `httpapi_test.go` den `failingStore` testweise durch einen vollständigen Fake mit allen sechs Methoden. Vergleiche Länge und Aussagekraft beider Varianten.

---

← [Teil 10: Datenbanken mit database/sql](10-datenbanken.md) · [Übersicht](../README.md) · [Teil 12: Logging, Konfiguration, Observability](12-logging-konfiguration-observability.md) →
