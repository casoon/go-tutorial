# Teil 03: Die Sprache, kompakt

Nach diesem Kapitel kannst du Go-Code lesen und selbst schreiben: Variablen, Typen, Kontrollfluss, Slices, Maps, Structs, Methoden, Pointer, Generics und `defer`. Das Kapitel ist bewusst kein Nachschlagewerk – es konzentriert sich auf das, was anders ist als in C# und TypeScript.

> **upwatch in diesem Teil:** die Domänentypen `Target` und `Result` in [internal/monitor/monitor.go](../internal/monitor/monitor.go); die Beispiele liegen unter [beispiele/03-die-sprache-kompakt](../beispiele/03-die-sprache-kompakt/)
> **Voraussetzungen:** Teil 01 und 02 (`go run`, `go test`)

## Jetzt du: erst ausführen, dann lesen

Bevor es um Details geht, starte das Beispiel, das die meisten Themen dieses Kapitels auf einer Seite zeigt:

```bash
go run ./beispiele/03-die-sprache-kompakt/grundlagen
```

```
Hello, Ada
https://example.com checked 2 times, every 1m0s
scheme: https rest: example.com
unknown 1
up      1
down    1
done (deferred, runs last)
```

Öffne danach [grundlagen/main.go](../beispiele/03-die-sprache-kompakt/grundlagen/main.go), ändere in `main` den Statuscode `503` auf `302` und starte erneut. Zwei Zeilen der Ausgabe ändern sich: `up` steigt auf 2, `down` fällt auf 0. Die Zeile `done …` steht weiterhin am Ende, obwohl sie im Code als Erstes kommt – warum, erklärt der Abschnitt über `defer`. Mach die Änderung danach rückgängig.

## Spickzettel: von C#, Java und TypeScript nach Go

Die Tabelle ordnet zu, was du kennst. Jede Zeile wird weiter unten oder in einem späteren Teil erklärt.

| C# | Java | TypeScript | Go |
|---|---|---|---|
| `var x = 1;` | `var x = 1;` | `let x = 1;` | `x := 1` |
| `class` mit Methoden | `class` mit Methoden | `class` | `struct` plus Methoden |
| `class A : IFoo` | `class A implements Foo` | `class A implements Foo` | nichts – Interfaces werden implizit erfüllt |
| `List<T>`, `T[]` | `ArrayList<T>`, `T[]` | `T[]` | Slice `[]T` |
| `Dictionary<K,V>` | `HashMap<K,V>` | `Map<K,V>` | `map[K]V` |
| `null` | `null` | `null`, `undefined` | `nil` bzw. Zero Value |
| `try`/`catch` | `try`/`catch` | `try`/`catch` | `error` als Rückgabewert (Teil 05) |
| `using`, `finally` | try-with-resources, `finally` | `finally` | `defer` |
| `async`/`await`, `Task` | Virtual Threads, `CompletableFuture` | `async`/`await`, `Promise` | Goroutine (Teil 06) |
| `public` | `public` | `export` | Großbuchstabe am Namensanfang |
| Konstruktor | Konstruktor | `constructor` | Funktion `New…` |

## Variablen und Konstanten

```go
var interval time.Duration = time.Minute // explicit type
var retries = 3                          // type inferred: int
timeout := 10 * time.Second              // short form, only inside functions
```

`:=` deklariert und initialisiert in einem Schritt; es entspricht `var` in C# oder `const`/`let` mit Typinferenz in TypeScript. Auf Paketebene ist nur `var` erlaubt.

Jede Variable hat ohne Initialisierung einen **Zero Value**: `0` für Zahlen, `""` für Strings, `false` für Bool, `nil` für Pointer, Slices, Maps, Channels, Funktionen und Interfaces. Für Structs ist es der Struct, dessen Felder alle ihren Zero Value haben. Es gibt kein `undefined`, keine uninitialisierte Variable und keine Warnung „possibly null“, weil es den Zustand „nicht zugewiesen“ nicht gibt. Teil 04 zeigt, wie man Typen so entwirft, dass der Zero Value sinnvoll ist.

