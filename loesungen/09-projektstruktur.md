# Lösungen zu Teil 09: Projektstruktur und Architektur

Zu den Aufgaben: [Teil 09](../kapitel/09-projektstruktur.md#übungen)

## Übung 1: internal/ von außen importieren (Aufwärmen)

Das Testmodul liegt irgendwo außerhalb des Checkouts und besteht aus zwei Dateien. Den Pfad in der `replace`-Direktive passt du an deinen Checkout an:

```text
// go.mod
module example.com/other

go 1.26

require github.com/casoon/go-tutorial v0.0.0

replace github.com/casoon/go-tutorial => /pfad/zu/go-tutorial
```

```go
// main.go
package main

import (
	"fmt"

	"github.com/casoon/go-tutorial/internal/monitor"
)

func main() {
	fmt.Println(monitor.MaxURLLength)
}
```

Der erste `go build` verlangt noch ein `go mod tidy` (er normalisiert die `go`-Zeile zu `go 1.26.0`). Danach:

```bash
go mod tidy
go build ./...
```

```text
package example.com/other
	main.go:6:2: use of internal package github.com/casoon/go-tutorial/internal/monitor not allowed
```

Die Meldung ist dieselbe wie im Kapitel, nur die Position unterscheidet sich: Im Kapitel stand der Import einzeln (`import "…"`, daher `3:8`), hier steht er in einem Import-Block (`6:2`). Der Build bricht mit Exit-Code 1 ab, und zwar schon bei der Auflösung der Imports, bevor irgendetwas kompiliert wird.

**Warum so:** Die `replace`-Direktive ändert nur, *woher* der Code des Moduls kommt, nicht, *wer* er ist. Für den Compiler bleibt das Package `github.com/casoon/go-tutorial/internal/monitor`, und die Regel prüft den Import-Pfad des importierenden Packages (`example.com/other`) gegen den Teil vor `internal/`. Es gibt also keinen Trick über `replace`, `vendor` oder einen lokalen Pfad. Wer den Code wirklich von außen benutzen will, muss ihn aus `internal/` herausziehen, und das ist eine bewusste API-Entscheidung des Modulautors, keine des Nutzers.

## Übung 2: Skizze für internal/alert (Kern)

Es gibt mehrere vertretbare Schnitte; der folgende hält sich an das Muster von upwatch: Interfaces beim Konsumenten, Abhängigkeiten über den Konstruktor, Verdrahtung in `main`.

`alert` braucht drei Dinge von außen: die letzten Ergebnisse eines Ziels (um „dreimal hintereinander down“ zu erkennen), das Ziel selbst (für den Text der Mail) und einen Versandweg. Daraus werden zwei kleine Interfaces in `alert`:

```go
// History is what alert needs from persistence. It is defined here, at
// the consumer; *sqlitestore.Store satisfies it without knowing it.
type History interface {
	Target(ctx context.Context, id int64) (monitor.Target, error)
	Results(ctx context.Context, targetID int64, limit int) ([]monitor.Result, error)
}

// Sender delivers a notification, for example as a mail.
type Sender interface {
	Send(ctx context.Context, n Notification) error
}

type Notification struct {
	Target   monitor.Target
	Failures []monitor.Result
}

type Alerter struct {
	history   History
	sender    Sender
	logger    *slog.Logger
	threshold int
	cooldown  time.Duration
	now       func() time.Time

	mu       sync.Mutex
	lastSent map[int64]time.Time // target id -> time of the last alert
}

func New(history History, sender Sender, logger *slog.Logger, threshold int, cooldown time.Duration) *Alerter

// Observe is called once for every stored result.
func (a *Alerter) Observe(ctx context.Context, r monitor.Result) error
```

Aufrufen würde `Observe` der Writer des Schedulers, direkt nach `SaveResult` – dort kommt jedes Ergebnis genau einmal vorbei. Der Scheduler bekäme dafür sein eigenes, einmethodiges Interface, wieder beim Konsumenten:

```go
// in package scheduler
type Observer interface {
	Observe(ctx context.Context, r monitor.Result) error
}
```

`main.go` bräuchte zusätzlich den Import `internal/alert` und einen Import für die `Sender`-Implementierung, etwa ein Package `internal/mail` auf Basis von `net/smtp`. Die Verdrahtung wäre eine Zeile mehr: `alerter := alert.New(store, mailer, logger, 3, time.Hour)`, und `alerter` geht als weiterer Parameter an `scheduler.New`.

Der Patch enthält die Typen und Signaturen (der Rumpf von `Observe` ist ein `panic`) und einen Test, der nur zur Compile-Zeit prüft, dass `*sqlitestore.Store` das Interface `History` schon erfüllt:

```go
var _ History = (*sqlitestore.Store)(nil)
```

```bash
git apply loesungen/09-projektstruktur/uebung-2.patch
go test ./...
go list -f '{{.ImportPath}}: {{join .Imports " "}}' ./cmd/... ./internal/... \
  | sed 's|github.com/casoon/go-tutorial/||g'
```

```text
internal/alert: context internal/monitor log/slog sync time
internal/check: context errors fmt internal/monitor io net net/http net/netip syscall time
internal/config: errors fmt log/slog os strconv time
internal/httpapi: context encoding/json errors fmt internal/monitor log/slog net/http strconv time
internal/monitor: errors fmt net/url strings time unicode
internal/scheduler: context expvar internal/monitor log/slog sync time
internal/sqlitestore: context database/sql embed errors fmt internal/monitor io/fs modernc.org/sqlite modernc.org/sqlite/lib net/url sort time
```

`alert` importiert aus `internal/` nur `monitor` und steht damit in der mittleren Reihe neben `scheduler` und `sqlitestore`. Kein Zyklus ist möglich, weil keine Kante von `monitor` nach oben zeigt und `alert` niemanden aus seiner Reihe importiert. Der Test importiert zwar `sqlitestore` (`go list -f '{{.TestImports}}'` zeigt es), aber Test-Imports zählen nur für das Testbinary, und `sqlitestore` importiert `alert` nicht.

**Warum so:** `alert` definiert `History` selbst, statt `sqlitestore` zu importieren – dadurch bleibt es mit einem Fake testbar, und der Store muss nichts über Alarme wissen. Den Zustand „wann zuletzt gewarnt“ hält die Skizze im Speicher; das ist einfach, verliert aber bei einem Neustart die Sperre, sodass höchstens eine Mail doppelt kommt. Wer das nicht will, legt eine Tabelle an und erweitert `History` um zwei Methoden. Eine Alternative zum `Observer` im Scheduler wäre ein Decorator: `alert` umhüllt den Store, erfüllt `scheduler.Store` und ruft nach jedem `SaveResult` sich selbst auf. Dann bleibt der Scheduler unverändert, dafür ist der Ablauf schwerer zu finden, weil er in der Verdrahtung versteckt ist.

## Übung 3: ein gemeinsames Store-Interface (Kern)

Ein gemeinsames Interface müsste in einem Package liegen, das sowohl `httpapi` als auch `scheduler` importieren dürfen, ohne dass ein Zyklus entsteht. Dafür gibt es zwei Orte: `monitor` selbst oder ein neues Package wie `internal/store`, das nur das Interface enthält und `monitor` importiert. Das Interface hätte dann alle acht Methoden: `CreateTarget`, `Target`, `Targets`, `DeleteTarget`, `Results`, `Ping` aus der API und `ClaimDue`, `SaveResult` aus dem Scheduler. Beide Konsumenten würden ihr eigenes `Store`-Interface löschen und das gemeinsame importieren; `main.go` bliebe gleich, weil `*sqlitestore.Store` alle acht Methoden ohnehin hat.

Technisch geht das, aber es ist in upwatch schlechter, aus vier Gründen:

- **Fakes werden teuer.** `fakeStore` im Scheduler-Test hat heute zwei Methoden. Mit dem gemeinsamen Interface bräuchte er acht, davon sechs, die der Scheduler nie aufruft – oder den Embedding-Trick aus Teil 11, der bei einem unerwarteten Aufruf erst zur Laufzeit panickt.
- **Das Interface beschreibt den Anbieter, nicht den Bedarf.** Aus `scheduler.Store` liest man heute ab, was der Scheduler mit der Datenbank macht. Ein Interface mit acht Methoden sagt nur noch, was der Store kann.
- **Änderungen strahlen aus.** Braucht die API eine neunte Methode, ändert sich ein Typ, von dem auch der Scheduler abhängt; jeder Fake beider Packages muss nachgezogen werden.
- **Das Domänen-Package wird zur Ablage.** Liegt das Interface in `monitor`, beschreibt `monitor` plötzlich Persistenz und Readiness (`Ping`) – genau die Infrastrukturbegriffe, die der Paketkommentar ausschließt. Ein eigenes `internal/store` wäre sauberer, aber ein Package, das nur ein Interface enthält, ist ein deutliches Zeichen, dass die Abstraktion keinem Konsumenten gehört.

**Warum so:** In Java oder C# ist das gemeinsame `ITargetRepository` der Normalfall, weil eine Klasse ihre Interfaces ausdrücklich implementieren muss und deshalb eines vom Anbieter definiert wird. In Go erfüllt ein Typ jedes passende Interface implizit, also kostet ein weiteres kleines Interface nichts. Ein gemeinsames Interface lohnt sich erst, wenn es mehrere *Implementierungen* gibt, die austauschbar sein sollen (etwa SQLite und PostgreSQL) und mehrere Konsumenten wirklich dieselbe Methodenmenge brauchen. Selbst dann würde man es eher aus kleinen Interfaces zusammensetzen (`type Store interface { TargetReader; ResultWriter }`) als ein breites zu teilen.

## Übung 4: Durchreich-Services in einer Schichtenarchitektur (Profi)

Diese Übung hängt an deinem eigenen Projekt, eine Musterlösung im engeren Sinn gibt es nicht. Hier ein typischer Fall aus ASP.NET, wie er in vielen Codebasen steht:

```text
Controllers/OrdersController.cs     → IOrderService
Services/OrderService.cs            → IOrderRepository, IMapper
Repositories/OrderRepository.cs     → DbContext
Models/Order.cs, Dtos/OrderDto.cs
```

Durchreicher erkennst du an Methoden, deren Rumpf aus einem einzigen Aufruf der nächsten Schicht besteht, eventuell plus Mapping: `GetById(id) => _mapper.Map<OrderDto>(_repo.GetById(id))`. Meist trifft das auf die Mehrzahl der Service-Methoden zu – Lesen, Löschen, Auflisten. Echte Logik steckt typischerweise in wenigen Methoden wie `PlaceOrder` (Lager prüfen, Preis berechnen, Zahlung anstoßen, Mail schicken).

In Go sähe dieselbe Anwendung etwa so aus:

```text
cmd/shop/main.go           wiring
internal/order/            domain: Order, NewOrder (validation), errors
internal/checkout/         PlaceOrder: the only real coordination logic
internal/httpapi/          handlers + DTOs; calls the store directly for reads,
                           checkout.PlaceOrder for orders
internal/pgstore/          *Store with SQL; satisfies the small interfaces
                           defined in httpapi and checkout
```

Die Lese- und Lösch-Endpunkte rufen den Store direkt auf, wie `httpapi` in upwatch. Das Mapping zwischen Domäne und Wire-Format übernimmt eine kleine Funktion wie `toTargetJSON`, kein Mapper-Framework. Nur `PlaceOrder` bekommt ein eigenes Package, benannt nach dem, was es tut (`checkout`), nicht nach der Schicht (`services`). `IOrderService` und `IOrderRepository` als vom Anbieter definierte Interfaces fallen weg; jeder Konsument definiert das, was er braucht.

**Warum so:** Der Maßstab ist, ob eine Schicht eine Entscheidung trifft oder nur weiterreicht. Durchreich-Schichten kosten in Go mehr als in C#, weil jede Grenze zwischen Packages Bezeichner exportieren muss und jede zusätzliche Indirektion einen weiteren Fake im Test bedeutet. Wenn du die markierten Methoden zählst, hast du außerdem eine gute Schätzung, wie viel Code der Umbau spart. Wichtig ist, nicht ins Gegenteil zu kippen: Wo wirklich koordiniert wird, ist ein eigenes Package richtig – nur eben eines pro Aufgabe, nicht eines pro Schicht.
