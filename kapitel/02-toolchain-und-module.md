# Teil 02: Toolchain und Module

Nach diesem Kapitel kennst du die Werkzeuge, die mit Go ausgeliefert werden, kannst ein Modul anlegen, Abhängigkeiten verwalten und auf Schwachstellen prüfen – und du verstehst, was in `go.mod` und `go.sum` steht und warum Go Versionen anders auflöst als npm oder NuGet.

## Ein Befehl für alles

In TypeScript-Projekten setzt du die Werkzeugkette aus Einzelteilen zusammen: `npm` oder `pnpm` für Pakete, `tsc` zum Übersetzen, ESLint, Prettier, Jest oder Vitest, dazu je eine Konfigurationsdatei. In .NET bündelt die `dotnet`-CLI das meiste davon; Go geht denselben Weg noch etwas konsequenter. Der Befehl `go` übersetzt, testet, formatiert, prüft, verwaltet Abhängigkeiten, zeigt Dokumentation an und lädt sogar neue Go-Versionen nach. Konfigurationsdateien für diese Werkzeuge gibt es nicht.

```bash
go version
```

```
go version go1.27.1 darwin/arm64
```

Die Befehle, die du täglich brauchst:

| Befehl | Zweck | Entspricht etwa |
|---|---|---|
| `go run .` | übersetzen und starten | `dotnet run`, `tsx main.ts` |
| `go build` | Binary bauen | `dotnet build`/`publish`, `tsc` |
| `go test ./...` | alle Tests ausführen | `dotnet test`, `jest` |
| `go fmt ./...` | Code formatieren | `dotnet format`, `prettier --write` |
| `go vet ./...` | statische Analyse | Roslyn-Analyzer, `eslint` |
| `go doc` | Dokumentation im Terminal | – |
| `go mod tidy` | `go.mod`/`go.sum` aufräumen | `npm prune` + `npm install` |
| `go get` | Abhängigkeit hinzufügen/ändern | `dotnet add package`, `npm install x` |

`./...` ist ein Paketmuster und bedeutet „dieses Verzeichnis und alle Unterverzeichnisse“. Fast jeder `go`-Befehl versteht es.

## Ein Modul anlegen

Ein Modul ist eine Sammlung von Paketen mit gemeinsamer Version – vergleichbar mit einem npm-Paket oder einer .NET-Solution mit einem NuGet-Paket. Es wird durch die Datei `go.mod` im Wurzelverzeichnis definiert. In einem leeren Verzeichnis:

```bash
mkdir hello && cd hello
go mod init example.com/hello
```

```
go: creating new go.mod: module example.com/hello
```

Die erzeugte `go.mod`:

```
module example.com/hello

go 1.27.1
```

Das Argument von `go mod init` ist der **Modulpfad**. Er ist der Präfix aller Importpfade in diesem Modul und sollte dort liegen, wo der Code später zu finden ist, typischerweise `github.com/<user>/<repo>`. Der Pfad ist kein Paketname wie bei npm, sondern eine Adresse: Wenn jemand `github.com/casoon/go-tutorial` importiert, weiß die Toolchain, wo sie den Code findet. Für Code, der nie veröffentlicht wird, reicht ein beliebiger Pfad mit Punkt im ersten Element, etwa `example.com/hello`.

Lege daneben eine `main.go` an:

```go
package main

import "fmt"

func main() {
	fmt.Println("hello, upwatch")
}
```

```bash
go run .
```

`go run .` übersetzt das Paket im aktuellen Verzeichnis in ein temporäres Binary und führt es aus. `go build` legt das Binary dagegen dauerhaft ab, hier als `./hello`. Der Name kommt vom letzten Element des Modulpfads.

### Das Beispiel im Repository

Im Tutorial-Repository liegt das Beispiel unter [beispiele/02-toolchain-und-module/hello](../beispiele/02-toolchain-und-module/hello/main.go). Es hat keine eigene `go.mod`, sondern gehört zum Modul des Repositorys – ein Modul kann beliebig viele Pakete enthalten, und jedes Verzeichnis ist genau ein Paket.

```go
func main() {
	fmt.Println(greeting(os.Args[1:]))
}

// greeting builds the message for the given URLs.
func greeting(urls []string) string {
	if len(urls) == 0 {
		return "upwatch: no URLs to watch yet"
	}
	return "upwatch: watching " + strings.Join(urls, ", ")
}
```

```bash
go run ./beispiele/02-toolchain-und-module/hello https://example.com
```

