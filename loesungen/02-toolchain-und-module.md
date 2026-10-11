# Lösungen zu Teil 02: Toolchain und Module

Zu den Aufgaben: [Teil 02](../kapitel/02-toolchain-und-module.md#übungen)

Alle Übungen dieses Teils sind Experimente mit der Toolchain, die meisten in Wegwerf-Modulen außerhalb des Repositorys. Patches gibt es deshalb keine – Übung 4 würde zudem eine `go.mod` ändern und einen Toolchain-Download auslösen, und das gehört nicht in das Repository. Die Ausgaben stammen aus echten Läufen auf einem Apple-M-Rechner mit Go 1.27.1.

## Übung 1: `gofmt -l` und `go fmt` (Aufwärmen)

Im `vetdemo`-Modul, mit vier Leerzeichen statt Tab und Leerzeichen vor und nach dem Komma:

```go
func main() {
    fmt.Printf("checked %s targets\n" ,  "three")
}
```

```bash
gofmt -l .
echo $?
```

```
main.go
0
```

`gofmt -l` listet nur die Datei, die nicht formatiert ist, und ändert nichts. Was genau anders wäre, zeigt `gofmt -d .`:

```
-    fmt.Printf("checked %s targets\n" ,  "three")
+	fmt.Printf("checked %s targets\n", "three")
```

```bash
go fmt ./...
```

```
main.go
```

`go fmt` schreibt die Datei um und gibt die Namen der geänderten Dateien aus: Die Einrückung ist danach ein Tab, das Leerzeichen vor dem Komma ist weg, nach dem Komma steht genau eins. Ein zweites `gofmt -l .` gibt nichts mehr aus.

**Warum so:** `go fmt ./...` ist nur ein Wrapper, der `gofmt -l -w` auf die Dateien der Pakete anwendet – deshalb die Dateiliste als Ausgabe. Wichtig für CI ist das `0` oben: `gofmt -l` endet auch dann erfolgreich, wenn es unformatierte Dateien findet. Ein CI-Schritt muss deshalb prüfen, ob die Ausgabe leer ist; genau das macht `make check` im [Makefile](../Makefile) mit `test -z "$(gofmt -l .)"`.

## Übung 2: Ein Modul `example.com/ping` für Linux auf ARM64 (Kern)

```bash
mkdir ping && cd ping
go mod init example.com/ping
```

```go
package main

import (
	"fmt"
	"log"
	"net/http"
)

func main() {
	resp, err := http.Get("https://example.com")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	fmt.Println(resp.StatusCode)
}
```

```bash
go run .                          # 200
GOOS=linux GOARCH=arm64 go build
file ping
go version -m ping
```

```
ping: ELF 64-bit LSB executable, ARM aarch64, version 1 (SYSV), statically linked, Go BuildID=…, with debug_info, not stripped
ping: go1.27.1
	path	example.com/ping
	mod	example.com/ping	(devel)
	build	-buildmode=exe
	build	-compiler=gc
	build	CGO_ENABLED=0
	build	GOARCH=arm64
	build	GOOS=linux
	build	GOARM64=v8.0
```

Die Datei ist ein statisch gelinktes Linux-ARM64-Binary, 7,7 MB groß. Auf dem Mac lässt sie sich nicht starten (`exec format error`), auf einem Raspberry Pi oder einer Graviton-Instanz schon.

**Warum so:** `log.Fatal` ist für ein Kommandozeilenprogramm dieser Größe die passende Fehlerbehandlung; `defer resp.Body.Close()` gehört trotzdem dazu, auch wenn das Programm gleich endet (Teil 03). Zwei Zeilen in `go version -m` sind lehrreich. `CGO_ENABLED=0` steht dort, obwohl du es nicht gesetzt hast: Beim Cross-Compilieren schaltet Go cgo standardmäßig ab – und `net/http` kommt ohne cgo aus, der DNS-Resolver ist dann der in Go geschriebene. `(devel)` als Modulversion heißt, dass das Binary aus einem Arbeitsverzeichnis ohne Versions-Tag gebaut wurde; in einem Git-Repository kämen noch `vcs.revision` und `vcs.time` dazu. Im Unterschied zu upwatchs `make build` fehlen hier `-trimpath` und `-ldflags="-s -w"`, deshalb „with debug_info, not stripped“.

## Übung 3: `go mod why` und `go list -m all` (Kern)

```bash
go mod why -m golang.org/x/sys
```

```
# golang.org/x/sys
github.com/casoon/go-tutorial/beispiele/10-datenbanken/sqlite
modernc.org/sqlite
golang.org/x/sys/unix
```

```bash
go list -m all | wc -l
```

```
      26
```

Das ist das Hauptmodul plus 25 Abhängigkeiten. In der `go.mod` stehen dagegen nur zehn: `modernc.org/sqlite` direkt und neun als `// indirect`. Zu den zusätzlichen gehören `golang.org/x/tools`, `golang.org/x/mod`, `golang.org/x/sync`, `github.com/google/pprof`, `github.com/hashicorp/golang-lru/v2` und eine Reihe `modernc.org/…`-Module wie `cc/v4` und `ccgo/v4`.

`go mod why` zeigt die kürzeste Importkette von einem Paket des Repositorys zu einem Paket aus `golang.org/x/sys`. Bei mir beginnt sie in einem Beispiel aus Teil 10, nicht in `internal/sqlitestore` – beide importieren den Treiber, und Go nimmt irgendeine der kürzesten Ketten. Der SQLite-Treiber selbst importiert `golang.org/x/sys/unix`.

**Warum stehen mehr Module in `go list -m all`?** `go list -m all` zeigt den ganzen Modulgraph, den Minimal Version Selection auswertet: alle Module, die irgendeine `go.mod` im Graph verlangt, auch für Tests und Werkzeuge der Abhängigkeiten. Die `go.mod` von upwatch enthält seit Go 1.17 dagegen nur die Module, die Pakete liefern, welche für den Build und die Tests der *eigenen* Pakete gebraucht werden (Graph Pruning). `golang.org/x/tools` etwa braucht nur ein Test von `modernc.org/libc`:

```bash
go mod why -m golang.org/x/tools
```

```
# golang.org/x/tools
github.com/casoon/go-tutorial/beispiele/10-datenbanken/sqlite
modernc.org/sqlite
modernc.org/libc
modernc.org/libc.test
golang.org/x/tools/go/packages
```

Die vorletzte Zeile ist der Schlüssel: `modernc.org/libc.test` ist das Testbinary von `libc`. Dessen Tests führst du nie aus, also steht `x/tools` nicht in deiner `go.mod`, wohl aber im Graph.

**Warum so:** Die kurze `go.mod` ist eine bewusste Optimierung. Für einen Build muss die Toolchain nur die `go.mod`-Dateien der dort genannten Module lesen und nicht den transitiven Graph bis zum letzten Testwerkzeug. `go list -m all` ist das Werkzeug, wenn du wissen willst, welche Version eines Moduls MVS tatsächlich gewählt hätte, `go mod why` das, wenn du wissen willst, warum ein Modul überhaupt auftaucht – etwa, bevor du einem Sicherheitshinweis zu einem Modul hinterherjagst, das dein Code nie erreicht.

## Übung 4: Die toolchain-Direktive (Profi)

Installiert ist Go 1.27.1, das nächste Patch-Release ist 1.27.2:

```bash
go mod edit -toolchain=go1.27.2
cat go.mod
```

```
module example.com/ping

go 1.27.1

toolchain go1.27.2
```

Der erste `go`-Befehl in diesem Modul lädt die Toolchain herunter und führt sich damit aus. Damit der Download sichtbar wird, lief das Experiment mit einem leeren Modul-Cache (`GOMODCACHE` auf ein temporäres Verzeichnis gesetzt):

```bash
go version
```

```
go: downloading go1.27.2 (darwin/arm64)
go version go1.27.2 darwin/arm64
```

Was heruntergeladen wird, ist eine komplette Go-Distribution, verpackt als Modul `golang.org/toolchain` in der Version `v0.0.1-go1.27.2.darwin-arm64`. Sie landet im Modul-Cache: das Archiv unter `$(go env GOMODCACHE)/cache/download/golang.org/toolchain/@v/`, entpackt unter `$(go env GOMODCACHE)/golang.org/toolchain@v0.0.1-go1.27.2.darwin-arm64` – bei mir 243 MB. Wie lange der Download dauert, hängt an deiner Leitung; hier war es etwa eine Minute. Ein zweites `go version` startet ohne Verzögerung, weil die Toolchain im Cache liegt.

An deiner Go-Installation ändert sich nichts. `/usr/local/go/VERSION` sagt weiter `go1.27.1`. Der installierte `go`-Befehl startet nur, liest die `go.mod`, sieht die `toolchain`-Zeile und übergibt an das Binary aus dem Cache. Das sieht man an `go env GOROOT` im Modulverzeichnis:

```
/Users/…/go/pkg/mod/golang.org/toolchain@v0.0.1-go1.27.2.darwin-arm64
```

Außerhalb des Moduls zeigt `go env GOROOT` wieder `/usr/local/go`.

```bash
GOTOOLCHAIN=local go version
```

```
go version go1.27.1 darwin/arm64
```

Mit `GOTOOLCHAIN=local` wird nicht umgeschaltet und nichts geladen; es läuft die installierte 1.27.1, ohne Fehler. Die `toolchain`-Zeile ist nur eine Bevorzugung. Anders bei der `go`-Zeile, der Mindestversion: Mit `go 1.27.2` (per `go mod edit -go=1.27.2`) bleibt `GOTOOLCHAIN=local go version` zwar still, aber ein Build verweigert sich:

```
go: go.mod requires go >= 1.27.2 (running go 1.27.1; GOTOOLCHAIN=local)
```

Aufräumen: `go mod edit -toolchain=none` entfernt die Zeile wieder. Die Toolchain im Cache löscht erst `go clean -modcache` – zusammen mit allen anderen Modulen im Cache.

**Warum so:** Die Trennung in `go` (Mindestversion, bestimmt auch die Sprachsemantik) und `toolchain` (Empfehlung) erlaubt beides zugleich: Eine Bibliothek bleibt für alle ab 1.27.1 benutzbar, und wer im Projekt selbst arbeitet, bekommt automatisch das Patch-Release mit den aktuellen Sicherheitsfixes der Standardbibliothek. `GOTOOLCHAIN=local` ist der Schalter für Umgebungen, die nichts nachladen dürfen oder sollen – etwa ein CI-Job, der ausdrücklich gegen die installierte Mindestversion prüfen will, oder ein Build ohne Netzzugang. Weil die geladene Toolchain ein ganz normales Modul ist, gelten für sie dieselben Prüfsummen über `sum.golang.org` wie für jede Abhängigkeit.