Ungenutzte lokale Variablen und ungenutzte Imports sind Compilerfehler, keine Warnungen. Das nervt beim Ausprobieren und hält Code sauber.

Konstanten werden zur Übersetzungszeit ausgewertet. Mit `iota` entstehen Aufzählungen:

```go
type Status int

const (
	StatusUnknown Status = iota // 0, also the zero value
	StatusUp                    // 1
	StatusDown                  // 2
)
```

Go hat kein `enum`-Schlüsselwort. Ein eigener Typ mit Konstanten deckt den Großteil ab, ist aber nicht abgeschlossen: `Status(42)` kompiliert. Damit `Status` lesbar ausgegeben wird, bekommt er eine Methode `String()` – mehr dazu bei Interfaces. Der erste Wert heißt `StatusUnknown`, weil er zugleich der Zero Value ist: Ein vergessenes Feld hat dann einen ehrlichen Wert statt zufällig „up“.

## Datentypen

Die eingebauten Typen sind schnell aufgezählt: `bool`, `string`, Ganzzahlen (`int`, `int8` … `int64`, `uint` …, `uintptr`), `float32`, `float64`, `complex64/128`, dazu `byte` (= `uint8`) und `rune` (= `int32`, ein Unicode-Codepoint). `int` hat auf 64-Bit-Systemen 64 Bit.

Zwei Unterschiede zu C# und TypeScript fallen sofort auf. Erstens gibt es **keine impliziten Konvertierungen**, nicht einmal zwischen `int` und `int64`:

```go
var n int = 10
var d time.Duration = time.Duration(n) * time.Second // explicit
```

Zweitens sind Strings unveränderliche Byte-Folgen, meist UTF-8. `len(s)` liefert Bytes, nicht Zeichen; `for _, r := range s` iteriert über Runes. In C# und JavaScript sind Strings UTF-16 – ein Emoji hat dort Länge 2, in Go 4.

`time.Duration` ist ein gutes Beispiel für einen **benannten Typ**: `type Duration int64`. Er hat dieselbe Darstellung wie `int64`, ist aber ein eigener Typ mit eigenen Methoden (`String`, `Seconds` …). upwatch nutzt das überall, wo Zeitspannen vorkommen, etwa `Target.Interval`.

## if und switch

```go
if scheme, rest, ok := splitURL("https://example.com"); ok {
	fmt.Println("scheme:", scheme, "rest:", rest)
}
```

Keine Klammern um die Bedingung, geschweifte Klammern sind Pflicht. Vor der Bedingung darf eine Anweisung stehen, deren Variablen nur im `if`/`else` sichtbar sind. Dieses Muster wirst du bei Fehlern ständig sehen: `if err := f(); err != nil { … }`. Die Funktion `splitURL` mit ihren drei Rückgabewerten kommt weiter unten bei den Funktionen.

`switch` bricht nach jedem Fall ab, ein `break` ist nicht nötig (für den seltenen Fall des Durchfallens gibt es `fallthrough`). Ohne Ausdruck ersetzt `switch` eine `if`-`else`-Kette:

```go
func (r Result) Status() Status {
	switch {
	case r.StatusCode == 0:
		return StatusUnknown
	case r.StatusCode < 400:
		return StatusUp
	default:
		return StatusDown
	}
}
```

Pattern Matching wie `switch`-Ausdrücke in C# gibt es nicht. Der Typ-Switch (`switch v := x.(type)`) deckt den häufigsten Fall ab, Werte nach ihrem dynamischen Typ zu unterscheiden.

## for

Go hat genau eine Schleife:

```go
for i := 0; i < 3; i++ { … }      // classic
for status >= 500 { … }           // like while
for { … }                         // endless, leave with break or return
for i, r := range results { … }   // index and element
for range 3 { … }                 // n times, since Go 1.22
```

`range` funktioniert über Slices, Arrays, Strings, Maps, Channels, Funktionen (Iteratoren) und seit Go 1.22 auch über Ganzzahlen. Ein kleines Beispiel, das die URLs einer Liste nummeriert ausgibt:

```go
urls := []string{"https://example.com", "https://example.org"}
for i, u := range urls {
	fmt.Printf("%d: %s\n", i+1, u)
}
```