```
upwatch: watching https://example.com
```

Daneben liegt [main_test.go](../beispiele/02-toolchain-und-module/hello/main_test.go). Tests stehen in Go im selben Verzeichnis wie der Code, in Dateien mit der Endung `_test.go`. Sie werden beim normalen Build ignoriert.

```bash
go test ./beispiele/02-toolchain-und-module/...
```

```
ok  	github.com/casoon/go-tutorial/beispiele/02-toolchain-und-module/hello	0.232s
```

Mehr zu Tests in Teil 11.

## Formatieren, prüfen, nachschlagen

**`go fmt`** formatiert Code nach dem einen Go-Stil: Tabs zur Einrückung, feste Regeln für Leerzeichen und Klammern. Es gibt keine Optionen. Das klingt nach Bevormundung und ist in der Praxis eine Erleichterung: Diskussionen über Formatierung finden nicht statt, Diffs enthalten nur inhaltliche Änderungen, und jeder Go-Code sieht gleich aus. Der Befehl `gofmt -l .` listet nur die Dateien auf, die nicht formatiert sind – praktisch in CI. Dein Editor ruft `gofmt` (über `gopls`, den Language Server) normalerweise beim Speichern auf.

**`go vet`** findet Code, der kompiliert, aber fast sicher falsch ist: `Printf` mit falschen Platzhaltern, kopierte Mutexe, unerreichbarer Code, falsch geschriebene Struct-Tags, ein vergessenes `cancel` beim Context. `go test` führt eine Auswahl dieser Prüfungen automatisch mit aus. Für mehr Analysen nutzen viele Projekte zusätzlich `staticcheck` oder `golangci-lint`; für den Anfang genügt `go vet`.

**`go doc`** zeigt die Dokumentation eines Pakets, Typs oder einer Funktion im Terminal – auch für deinen eigenen Code:

```bash
go doc strings.Cut
```

```
package strings // import "strings"

func Cut(s, sep string) (before, after string, found bool)
    Cut slices s around the first instance of sep, returning the text before and
    after sep. The found result reports whether sep appears in s. If sep does
    not appear in s, cut returns s, "", false.
```

```bash
go doc ./internal/monitor
go doc ./internal/monitor.NewTarget
```

Dokumentation ist in Go einfach ein Kommentar direkt über der Deklaration, der mit deren Namen beginnt. Es gibt kein `///` mit XML wie in C# und keine JSDoc-Tags. Im Browser findest du dieselben Texte für jedes öffentliche Modul auf pkg.go.dev.

## Abhängigkeiten: go get und go mod tidy

Eine Abhängigkeit fügst du hinzu, indem du sie im Code importierst und dann `go mod tidy` ausführst, oder ausdrücklich mit `go get`:

```bash
go get modernc.org/sqlite@v1.60.1
```

`go get` ändert `go.mod` und `go.sum`, installiert aber nichts global und legt kein Verzeichnis im Projekt an. Heruntergeladene Module landen im Modul-Cache (`go env GOMODCACHE`, meist `~/go/pkg/mod`), den sich alle Projekte teilen. Ein `node_modules` pro Projekt gibt es nicht.

`go mod tidy` gleicht `go.mod` mit dem Code ab: Es fügt fehlende Abhängigkeiten hinzu, entfernt nicht mehr importierte und ergänzt `go.sum`. Führe es nach jeder Änderung an Imports aus. Wenn `go build` meldet, dass ein Paket fehlt, ist `go mod tidy` fast immer die Lösung.

Weitere nützliche Varianten:

```bash
go get -u ./...                 # alle Abhängigkeiten auf das neueste Minor/Patch
go get example.com/lib@none     # Abhängigkeit entfernen
go list -m -u all               # zeigen, wofür es Updates gibt
go mod why -m golang.org/x/sys  # warum ist dieses Modul im Build?
```

## go.mod im Detail

So sieht die `go.mod` von upwatch aus:

```
module github.com/casoon/go-tutorial

go 1.26.0

require modernc.org/sqlite v1.60.1

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.48.0 // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)
```

**`module`** ist der Modulpfad. Jedes Paket im Repository wird relativ dazu importiert, etwa `github.com/casoon/go-tutorial/internal/monitor`.