Seit Go 1.22 hat außerdem jede Iteration ihre eigene Schleifenvariable. Der klassische Fehler, dass alle Closures dieselbe Variable sehen (in C# vor Version 5 bei `foreach` berüchtigt), ist damit verschwunden.

## Arrays und Slices

Ein **Array** hat eine feste Länge, die Teil des Typs ist: `[3]int` und `[4]int` sind verschiedene Typen. Arrays sind Werte; eine Zuweisung kopiert alle Elemente. Im Alltag begegnen sie dir selten direkt.

Ein **Slice** ist das, was du in C# als `List<T>` und in TypeScript als Array verwendest. Technisch ist er eine kleine Struktur aus drei Feldern: ein Pointer auf ein darunterliegendes Array, eine Länge (`len`) und eine Kapazität (`cap`).

```go
var durations []int // nil slice: len 0, cap 0, append works
for i := range 6 {
	durations = append(durations, (i+1)*100)
	fmt.Printf("len=%d cap=%d %v\n", len(durations), cap(durations), durations)
}
```

```bash
go run ./beispiele/03-die-sprache-kompakt/slices
```

```
len=1 cap=1 [100]
len=2 cap=2 [100 200]
len=3 cap=4 [100 200 300]
len=4 cap=4 [100 200 300 400]
len=5 cap=8 [100 200 300 400 500]
len=6 cap=8 [100 200 300 400 500 600]
```

`append` schreibt in die freie Kapazität, wenn welche da ist. Wenn nicht, legt es ein größeres Array an, kopiert die Elemente und gibt einen Slice auf das neue Array zurück. Deshalb musst du das Ergebnis von `append` immer zuweisen. Kennst du die Größe vorher, reservierst du sie mit `make([]T, 0, n)` – das macht upwatch in den Handlern, bevor es Ergebnisse in JSON-Objekte umwandelt.

### Die Aliasing-Falle

Ein Teil-Slice `s[a:b]` kopiert nichts. Er zeigt auf dasselbe Array, und seine Kapazität reicht bis zum Ende des Originals. Das hat eine Konsequenz, die C#- und TypeScript-Entwickler regelmäßig überrascht:

```go
all := []int{200, 200, 503, 200}
prefix := all[:2]            // len 2, cap 4
prefix = append(prefix, 404) // fits into cap: overwrites all[2]
// all is now [200 200 404 200]
```

Der Test [TestAppendOverwritesSharedArray](../beispiele/03-die-sprache-kompakt/slices/main_test.go) prüft genau das. Die Lösung ist ein Slice-Ausdruck mit drei Indizes, der die Kapazität begrenzt:

```go
prefix := all[:2:2]          // len 2, cap 2
prefix = append(prefix, 404) // no room: append allocates a new array
```

Oder du kopierst ausdrücklich mit `slices.Clone`. Die Faustregel: Gibt eine Funktion einen Teil eines internen Slices heraus oder speichert sie einen übergebenen Slice, sollte sie kopieren, wenn der Aufrufer ihn danach noch verändern könnte.

Das Paket `slices` der Standardbibliothek enthält die Operationen, die du aus LINQ oder von `Array.prototype` kennst: `slices.Contains`, `slices.Index`, `slices.Sort`, `slices.SortFunc`, `slices.Equal`, `slices.MaxFunc` und weitere.

## Maps

```go
counts := map[Status]int{}
for _, r := range results {
	counts[r.Status()]++ // missing keys read as 0
}

n, ok := counts[StatusDown] // ok reports whether the key exists
delete(counts, StatusDown)
```

Eine Map entspricht `Dictionary<K,V>` oder `Map<K,V>`. Ein fehlender Schlüssel liefert den Zero Value statt einer Exception; ob er existiert, verrät die zweite Rückgabe. Schlüssel müssen vergleichbar sein (`==`), Slices sind es nicht.

Drei Dinge solltest du wissen. Die Iterationsreihenfolge ist zufällig; wer eine Reihenfolge braucht, sortiert die Schlüssel. Eine `nil`-Map – etwa ein Struct-Feld, das nie initialisiert wurde – kann gelesen, aber nicht beschrieben werden: Das Schreiben bricht das Programm mit einer *Panic* ab, Gos Gegenstück zu einer unbehandelten Exception (Teil 05). Deshalb legst du Maps mit `map[K]V{}` oder `make(map[K]V)` an. Und eine Map ist nicht dafür gebaut, dass mehrere gleichzeitig laufende Funktionen sie verändern; wie man sie dann schützt, zeigt Teil 06.

## Structs

Go hat keine Klassen. Es gibt Structs – Felder – und Methoden, die man für einen Typ deklariert:

```go
type User struct {
	ID   int
	Name string
}

func (u User) Greeting() string {
	return "Hello, " + u.Name
}
```

```go
u := User{ID: 1, Name: "Ada"}
fmt.Println(u.Greeting())
```

Es gibt keine Konstruktoren, keine Vererbung, keine `protected`-Members und keine Properties mit Gettern und Settern. Die Sichtbarkeit ergibt sich aus dem ersten Buchstaben: `Name` ist außerhalb des Pakets sichtbar, `name` nicht. Das gilt für alles – Typen, Funktionen, Felder, Methoden, Konstanten. Die Einheit der Kapselung ist das Paket, nicht der Typ.

Wo ein Konstruktor nötig ist, schreibt man eine normale Funktion, per Konvention `New` oder `NewXxx`. Sie kann validieren und einen Fehler zurückgeben, was ein C#-Konstruktor nur per Exception könnte:

```go
func NewTarget(rawURL string, interval time.Duration) (Target, error)
```

So steht es in [monitor.go](../internal/monitor/monitor.go). Die Domänentypen dort sind reine Daten:

```go
type Target struct {
	ID        int64
	URL       string
	Interval  time.Duration
	CreatedAt time.Time
}
```

Structs sind Werte. `b := a` kopiert alle Felder, und ein Struct, den du einer Funktion übergibst, ist eine Kopie. In C# entspricht das einem `struct`, nicht einer `class`; in TypeScript gibt es kein Gegenstück, weil Objekte immer Referenzen sind. Für kleine Structs wie `Target` und `Result` ist das genau richtig: Niemand kann ein Ergebnis verändern, das man ihm übergeben hat.

## Funktionen und mehrere Rückgabewerte

Funktionen können mehrere Werte zurückgeben. Das ersetzt `out`-Parameter, Tupel und – vor allem – Exceptions:

```go
func splitURL(raw string) (scheme, rest string, ok bool) {
	scheme, rest, ok = strings.Cut(raw, "://")
	return scheme, rest, ok
}
```

Die Rückgaben können wie hier Namen haben; das dokumentiert, was zurückkommt. Werte, die du nicht brauchst, ignorierst du mit `_`. Überladung gibt es nicht. Zwei Funktionen mit unterschiedlichen Parametern brauchen unterschiedliche Namen. Default-Parameter gibt es ebenfalls nicht; stattdessen übergibt man einen Struct mit Optionen oder nutzt den Zero Value als „Standard“, wie `NewTarget` mit dem Intervall `0`.

### Funktionen als Werte und Closures

Funktionen sind Werte: Du kannst sie Variablen zuweisen, übergeben und zurückgeben. Lambdas schreibst du als `func(x int) int { return x * 2 }` – länger als `x => x * 2`, aber ohne Sonderregeln. Eine Funktion, die Variablen aus ihrer Umgebung verwendet, ist eine Closure. upwatch nutzt das in `config.Load` in [config.go](../internal/config/config.go), um dieselbe Prüfung nicht für jede Umgebungsvariable neu auszuschreiben (gekürzt):

```go
var errs []error
num := func(key string, dst *int) {
	if v := getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			errs = append(errs, fmt.Errorf("%s: want a positive integer, got %q", key, v))
			return
		}
		*dst = n
	}
}

num("UPWATCH_WORKERS", &cfg.Workers)
```

`num` ist eine lokale Variable vom Typ `func(string, *int)`. Sie verwendet `getenv` (den Parameter von `Load`) und `errs` aus der umgebenden Funktion; ein `append` innerhalb der Closure verändert die äußere Variable `errs`. Das Ziel bekommt sie als Pointer `dst` – was `&cfg.Workers` und `*dst = n` bedeuten, zeigt der Abschnitt über Pointer.

## Methoden: Value- oder Pointer-Receiver

Eine Methode ist eine Funktion mit einem Receiver. Der Receiver ist entweder ein Wert oder ein Pointer, und das macht einen echten Unterschied. Das folgende `Target` ist ein vereinfachter Beispieltyp aus [grundlagen/main.go](../beispiele/03-die-sprache-kompakt/grundlagen/main.go), nicht der upwatch-Typ `monitor.Target` – das Feld `checks` gibt es nur im Beispiel:

```go
// Value receiver: works on a copy, cannot change r.
func (r Result) Status() Status { … }

// Pointer receiver: works on the original.
func (t *Target) RecordCheck() { t.checks++ }
```

```go
t := Target{URL: "https://example.com"}
t.RecordCheck() // Go takes &t automatically
```

Die Regeln dafür sind kurz. Nimm einen Pointer-Receiver, wenn die Methode den Wert verändert, wenn der Struct groß ist oder wenn er etwas enthält, das nicht kopiert werden darf (etwa einen `sync.Mutex`). Nimm einen Value-Receiver für kleine, unveränderliche Werte wie `monitor.Result`, dessen Methode `Up()` nur liest. Und bleib innerhalb eines Typs bei einer Variante – mischen verwirrt und hat Folgen bei Interfaces.

Der Test [TestRecordCheckNeedsPointer](../beispiele/03-die-sprache-kompakt/grundlagen/main_test.go) zeigt außerdem, dass eine Kopie unabhängig ist: Nach `cp := target` ändert `cp.RecordCheck()` das Original nicht.

In C# ist `this` in einer Klasse immer eine Referenz. In Go entscheidest du pro Typ, und der Methodenaufruf verrät dir, ob etwas verändert werden kann.

## Pointer

Ein Pointer `*T` zeigt auf einen Wert vom Typ `T`. `&x` nimmt die Adresse, `*p` liest den Wert, `*p = v` schreibt ihn. Pointer-Arithmetik gibt es nicht (außer über das Paket `unsafe`), und der Garbage Collector sorgt dafür, dass ein Wert lebt, solange jemand auf ihn zeigt. Du kannst also problemlos die Adresse einer lokalen Variablen zurückgeben:

```go
func newStats() *Stats {
	s := Stats{}
	return &s // fine: the compiler moves s to the heap
}
```

Pointer brauchst du aus zwei Gründen: um etwas zu verändern, das dir übergeben wurde (wie `dst` in der Closure oben), und um zu teilen statt zu kopieren. Der Scheduler in upwatch ist ein `*Scheduler`, weil es genau eine Instanz geben soll, die alle verwenden. Für „optional“ verwendet man Pointer in Go seltener als `T?` in C#; meist trägt der Zero Value die Bedeutung „nicht gesetzt“.

Ein `nil`-Pointer ist das Go-Gegenstück zur `NullReferenceException`: Der Zugriff löst eine Panic aus. Go hat keine Nullable Reference Types wie C# und keine `strictNullChecks` wie TypeScript. Die Verteidigung ist Konvention: Funktionen, die einen Pointer und einen `error` zurückgeben, liefern bei `err == nil` einen gültigen Pointer.

## Interfaces, kurz

Ein Interface beschreibt eine Menge von Methoden. Ein Typ erfüllt es, indem er die Methoden hat – ohne `implements`:

```go
type Stringer interface {
	String() string
}
```

`Status` aus dem Beispiel hat eine Methode `String() string` und erfüllt damit `fmt.Stringer`. Deshalb gibt `fmt.Println(StatusUp)` „up“ aus und nicht „1“. Das Paket `fmt` kennt `Status` nicht, und `Status` kennt `fmt.Stringer` nicht. TypeScript-Entwickler kennen das als strukturelle Typisierung; in Go gilt sie für Interfaces, nicht für Structs.

Interfaces sind der wichtigste Abstraktionsmechanismus in Go und bekommen deshalb einen großen Teil von Teil 04.

## Generics

Seit Go 1.18 gibt es Typparameter. Eine generische Funktion bekommt sie in eckigen Klammern, zusammen mit einem **Constraint**, der festlegt, welche Typen erlaubt sind:

```go
type Number interface {
	~int | ~int64 | ~float64
}

func Average[T Number](values []T) T {
	if len(values) == 0 {
		return 0
	}
	var sum T
	for _, v := range values {
		sum += v
	}
	return sum / T(len(values))
}
```

Ein Constraint ist ein Interface, das auch eine Typmenge enthalten kann. Die Tilde `~int64` schließt alle Typen ein, deren zugrunde liegender Typ `int64` ist – also auch `time.Duration`. Deshalb funktioniert derselbe Code für Statuscodes und Antwortzeiten:

```go
durations := Map(results, func(r Result) time.Duration { return r.Duration })
fmt.Println("average duration:", Average(durations))
```

```bash
go run ./beispiele/03-die-sprache-kompakt/generics
```

```
average duration: 113.333333ms
average status:   301
results for target 1: 2
slowest: 180ms
all up: false
```

Die eingebauten Constraints sind `any` (jeder Typ) und `comparable` (Typen, die `==` unterstützen und als Map-Schlüssel taugen). Weitere wie `cmp.Ordered` liefert die Standardbibliothek. [main.go](../beispiele/03-die-sprache-kompakt/generics/main.go) enthält noch `Map` mit zwei Typparametern und `GroupBy`, das `comparable` für den Schlüssel braucht.

Gegenüber C# fehlt einiges: Es gibt keine Varianz (`out T`/`in T`), keine Spezialisierung und keine Constraints der Art „hat einen parameterlosen Konstruktor“. Gegenüber TypeScript fehlen bedingte und gemappte Typen. Go-Generics sind für Container und Algorithmen gedacht, nicht für Typ-Akrobatik.

<details>
<summary>Methoden mit eigenen Typparametern</summary>

Methoden mit eigenen Typparametern erlaubt Go erst seit 1.27, und auch dann nur an konkreten Typen – Interface-Methoden dürfen keine Typparameter haben. Weil die `go.mod` von upwatch `go 1.26.0` verlangt, meldet der Compiler im Repository für eine solche Methode „generic method requires go1.27 or later“, auch wenn lokal Go 1.27 installiert ist: Die `go`-Zeile in `go.mod` bestimmt den Sprachstand (Teil 02).

</details>

### Wann nicht

Die Regel aus der Go-Community lautet: Schreib den Code erst für einen konkreten Typ. Wenn du dieselbe Funktion für einen zweiten Typ schreibst und sie sich nur im Typ unterscheidet, mach sie generisch. Generics sind falsch, wenn sich das Verhalten unterscheidet – dafür sind Interfaces da. Eine Funktion, die „irgendetwas Speicherbares“ entgegennimmt, braucht ein Interface `Store`, kein `T any`.

Und bevor du selbst ein `Map`, `Filter` oder `Max` schreibst, sieh in `slices`, `maps` und `cmp` nach:

```go
slowest := slices.MaxFunc(results, func(a, b Result) int {
	return cmp.Compare(a.Duration, b.Duration)
})
```

upwatch selbst kommt ganz ohne eigene generische Typen aus. Das ist typisch für Anwendungscode: Generics stecken in den Bibliotheken, die du benutzt.

## Errors, kurz

Fehler sind in Go Werte vom Interface-Typ `error`, und sie werden als letzte Rückgabe zurückgegeben:

```go
target, err := monitor.NewTarget(req.URL, interval)
if err != nil {
	return err
}
```

Es gibt keine Exceptions, kein `try`/`catch` und kein `throws`. Jeder Fehler ist im Code sichtbar, an der Stelle, an der er entsteht. Das ist der bekannteste Streitpunkt an Go, und er hat gute Gründe, die Teil 05 ausführlich behandelt – zusammen mit `errors.Is`, `errors.As`, eigenen Fehlertypen wie `monitor.ValidationError` und dem Unterschied zwischen `error` und `panic`.

## defer

`defer` verschiebt einen Funktionsaufruf auf das Ende der umgebenden Funktion – egal ob sie normal endet, mit `return` an beliebiger Stelle oder durch eine Panic:

```go
resp, err := c.client.Do(req)
if err != nil {
	…
}
defer resp.Body.Close()
```

Das ist Gos Gegenstück zu `using` in C#, try-with-resources in Java und `try`/`finally` in TypeScript. Der Vorteil: Das Aufräumen steht direkt neben dem Anfordern. Mehrere `defer` laufen in umgekehrter Reihenfolge (LIFO). Die Argumente werden beim `defer` ausgewertet, nicht erst beim Ausführen. Im Beispiel `grundlagen` steht `defer fmt.Println("done …")` in der ersten Zeile von `main` und erscheint trotzdem als letzte Ausgabe.

Zwei Verwendungen siehst du in upwatch immer wieder: `defer mu.Unlock()` direkt nach `mu.Lock()`, damit eine Sperre auch bei einem frühen `return` freigegeben wird, und `defer store.Close()` in [main.go](../cmd/upwatch/main.go), damit die Datenbank beim Beenden geschlossen wird.

Eine Falle: `defer` in einer Schleife läuft erst am Ende der Funktion, nicht am Ende der Iteration. Wer in einer Schleife Dateien öffnet, lagert den Schleifenrumpf in eine eigene Funktion aus.

## Im Projekt

- [internal/monitor/monitor.go](../internal/monitor/monitor.go) – Structs (`Target`, `Result`), Konstanten (`MinInterval` …), eine Methode mit Value-Receiver (`Result.Up`), ein Konstruktor mit Validierung (`NewTarget`) und ein eigener Fehlertyp (`ValidationError`).
- [internal/scheduler/scheduler.go](../internal/scheduler/scheduler.go) – `for range s.workers`, Pointer-Receiver auf `*Scheduler`, `defer ticker.Stop()`.
- [internal/config/config.go](../internal/config/config.go) – Closures (`str`, `num`, `dur`, `boolean`), die über Pointer auf Felder der Config schreiben.
- [internal/check/check.go](../internal/check/check.go) – `defer resp.Body.Close()`.

## Übungen

Musterlösungen mit Begründung stehen im Branch [`loesungen`](https://github.com/casoon/go-tutorial/blob/loesungen/loesungen/03-die-sprache-kompakt.md) – versuch es zuerst selbst.

1. **Aufwärmen:** Schreib eine Funktion `Uptime(results []monitor.Result) float64`, die den Anteil erfolgreicher Prüfungen berechnet, und nutze `Result.Up()`. Was gibst du für einen leeren Slice zurück, und brauchst du Generics?
2. **Kern:** Ergänze in [grundlagen/main.go](../beispiele/03-die-sprache-kompakt/grundlagen/main.go) einen Status `StatusDegraded` für Antworten, die zwar erfolgreich, aber langsamer als 2 Sekunden sind. Wo musst du überall etwas ändern (denk an `String` und an die Schleife in `main`), und welchen Testfall ergänzt du in `TestResultStatus`?
3. **Kern:** Die Closure `num` aus `config.Load` (oben gezeigt) schreibt über `*dst = n` in die Config. Schreib eine Variante, die stattdessen den Wert zurückgibt (`func(key string, def int) int`), und rufe sie für `UPWATCH_WORKERS` auf. Was wird an der Aufrufstelle länger, und wie kommt der Default jetzt hinein?
4. **Profi:** Erweitere [generics/main.go](../beispiele/03-die-sprache-kompakt/generics/main.go) um eine Funktion `Percentile[T cmp.Ordered](values []T, p float64) T`. Achte darauf, dass sie den übergebenen Slice nicht umsortiert – siehe Aliasing –, und schreib einen Test, der genau das prüft.

---

← [Teil 02: Toolchain und Module](02-toolchain-und-module.md) · [Übersicht](../README.md) · [Teil 04: Go-Denkweise statt OOP-Denkweise](04-go-denkweise.md) →