**`go 1.26.0`** ist die minimale Go-Version, die das Modul braucht. Sie legt auch fest, welche Sprachversion der Compiler verwendet. Ein Beispiel: Seit Go 1.22 bekommt jede Iteration einer `for`-Schleife eine eigene Schleifenvariable. Ein Modul mit `go 1.21` in der `go.mod` behält das alte Verhalten, auch wenn es mit Go 1.27 übersetzt wird. Go bricht so niemals still die Semantik älteren Codes.

**`require`** listet die Abhängigkeiten mit Version. Der erste Block enthält die direkte Abhängigkeit – den SQLite-Treiber, den upwatch selbst importiert. Der zweite Block enthält Module mit dem Kommentar `// indirect`: Sie werden nicht von upwatch importiert, sondern von `modernc.org/sqlite`. Seit Go 1.17 stehen alle Module, die für den Build der eigenen Pakete gebraucht werden, explizit in `go.mod`. Das macht die Datei länger, aber die Toolchain muss nicht mehr den gesamten Abhängigkeitsgraph laden, um zu bauen.

Warum `github.com/google/uuid` dabei ist, verrät `go mod why`:

```bash
go mod why github.com/google/uuid
```

```
# github.com/google/uuid
github.com/casoon/go-tutorial/internal/sqlitestore
modernc.org/sqlite
modernc.org/libc
github.com/google/uuid
```

Die Version `v0.0.0-20230129092748-24d4a6f8daec` ist eine **Pseudo-Version**: Das Modul `bigfft` hat keine Release-Tags, also verweist Go auf einen Commit mit Zeitstempel und Hash.

Module ab Version 2 tragen die Hauptversion im Pfad, etwa `github.com/jackc/pgx/v5`. Das ist die Import-Kompatibilitätsregel: Ein Importpfad muss über alle Versionen kompatibel bleiben; ein Breaking Change bekommt einen neuen Pfad. Dadurch können `v4` und `v5` derselben Bibliothek sogar gleichzeitig im Build vorkommen.

## go.sum: Prüfsummen statt Lockfile

`go.sum` enthält für jedes Modul im Graph kryptografische Prüfsummen:

```
modernc.org/sqlite v1.60.1 h1:/blz53O951KWFOso4QQvEs/Fq6cDBKLtMVrYNSeJVKw=
modernc.org/sqlite v1.60.1/go.mod h1:1dIoEagfDE72QytD5scH1lxARtaUgKgHC/NuApA27r0=
```

Die erste Zeile ist der Hash des gesamten Modulinhalts, die zweite der Hash nur seiner `go.mod`. Beim Download prüft die Toolchain jedes Modul gegen diese Hashes. Hat jemand eine veröffentlichte Version nachträglich verändert, schlägt der Build fehl. `go.sum` gehört deshalb ins Repository.

Der wichtige Unterschied zu `package-lock.json` oder `packages.lock.json`: `go.sum` bestimmt **nicht**, welche Versionen verwendet werden. Das tut allein `go.mod`. `go.sum` stellt nur sicher, dass die Versionen, die `go.mod` festlegt, unverändert sind. Ein separates Lockfile braucht Go nicht – das liegt am Algorithmus, mit dem Versionen ausgewählt werden.

## Minimal Version Selection

npm und NuGet lösen Versionsbereiche auf. `"lodash": "^4.17.0"` heißt „irgendeine 4.x ab 4.17.0“, und welche es wird, hängt davon ab, was zum Zeitpunkt der Installation veröffentlicht ist. Damit zwei Entwickler dasselbe bekommen, braucht es ein Lockfile.

Go verwendet **Minimal Version Selection** (MVS). Jede `require`-Zeile ist eine Mindestversion, kein Bereich. Verlangt upwatch `golang.org/x/sys v0.48.0` und eine Abhängigkeit `v0.45.0`, wählt Go die höchste der geforderten Mindestversionen, also `v0.48.0` – und nicht die neueste, die es gerade gibt. Das Ergebnis hängt nur von den `go.mod`-Dateien im Graph ab, nicht von der Uhrzeit. Derselbe Code baut heute und in drei Jahren mit denselben Versionen.

Der Trade-off: Updates passieren nie automatisch. Wenn eine Abhängigkeit einen Sicherheitsfix veröffentlicht, bekommst du ihn erst, wenn du `go get` ausführst. Deshalb ist der Abschnitt über `govulncheck` weiter unten kein Extra, sondern Teil des Arbeitsablaufs.

Die vollständige Liste aller Module, die MVS ausgewählt hat, zeigt:

```bash
go list -m all
```

Sie ist länger als `go.mod`, weil sie auch Module enthält, die zwar im Abhängigkeitsgraph vorkommen, aber für den Build der upwatch-Pakete nicht gebraucht werden (etwa `golang.org/x/tools`, das nur die Tests von `modernc.org/libc` brauchen – `go mod why -m golang.org/x/tools` zeigt die Kette).

## Modul-Proxy und Checksum-Datenbank

Woher kommen die Module? Standardmäßig nicht direkt von GitHub:

```bash
go env GOPROXY GOSUMDB
```

```
https://proxy.golang.org,direct
sum.golang.org
```

`proxy.golang.org` ist ein Cache, den das Go-Team betreibt. Er hält jede einmal angefragte Version dauerhaft vor. Wird ein Repository gelöscht oder ein Tag umgeschrieben, baut dein Projekt trotzdem weiter – ein Unterschied zu Ökosystemen, in denen das Entfernen eines Pakets fremde Builds brechen kann. `direct` heißt: Wenn der Proxy ein Modul nicht kennt, wird es direkt aus dem Versionskontrollsystem geholt.

`sum.golang.org` ist eine öffentliche, nur anhängbare Datenbank mit den Prüfsummen aller öffentlichen Modulversionen. Wenn du ein Modul zum ersten Mal hinzufügst und noch kein `go.sum`-Eintrag existiert, gleicht die Toolchain den Hash damit ab. Damit kann auch ein kompromittierter Proxy dir keine manipulierte Version unterschieben, ohne dass es auffällt.

Für private Module in Firmen-Repositorys setzt du `GOPRIVATE` (zum Beispiel `GOPRIVATE=github.com/meinefirma/*`). Diese Module werden dann direkt geholt und nicht gegen die öffentliche Datenbank geprüft.

## Die toolchain-Direktive

Seit Go 1.21 kann die Toolchain sich selbst aktualisieren. Verlangt die `go`-Zeile eines Moduls eine neuere Version als die installierte, lädt `go` die passende Version herunter und führt den Befehl damit aus – vorausgesetzt, `GOTOOLCHAIN` steht auf `auto`, was der Standard ist.

Mit der Direktive `toolchain` legst du zusätzlich fest, welche Version **bevorzugt** werden soll, ohne die Mindestanforderung zu erhöhen:

```bash
go mod edit -toolchain=go1.27.2
```

```
module example.com/hello

go 1.26.0

toolchain go1.27.2
```

Das heißt: Das Modul funktioniert ab Go 1.26.0, aber wer im Projekt arbeitet, nutzt 1.27.2. Ist lokal nur 1.27.1 installiert, meldet der nächste `go`-Befehl:

```
go: downloading go1.27.2 (darwin/arm64)
```

Die heruntergeladene Toolchain landet im Modul-Cache; deine Installation bleibt unverändert. Eine vergleichbare Funktion bietet in .NET die `global.json`, in Node das Feld `engines` zusammen mit einem Versionsmanager – nur dass Go das selbst erledigt.

## Schwachstellen finden: govulncheck

`govulncheck` gleicht deinen Code mit der Go-Schwachstellendatenbank (vuln.go.dev) ab. Es muss nicht installiert werden; `go run` mit Versionsangabe lädt und startet es:

```bash
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Im Unterschied zu `npm audit` prüft govulncheck nicht nur, ob ein betroffenes Modul im Graph vorkommt, sondern ob dein Code die betroffene Funktion tatsächlich aufruft. Das reduziert Fehlalarme erheblich. Die Ausgabe trennt deshalb zwischen Schwachstellen, die erreichbar sind, und solchen, die nur „irgendwo im Graph“ liegen.

Ein echter Lauf gegen upwatch mit Go 1.27.1 zeigt, wie das in der Praxis aussieht:

```
Vulnerability #1: GO-2026-6617
    Found in: net/http/internal/http2@go1.27.1
    Fixed in: net/http/internal/http2@go1.27.2
…
      #36: internal/check/check.go:114:16: check.Checker.Check calls io.Copy,
           which eventually calls http2.transportResponseBody.Read

Your code is affected by 9 vulnerabilities from the Go standard library.
This scan also found 2 vulnerabilities in packages you import and 2
vulnerabilities in modules you require, but your code doesn't appear to call
these vulnerabilities.
```

Die gefundenen Schwachstellen liegen in der Standardbibliothek, nicht in einer externen Abhängigkeit. Weil die Standardbibliothek ins Binary einkompiliert wird, hilft hier kein `go get`, sondern nur eine neuere Go-Version – also genau das, wofür die `toolchain`-Direktive da ist. govulncheck zeigt außerdem den Aufrufpfad aus deinem Code heraus, hier von `Checker.Check` bis in den HTTP/2-Client. Das ist der Grund, warum govulncheck in die CI gehört ([ci.yml](../.github/workflows/ci.yml) führt es bei jedem Lauf aus): Ein Projekt, an dessen Code sich nichts ändert, kann trotzdem morgen verwundbar sein.

## Build-Varianten

Ein paar `go build`-Optionen, die upwatch im [Makefile](../Makefile) verwendet:

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=v1.0.0" -o bin/upwatch ./cmd/upwatch
```

`CGO_ENABLED=0` schaltet die C-Anbindung ab und erzwingt ein statisch gelinktes Binary. `-trimpath` entfernt lokale Dateipfade aus dem Binary, damit Builds auf verschiedenen Rechnern identisch sind und keine Verzeichnisnamen preisgeben. `-ldflags "-s -w"` lässt Symboltabelle und Debug-Informationen weg und macht die Datei kleiner. `-X main.version=…` setzt beim Linken den Wert der Paketvariablen `version` in [main.go](../cmd/upwatch/main.go) – ein einfacher Weg, die Git-Version ins Binary zu bringen, ohne eine Datei zu generieren.

Welche Go-Version und welche Modulversionen in einem Binary stecken, liest `go version -m` aus:

```bash
go version -m bin/upwatch-linux-amd64
```

```
bin/upwatch-linux-amd64: go1.27.1
	path	github.com/casoon/go-tutorial/cmd/upwatch
	mod	github.com/casoon/go-tutorial	(devel)
	dep	github.com/dustin/go-humanize	v1.0.1	h1:GzkhY7T5VNhEkwH0PVJgjz+fX1rhBrR7pRT3mDkpeCY=
	…
```

Damit kannst du auch Jahre später feststellen, ob ein ausgeliefertes Binary von einer Schwachstelle betroffen ist. govulncheck prüft solche Binaries direkt mit `govulncheck -mode=binary`.

## Im Projekt

- [go.mod](../go.mod) und [go.sum](../go.sum) – eine direkte Abhängigkeit, der Rest indirekt über den SQLite-Treiber.
- [Makefile](../Makefile) – `make check` führt aus, was auch die CI prüft: `gofmt -l`, `go vet`, `go test -race` und die Beispiele.
- [.github/workflows/ci.yml](../.github/workflows/ci.yml) – der Hauptjob nimmt die aktuelle stabile Go-Version (sonst würde govulncheck die Schwachstellen eines alten Patch-Releases melden) und führt gofmt, vet, Tests mit Race-Detector, govulncheck und Cross-Compilation aus. Ein zweiter Job liest die Mindestversion aus `go.mod` (`go-version-file`) und prüft, dass upwatch damit noch baut und die Tests besteht.
- [Dockerfile](../Dockerfile) – kopiert zuerst nur `go.mod` und `go.sum` und führt `go mod download` aus, damit diese Schicht im Docker-Cache bleibt, solange sich die Abhängigkeiten nicht ändern.

## Übungen

1. Lege außerhalb des Repositorys ein Modul `example.com/ping` an, schreibe ein Programm, das `https://example.com` mit `http.Get` abruft und den Statuscode ausgibt, und baue es mit `GOOS=linux GOARCH=arm64 go build`. Prüfe das Ergebnis mit `file` und `go version -m`.
2. Führe im upwatch-Repository `go mod why -m golang.org/x/sys` und `go list -m all | wc -l` aus. Warum stehen in `go list -m all` mehr Module als in `go.mod`?
3. Führe govulncheck gegen upwatch aus. Welche Schwachstellen sind erreichbar, und was wäre die passende Gegenmaßnahme? Setze sie testweise mit `go mod edit -toolchain=…` um und mache die Änderung danach mit `git checkout go.mod` rückgängig.
4. Baue in einer Kopie der Datei [hello/main.go](../beispiele/02-toolchain-und-module/hello/main.go) absichtlich einen Fehler ein, den der Compiler nicht findet, `go vet` aber schon – zum Beispiel `fmt.Printf("%d\n", "text")`.

---

← [Teil 01: Warum gibt es Go?](01-warum-go.md) · [Übersicht](../README.md) · [Teil 03: Die Sprache, kompakt](03-die-sprache-kompakt.md) →
