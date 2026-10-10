# Teil 10: Datenbanken mit database/sql

Nach diesem Kapitel kannst du mit `database/sql` sicher lesen und schreiben, verstehst, was der Connection Pool tut, setzt Transaktionen richtig ein und kannst einschätzen, wann sich sqlc oder ein ORM lohnt.

> **upwatch in diesem Teil:** der SQLite-Store in [store.go](../internal/sqlitestore/store.go) mit dem Schema in [001_init.sql](../internal/sqlitestore/migrations/001_init.sql)
> **Voraussetzungen:** Teil 05 und 07; für `ClaimDue` hilft Teil 06

## database/sql in drei Zeilen

Go bringt eine Datenbankschnittstelle in der Standardbibliothek mit. Sie ist bewusst klein: Verbindungen verwalten, SQL ausführen, Ergebnisse in Variablen scannen. Kein Mapping auf Objekte, kein Query-Builder, keine Migrationen.

```go
row := db.QueryRowContext(ctx, "SELECT id, name FROM users WHERE id = ?", id)

var u User
if err := row.Scan(&u.ID, &u.Name); err != nil {
	return User{}, err
}
```

Das ist näher an ADO.NET oder JDBC als an Entity Framework, Hibernate oder Prisma:

| Aufgabe | C# | Java | Go |
|---|---|---|---|
| Pool | Pool hinter `DbConnection`, im Treiber versteckt | `DataSource`, etwa HikariCP | `*sql.DB` |
| Abfrage | `DbCommand.ExecuteReaderAsync`, Dapper `QueryAsync<T>` | `PreparedStatement.executeQuery()` | `db.QueryContext` |
| Zeilen lesen | `DbDataReader.Read()` | `ResultSet.next()`, `rs.getString(…)` | `rows.Next()`, `rows.Scan(&…)` |
| aufräumen | `using` / `await using` | try-with-resources | `defer rows.Close()` |
| Transaktion | `BeginTransaction()` plus `using` | `setAutoCommit(false)`, `commit()` / `rollback()` | `BeginTx` plus `defer tx.Rollback()` |

Du schreibst SQL, übergibst Parameter über Platzhalter und liest die Spalten in der Reihenfolge des `SELECT` in Zeiger auf deine Variablen. `Scan` konvertiert dabei zwischen Datenbank- und Go-Typen und meldet einen Fehler, wenn das nicht geht – etwa `NULL` in einen `string`.

Die Platzhaltersyntax hängt vom Treiber ab: SQLite und MySQL nehmen `?`, PostgreSQL nimmt `$1`, `$2`. `database/sql` selbst parst kein SQL, es reicht den Text an den Treiber durch.

## Der Treiber: modernc.org/sqlite

`database/sql` definiert nur Interfaces. Die eigentliche Arbeit macht ein Treiber, der sich beim Import unter einem Namen registriert. upwatch benutzt `modernc.org/sqlite`, registriert als `"sqlite"`:

```go
import (
	"database/sql"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)
// ...
db, err := sql.Open("sqlite", dsn)
```

Meist sieht man Treiber als Blank-Import (`_ "modernc.org/sqlite"`), weil man nur den Seiteneffekt der Registrierung braucht. upwatch importiert das Package mit Namen, weil es `sqlite.Error` für die Fehlerbehandlung benutzt (dazu unten mehr).

Die Wahl dieses Treibers hat einen handfesten Grund. SQLite ist in C geschrieben. Der verbreitete Treiber `github.com/mattn/go-sqlite3` bindet die C-Bibliothek über cgo ein. Das funktioniert gut, hat aber Folgen: Zum Bauen brauchst du einen C-Compiler, und für Cross-Compilation einen C-Compiler für die Zielplattform. `GOOS=linux GOARCH=arm64 go build` auf einem Mac reicht dann nicht mehr. `modernc.org/sqlite` dagegen ist eine automatische Übersetzung der SQLite-C-Quellen nach Go. Es gibt kein cgo, also funktioniert `CGO_ENABLED=0`, und der `build-all`-Target im [Makefile](../Makefile) baut von einem Rechner aus für Linux, macOS und Windows. Das Ergebnis ist ein statisches Binary, das im Distroless-Image ohne libc läuft (Teil 15).

Der Preis: Das Binary wird größer, und die übersetzte Variante gilt als etwas langsamer als die C-Bibliothek. Für einen Monitoring-Dienst, der ein paar Ergebnisse pro Sekunde schreibt, spielt das keine Rolle; für einen schreiblastigen Dienst solltest du selbst messen.

Für PostgreSQL ist `github.com/jackc/pgx` die übliche Wahl. pgx lässt sich als `database/sql`-Treiber benutzen (`pgx/v5/stdlib`) oder über seine eigene, PostgreSQL-spezifische API, die Dinge wie `COPY`, Listen/Notify und PostgreSQL-Typen direkt unterstützt. Für MySQL gibt es `github.com/go-sql-driver/mysql`. Alles, was in diesem Kapitel über Pool, Context, Transaktionen und Fehler steht, gilt unabhängig vom Treiber.

## sql.Open öffnet keine Verbindung

Der erste Stolperstein: `sql.Open` prüft nur seine Argumente und gibt ein `*sql.DB` zurück. Eine Verbindung zur Datenbank entsteht erst bei der ersten Abfrage oder bei `db.PingContext`. Ein falscher Pfad oder ein falsches Passwort fällt also nicht bei `Open` auf. upwatch merkt es trotzdem sofort beim Start, weil `Open` direkt danach die Migrationen ausführt, und `/readyz` ruft `Ping` auf (Teil 12).

`*sql.DB` ist auch keine Verbindung, sondern ein **Connection Pool**. Du öffnest es einmal beim Programmstart, teilst es zwischen allen Goroutinen – es ist für nebenläufige Nutzung gebaut – und schließt es beim Beenden. Ein `sql.Open` pro Request ist ein typischer Umsteigerfehler; in .NET übernimmt der Pool hinter `new SqlConnection(...)` diese Arbeit unsichtbar, in Go ist das `*sql.DB` selbst der Pool.

So öffnet upwatch die Datenbank:

```go
func Open(ctx context.Context, path string) (*Store, error) {
	q := url.Values{}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "busy_timeout(5000)")
	// Take the write lock at BEGIN, not at the first write. Otherwise a
	// read-then-write transaction can fail with SQLITE_BUSY mid-way.
	q.Set("_txlock", "immediate")
	dsn := "file:" + path + "?" + q.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// SQLite allows one writer at a time. A small pool is plenty and keeps
	// lock contention low; readers still run in parallel thanks to WAL.
	db.SetMaxOpenConns(4)
	db.SetConnMaxIdleTime(5 * time.Minute)

	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
```

Der DSN wird mit `url.Values` gebaut statt mit String-Verkettung, damit Sonderzeichen korrekt kodiert werden. Die Parameter mit Unterstrich wertet der Treiber aus; jedes `_pragma` wird auf jeder neuen Verbindung ausgeführt. Das ist wichtig, weil einige SQLite-Einstellungen pro Verbindung gelten und der Pool jederzeit neue Verbindungen öffnen kann.

Jede Einstellung hat einen Grund:

| Einstellung | Problem ohne sie | Wirkung |
|---|---|---|
| `foreign_keys(1)` | SQLite prüft Fremdschlüssel standardmäßig nicht; das `ON DELETE CASCADE` der Migration würde still ignoriert. | Fremdschlüssel werden geprüft, Löschen kaskadiert. |
| `journal_mode(WAL)` | Ein Schreibvorgang sperrt die ganze Datei, auch für Leser. | Leser und Schreiber blockieren sich nicht gegenseitig. |
| `busy_timeout(5000)` | Ist die Datenbank gerade gesperrt, bricht die Abfrage sofort mit `SQLITE_BUSY` ab. | SQLite wartet bis zu fünf Sekunden auf die Sperre. |
| `_txlock=immediate` | Eine Transaktion holt die Schreibsperre erst beim ersten Schreiben. Wollen zwei Lese-dann-Schreib-Transaktionen gleichzeitig schreiben, bricht SQLite eine mit `SQLITE_BUSY` ab – Warten hilft dort nicht, `busy_timeout` greift also nicht. | `BEGIN` holt die Schreibsperre sofort; die zweite Transaktion wartet schon beim `BEGIN`, wo `busy_timeout` greift. `ClaimDue` ist genau so eine Transaktion. |
| `SetMaxOpenConns(4)` | Mit unbegrenzt vielen Verbindungen warten mehr Schreiber auf dieselbe Sperre; mit einer einzigen würde ein laufender `ClaimDue` jede API-Anfrage blockieren. | Ein Schreiber, daneben parallele Leser dank WAL. Sind alle vier belegt, wartet die nächste Abfrage, bis eine frei wird oder ihr Context abläuft. |

Bei PostgreSQL sähe die Rechnung für den Pool anders aus: Dort begrenzt der Server die Verbindungszahl, und du richtest `SetMaxOpenConns` daran aus, wie viele Instanzen deines Dienstes laufen.

`SetConnMaxIdleTime` schließt Verbindungen, die fünf Minuten unbenutzt waren. Daneben gibt es `SetMaxIdleConns` (Standard: 2) und `SetConnMaxLifetime`, das bei Datenbanken hinter Load Balancern oder mit Failover wichtig ist, damit Verbindungen regelmäßig neu aufgebaut werden.

## Context bei jeder Abfrage

Alle Methoden, die mit der Datenbank reden, gibt es in zwei Varianten: `Query` und `QueryContext`, `Exec` und `ExecContext`, `Begin` und `BeginTx`. Benutze immer die mit Context. upwatch tut das ausnahmslos.

Der Grund ist derselbe wie in Teil 07: Bricht der Client einen HTTP-Request ab, wird `r.Context()` beendet, und eine laufende Abfrage mit diesem Context wird abgebrochen. Die Verbindung geht zurück in den Pool, statt für ein Ergebnis zu arbeiten, das niemand mehr liest. Ebenso begrenzt der Context die Wartezeit auf eine freie Verbindung, wenn der Pool voll ist. `/readyz` nutzt das, um den `Ping` auf eine Sekunde zu begrenzen (Teil 12).

## Eine Zeile lesen: QueryRow und ErrNoRows

```go
func (s *Store) Target(ctx context.Context, id int64) (monitor.Target, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, url, interval_seconds, created_at FROM targets WHERE id = ?`, id)
	t, err := scanTarget(row)
	if errors.Is(err, sql.ErrNoRows) {
		return monitor.Target{}, fmt.Errorf("target %d: %w", id, monitor.ErrNotFound)
	}
	if err != nil {
		return monitor.Target{}, fmt.Errorf("select target %d: %w", id, err)
	}
	return t, nil
}
```

`QueryRowContext` gibt nie einen Fehler direkt zurück. Fehler – auch „keine Zeile gefunden“ – kommen erst bei `Scan`. Für den Fall ohne Ergebnis gibt es den Sentinel-Fehler `sql.ErrNoRows`.

Wichtig ist die Übersetzung: `sql.ErrNoRows` ist ein Detail von `database/sql`. Würde der Store ihn durchreichen, müsste `httpapi` `database/sql` importieren, um ihn zu erkennen, und ein anderer Store (etwa im Speicher für Tests) müsste denselben Fehler vortäuschen. Stattdessen übersetzt der Store ihn in `monitor.ErrNotFound`, den Fehler der Domäne. `httpapi.handleError` kennt nur diesen und macht daraus ein 404. Das `%w` hält die Kette für `errors.Is` intakt (Teil 05).

`scanTarget` nimmt ein kleines Interface statt eines konkreten Typs:

```go
// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}
```

So benutzen `Target`, `Targets` und `ClaimDue` dieselbe Funktion, um eine Zeile in ein `monitor.Target` zu verwandeln, egal ob sie aus `QueryRow` oder aus einer Schleife über `Rows` kommt.

## Mehrere Zeilen lesen: rows.Close und rows.Err

```go
func (s *Store) Targets(ctx context.Context) ([]monitor.Target, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, url, interval_seconds, created_at FROM targets ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("select targets: %w", err)
	}
	defer rows.Close()

	targets := []monitor.Target{}
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			return nil, fmt.Errorf("scan target: %w", err)
		}
		targets = append(targets, t)
	}
	return targets, rows.Err()
}
```

Drei Details, die alle zählen:

`defer rows.Close()` gibt die Verbindung an den Pool zurück – das Gegenstück zu `using` um einen `DbDataReader` oder zu try-with-resources um ein JDBC-`ResultSet`. Solange `rows` offen ist, ist eine Verbindung belegt. Vergisst du `Close` in einem Fehlerpfad, verlierst du sie – bei einem Pool von vier Verbindungen hängt upwatch nach vier solchen Fehlern. `rows.Next` schließt zwar automatisch, wenn es das Ende erreicht, aber nicht, wenn du die Schleife vorher mit `return` verlässt.

`rows.Err()` nach der Schleife unterscheidet „alle Zeilen gelesen“ von „beim Lesen ist etwas schiefgegangen“. `rows.Next` gibt in beiden Fällen `false` zurück. Ohne diese Prüfung bekommst du bei einem abgebrochenen Context eine halbe Liste und hältst sie für vollständig.

`targets := []monitor.Target{}` statt `var targets []monitor.Target` sorgt dafür, dass die API für „keine Ziele“ `[]` statt `null` liefert; warum, steht in Teil 08.

## Schreiben und Constraint-Fehler

```go
res, err := s.db.ExecContext(ctx,
	`INSERT INTO targets (url, interval_seconds, created_at, next_check_at)
	 VALUES (?, ?, ?, ?)`,
	t.URL, int64(t.Interval/time.Second), now.UnixMilli(), now.UnixMilli())
if err != nil {
	if isUniqueViolation(err) {
		return monitor.Target{}, fmt.Errorf("target %s: %w", t.URL, monitor.ErrDuplicate)
	}
	return monitor.Target{}, fmt.Errorf("insert target: %w", err)
}
id, err := res.LastInsertId()
```

`ExecContext` gibt ein `sql.Result` zurück, aus dem du `LastInsertId` und `RowsAffected` lesen kannst. `DeleteTarget` nutzt `RowsAffected`, um ein Löschen ohne Treffer als `monitor.ErrNotFound` zu melden – so wird `DELETE /targets/99` zu einem 404 statt zu einem stillen 204. Beachte: `LastInsertId` wird nicht von jedem Treiber unterstützt; bei PostgreSQL nimmt man stattdessen `INSERT ... RETURNING id` mit `QueryRowContext`.

Die `url`-Spalte ist `UNIQUE`. Ein zweites Ziel mit derselben URL soll nicht als 500 enden, sondern als 409. Dafür muss der Store den Constraint-Fehler erkennen:

```go
func isUniqueViolation(err error) bool {
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}
```

`errors.As` sucht in der Fehlerkette nach einem `*sqlite.Error` und gibt dessen erweiterten Fehlercode heraus. Das ist verlässlicher als ein `strings.Contains(err.Error(), "UNIQUE")`, das an Formulierungen hängt, die sich zwischen Versionen ändern dürfen. Es ist auch die einzige Stelle, an der der Store wirklich treiberspezifisch ist. Bei pgx wäre es ein `*pgconn.PgError` mit dem SQLSTATE `23505`.

Eine Kleinigkeit in `SaveResult` lohnt einen zweiten Blick:

```go
`INSERT INTO results (target_id, status_code, duration_ms, error, checked_at)
 SELECT id, ?, ?, ?, ? FROM targets WHERE id = ?`
```

Statt `VALUES` steht hier ein `SELECT` auf die Zieltabelle. Wurde das Ziel gelöscht, während sein Check noch lief, liefert das `SELECT` keine Zeile, und es wird nichts eingefügt – statt eines Fremdschlüsselfehlers, der im Log als Fehler auftauchen würde, obwohl nichts kaputt ist.

## Transaktionen: ClaimDue

Der Scheduler fragt alle paar Sekunden: Welche Ziele sind fällig? Die naive Lösung wäre ein `SELECT ... WHERE next_check_at <= now` und später, nach dem Check, ein `UPDATE`. Das hat einen Fehler: Dauert ein Check länger als ein Tick, findet der nächste Tick dasselbe Ziel noch einmal als fällig und startet einen zweiten Check. Mit einem Takt von fünf Sekunden und einem Check, der zwölf Sekunden braucht:

```text
ohne Claim: SELECT im Tick, UPDATE erst nach dem Check
  0 s  Tick 1   SELECT → Ziel 7 fällig   ── Check A läuft ────────────► 12 s UPDATE
  5 s  Tick 2   SELECT → Ziel 7 fällig   ── Check B läuft (doppelt) ──► 17 s UPDATE
 10 s  Tick 3   SELECT → Ziel 7 fällig   ── Check C läuft (dreifach) ─► 22 s UPDATE

mit ClaimDue: SELECT und UPDATE in einer Transaktion
  0 s  Tick 1   SELECT → Ziel 7 fällig, UPDATE next_check_at = 60 s ── Check läuft ─► 12 s
  5 s  Tick 2   SELECT → nichts fällig
 10 s  Tick 3   SELECT → nichts fällig
```

Deshalb heißt die Methode `ClaimDue` und nicht `SelectDue`: Sie wählt die fälligen Ziele aus **und** schiebt in derselben Transaktion deren nächsten Termin um ein Intervall nach vorn. Was zurückkommt, gehört diesem Aufrufer. Der Code in drei Schritten, zuerst Transaktion und `SELECT`:

```go
func (s *Store) ClaimDue(ctx context.Context, now time.Time, limit int) ([]monitor.Target, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() // no-op after Commit

	// Step 1: read the due targets, inside the transaction.
	rows, err := tx.QueryContext(ctx,
		`SELECT id, url, interval_seconds, created_at FROM targets
		 WHERE next_check_at <= ? ORDER BY next_check_at LIMIT ?`,
		now.UnixMilli(), limit)
	if err != nil {
		return nil, fmt.Errorf("select due: %w", err)
	}
	var due []monitor.Target
	for rows.Next() {
		t, err := scanTarget(rows)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan due: %w", err)
		}
		due = append(due, t)
	}
	rows.Close() // free the connection before the next statement
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("select due: %w", err)
	}
```

Dann das `UPDATE` als Prepared Statement, einmal vorbereitet und für jedes Ziel mit anderen Parametern ausgeführt:

```go
	// Step 2: move next_check_at forward, so the next tick skips them.
	stmt, err := tx.PrepareContext(ctx, `UPDATE targets SET next_check_at = ? WHERE id = ?`)
	if err != nil {
		return nil, fmt.Errorf("prepare update: %w", err)
	}
	defer stmt.Close()
	for _, t := range due {
		if _, err := stmt.ExecContext(ctx, now.Add(t.Interval).UnixMilli(), t.ID); err != nil {
			return nil, fmt.Errorf("schedule target %d: %w", t.ID, err)
		}
	}
```

Zum Schluss der Commit. Erst danach gehören die Ziele dem Aufrufer:

```go
	// Step 3: make both steps visible at once.
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return due, nil
}
```

Die Kommentare sind nur hier zur Orientierung ergänzt; im Repository steht die Funktion ohne sie am Stück. Vier Regeln stecken in diesem Code:

**`defer tx.Rollback()` direkt nach `BeginTx`.** Das ist das Standardmuster in Go. Jeder frühe `return` wegen eines Fehlers rollt automatisch zurück. Nach einem erfolgreichen `Commit` ist der Rollback harmlos: Er gibt `sql.ErrTxDone` zurück, und der wird ignoriert. In C# erledigt das `using var tx = ...` mit `Dispose`, in Java ein `rollback()` im `catch` oder `finally`, hier ist es ein `defer`. Vergisst du den Rollback, bleibt die Transaktion offen, hält ihre Verbindung und bei SQLite die Schreibsperre – bis der Pool irgendwann leer ist.

**Alle Statements über `tx`, nicht über `s.db`.** Eine Transaktion ist an genau eine Verbindung gebunden. Ein `s.db.ExecContext` innerhalb der Funktion würde sich eine andere Verbindung aus dem Pool holen und außerhalb der Transaktion laufen – bei SQLite mit `_txlock=immediate` sogar auf die eigene Schreibsperre warten.

**`rows.Close()` vor dem nächsten Statement.** Weil die Transaktion nur eine Verbindung hat, muss das Ergebnis vollständig gelesen und geschlossen sein, bevor dieselbe Verbindung das `UPDATE` ausführt. Deshalb steht hier kein `defer rows.Close()` – das würde erst am Ende der Funktion greifen –, sondern ein explizites `Close` im Fehlerpfad und nach der Schleife.

**Prepared Statement.** Das `UPDATE` wird einmal vorbereitet und dann für jedes fällige Ziel mit anderen Parametern ausgeführt. Die Datenbank muss das SQL nur einmal parsen. `stmt.Close` gibt die Ressourcen wieder frei; ein Statement, das über `tx.PrepareContext` entsteht, gehört zur Transaktion und wird mit ihr ungültig. Prepared Statements sind in Go eine Optimierung für Wiederholung, keine Sicherheitsmaßnahme – die Sicherheit kommt von den Platzhaltern, und die funktionieren auch ohne explizites `Prepare`.

Durch `_txlock=immediate` laufen zwei gleichzeitige `ClaimDue`-Aufrufe sauber hintereinander: Der zweite wartet beim `BEGIN`, bis der erste committet hat, und sieht danach die schon verschobenen Termine.

## Zeitstempel als Unix-Millisekunden

SQLite hat keinen Datumstyp. Man kann Zeiten als Text speichern (der Treiber hat dafür eigene Formatoptionen) oder als Zahl. upwatch speichert Millisekunden seit 1970 in UTC als `INTEGER`:

```sql
created_at       INTEGER NOT NULL, -- unix milliseconds, UTC
next_check_at    INTEGER NOT NULL  -- unix milliseconds, UTC
```

Das hat drei Vorteile. Zahlen vergleichen und sortieren korrekt, also funktioniert `WHERE next_check_at <= ?` mit dem Index `targets_next_check_at`. Es gibt keine Zeitzonen-Mehrdeutigkeit. Und das Format hängt nicht vom Treiber ab.

In Go wird an der Grenze umgerechnet: `now.UnixMilli()` beim Schreiben, `time.UnixMilli(createdAt).UTC()` beim Lesen. `CreateTarget` schneidet die zurückgegebene Zeit mit `now.Truncate(time.Millisecond)` ab. Dadurch ist das zurückgegebene `Target` exakt gleich dem, was ein späteres `Target(id)` liest – der Test in [store_test.go](../internal/sqlitestore/store_test.go) vergleicht die beiden Structs mit `!=`.

Bei PostgreSQL würdest du dagegen `timestamptz` nehmen und `time.Time` direkt scannen; der Treiber erledigt die Umrechnung.

## Jetzt du: in die Datenbank schauen

Mit dem `sqlite3`-Kommandozeilenwerkzeug (auf macOS vorinstalliert, unter Linux meist als Paket `sqlite3`) siehst du, was upwatch wirklich speichert. Starte die langsame Website aus Teil 06 mit kurzer Antwortzeit und upwatch mit frischer Datenbank:

```bash
go run ./beispiele/06-concurrency/langsam -delay 50ms
```

```bash
export UPWATCH_DB="$(mktemp -d)/upwatch.db"
UPWATCH_TICK=1s UPWATCH_ALLOW_PRIVATE_TARGETS=true UPWATCH_ADDR=localhost:8090 go run ./cmd/upwatch
```

Lege in einem dritten Terminal (dort ebenfalls `UPWATCH_DB` exportieren) ein Ziel mit zehn Sekunden Intervall an und schau nach ein paar Sekunden in die Datei:

```bash
curl -s -X POST localhost:8090/targets -d '{"url":"http://127.0.0.1:18101/ok","interval_seconds":10}'
sqlite3 "$UPWATCH_DB" '.tables'
sqlite3 "$UPWATCH_DB" 'SELECT * FROM targets; SELECT * FROM results;'
sqlite3 "$UPWATCH_DB" "SELECT url, datetime(next_check_at/1000, 'unixepoch') FROM targets;"
sqlite3 "$UPWATCH_DB" 'PRAGMA journal_mode;'
```

```text
results     schema_migrations     targets
1|http://127.0.0.1:18101/ok|10|1791667164003|1791667174998
1|1|200|62||1791667164999
http://127.0.0.1:18101/ok|2026-10-10 21:19:34
wal
```

Die Zeitstempel sind die Millisekunden von oben. `next_check_at` liegt knapp zehn Sekunden nach dem Check: Das hat `ClaimDue` beim Beanspruchen eingetragen. `datetime(…/1000, 'unixepoch')` macht daraus lesbare UTC-Zeit. Und `wal` bestätigt, dass das `_pragma` aus dem DSN gegriffen hat; neben `upwatch.db` liegen deshalb die Dateien `upwatch.db-wal` und `upwatch.db-shm`. Ohne `sqlite3` zeigt dir `go run ./beispiele/10-datenbanken/sqlite` weiter unten dieselben Muster in einem Programm.

## Migrationen mit embed

Das Schema liegt als SQL-Datei in [migrations/001_init.sql](../internal/sqlitestore/migrations/001_init.sql) und wird mit `embed` ins Binary kompiliert:

```go
//go:embed migrations/*.sql
var migrations embed.FS
```

Die Direktive `//go:embed` steht direkt über der Variablen und legt zur Build-Zeit alle passenden Dateien in ein schreibgeschütztes Dateisystem. Das Binary braucht zur Laufzeit keinen `migrations`-Ordner; es bleibt eine einzelne Datei.

`migrate` legt eine Tabelle `schema_migrations` an, sortiert die Dateinamen und wendet jede noch nicht gelaufene Datei in einer eigenen Transaktion an:

```go
func (s *Store) applyMigration(ctx context.Context, name string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after a successful Commit

	var done int
	err = tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, name).Scan(&done)
	// ...
	body, err := migrations.ReadFile(name)
	// ...
	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (name) VALUES (?)`, name); err != nil {
		return err
	}
	return tx.Commit()
}
```

Weil DDL in SQLite transaktional ist, wird eine fehlerhafte Migration vollständig zurückgerollt, und der Eintrag in `schema_migrations` entsteht nur bei Erfolg. Mehrere Statements in einem `ExecContext` funktionieren mit diesem Treiber; das ist nicht bei jedem Treiber so.

Das sind rund 50 Zeilen, und für upwatch reichen sie. Was fehlt: Down-Migrationen, Prüfsummen gegen nachträglich geänderte Dateien, Sperren gegen mehrere Instanzen, die gleichzeitig migrieren, und ein Kommandozeilenwerkzeug. Wenn du das brauchst, gibt es etablierte Bibliotheken:

- **goose** (`github.com/pressly/goose`): SQL- oder Go-Migrationen, unterstützt `embed.FS` direkt, als Bibliothek und als CLI nutzbar. Am nächsten an der Lösung von upwatch.
- **golang-migrate** (`github.com/golang-migrate/migrate`): Up- und Down-Dateien, viele Datenbanken und Quellen, verbreitet in CI-Pipelines.
- **Atlas** (`atlasgo.io`): Deklarativ – du beschreibst das gewünschte Schema, Atlas berechnet die Änderungen. Vergleichbar mit dem Diff-Ansatz von EF-Core-Migrationen oder Prisma Migrate.

## SQL Injection: Platzhalter statt fmt.Sprintf

Das Negativbeispiel sieht harmlos aus:

```go
// Never do this.
q := fmt.Sprintf(`SELECT COUNT(*) FROM targets WHERE url = '%s'`, url)
err := db.QueryRowContext(ctx, q).Scan(&n)
```

Mit der Eingabe `x' OR '1'='1` wird daraus `WHERE url = 'x' OR '1'='1'`, und die Bedingung ist für jede Zeile wahr. Mit Platzhalter:

```go
err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM targets WHERE url = ?`, url).Scan(&n)
```

Hier wird der Wert getrennt vom SQL-Text an die Datenbank übergeben und nie als SQL interpretiert. Das Beispiel unten führt beide Varianten mit derselben Eingabe aus.

Platzhalter funktionieren nur für Werte, nicht für Tabellen- oder Spaltennamen. Brauchst du einen dynamischen Spaltennamen, etwa für eine Sortierung, nimm ihn aus einer festen Liste (`switch sort { case "url": ... }`) und niemals direkt aus der Eingabe.

## Beispiel: database/sql mit SQLite im Speicher

Das Beispiel [beispiele/10-datenbanken/sqlite](../beispiele/10-datenbanken/sqlite/main.go) zeigt die Muster dieses Kapitels in einem Programm: Pool, `QueryRowContext` mit `ErrNoRows`, `rows.Err`, Unique-Constraint per `errors.As`, eine Transaktion, die zurückgerollt wird, und den Injection-Vergleich.

```bash
go run ./beispiele/10-datenbanken/sqlite
```

```
created  1 https://example.com
created  2 https://go.dev
created  3 https://example.org
again    errors.Is(err, ErrDuplicate) = true (target https://go.dev: already exists)
found    2 https://go.dev
missing  errors.Is(err, ErrNotFound) = true (target 99: not found)
all      3 targets
tx fail  insert result: constraint failed: CHECK constraint failed: status_code BETWEEN 100 AND 599 (275)
all      3 targets after rollback
inject   Sprintf: 3 rows, placeholder: 0 rows
```

Die Datenbank ist `":memory:"`, und das Beispiel ruft `db.SetMaxOpenConns(1)` auf. Der Grund: Jede neue Verbindung zu `":memory:"` bekommt eine eigene, leere Datenbank. Öffnet der Pool eine zweite Verbindung – etwa weil die erste gerade in einer Transaktion steckt –, sieht sie die Tabellen der ersten nicht, und du bekommst „no such table“. Solange alles nacheinander läuft, fällt das nicht auf, weil der Pool die eine freie Verbindung wiederverwendet; der Fehler kommt erst mit Nebenläufigkeit oder Transaktionen. Mit genau einer Verbindung gibt der Pool immer dieselbe heraus. Die Kehrseite: Während eine Transaktion läuft, ist diese eine Verbindung belegt, und ein `db.QueryRowContext` außerhalb der Transaktion wartet, bis sein Context abläuft – ohne Context für immer. Für Tests mit Dateien ist `t.TempDir()` deshalb oft die bessere Wahl (Teil 11).

## sqlc, ORMs und wann sie sich lohnen

`database/sql` verlangt Handarbeit: SQL schreiben, Spalten in der richtigen Reihenfolge scannen, Zeilen in Structs umwandeln. Bei sieben Abfragen wie in upwatch ist das überschaubar. Bei fünfzig wird es fehleranfällig, weil Spaltenliste und `Scan`-Aufruf auseinanderlaufen können, ohne dass der Compiler es merkt. Drei Richtungen gibt es:

**sqlc** (`sqlc.dev`) dreht das Verhältnis um: Du schreibst SQL-Dateien mit benannten Abfragen und das Schema, sqlc generiert daraus Go-Funktionen mit passenden Parameter- und Ergebnistypen. Fehler im SQL – eine falsche Spalte, ein falscher Typ – fallen beim Generieren auf, nicht zur Laufzeit. Der generierte Code benutzt `database/sql` oder pgx, ist lesbar und hat keine Laufzeit-Abhängigkeit. Kosten: ein Codegenerierungsschritt im Build, und dynamische Abfragen (optionale Filter) sind umständlich. Für Teams, die SQL mögen, ist das oft die beste Ergänzung.

**GORM** ist das bekannteste ORM für Go, im Stil von Active Record: Structs mit Tags, `db.Where(...).Find(&targets)`, Hooks, Assoziationen, Auto-Migration. Es spart viel Tipparbeit bei CRUD-lastigen Anwendungen. Der Preis ist der gleiche wie bei jedem ORM, nur deutlicher, weil Go keine LINQ-artige Typprüfung hat: Viele Fehler zeigen sich erst zur Laufzeit, die erzeugten Abfragen sind nicht immer offensichtlich, und Reflection kostet Leistung. Wer von Entity Framework kommt, findet sich schnell zurecht, vermisst aber die Compile-Zeit-Sicherheit von LINQ.

**ent** (`entgo.io`) beschreibt das Schema als Go-Code und generiert daraus einen typsicheren Client mit Abfragen über Beziehungen hinweg. Es liegt zwischen sqlc und GORM: typsicher wie sqlc, abstrakt wie ein ORM, dafür mit einem eigenen Modell, das man lernen muss.

Daneben gibt es dünne Helfer wie `sqlx` oder `scany`, die nur das Scannen in Structs übernehmen und sonst `database/sql` bleiben.

Eine pragmatische Reihenfolge: Fang mit `database/sql` an, wie upwatch. Wenn die Zahl der Abfragen wächst, ist sqlc der kleinste Schritt, weil du dein SQL behältst. Ein ORM lohnt sich, wenn die Anwendung überwiegend aus CRUD über viele Tabellen besteht und das Team die Abstraktion bewusst will.

## Im Projekt

- [store.go](../internal/sqlitestore/store.go) – `Open` mit DSN und Pool, `migrate`, `ClaimDue`, `isUniqueViolation`, `scanTarget`.
- [001_init.sql](../internal/sqlitestore/migrations/001_init.sql) – Schema mit `UNIQUE`, `ON DELETE CASCADE`, Indizes und Unix-Millisekunden.
- [store_test.go](../internal/sqlitestore/store_test.go) – Integrationstests gegen echte SQLite-Dateien in `t.TempDir()`.
- [httpapi.go](../internal/httpapi/httpapi.go) – `handleError` übersetzt `ErrNotFound` und `ErrDuplicate` in 404 und 409.

## Übungen

1. **Aufwärmen:** Lass `sqlite3 "$UPWATCH_DB" .schema` laufen und finde im Schema den Index, den `ClaimDue` für `WHERE next_check_at <= ?` nutzt. Lösche dann per `curl -X DELETE` ein Ziel mit Ergebnissen und prüfe mit `SELECT COUNT(*) FROM results`, dass `ON DELETE CASCADE` gegriffen hat.
2. **Kern:** Füge im Beispiel in `addWithResult` direkt nach `LastInsertId` eine Abfrage über `db` statt über `tx` ein, etwa `db.QueryRowContext(ctx, "SELECT COUNT(*) FROM targets").Scan(&n)`. Führe das Programm einmal so aus und einmal zusätzlich ohne `db.SetMaxOpenConns(1)`. Erkläre beide Ergebnisse – das eine hängt, bis der Context nach zehn Sekunden abläuft, das andere meldet „no such table“.
3. **Kern:** Schreib für upwatch eine Methode `(*Store).UpdateInterval(ctx, id int64, interval time.Duration) error`, die `monitor.ErrNotFound` zurückgibt, wenn es das Ziel nicht gibt, und ergänze einen Test in `store_test.go`. Füge danach eine zweite Migration `002_target_name.sql` hinzu, die eine Spalte `name TEXT NOT NULL DEFAULT ''` anlegt, und prüfe mit `TestMigrateIsIdempotent`, dass ein zweiter Start sie nicht erneut ausführt.
4. **Profi:** Schreib einen Test, der zwei Goroutines gleichzeitig `ClaimDue` für dasselbe fällige Ziel aufrufen lässt, und prüfe, dass genau eine es bekommt. Was passiert, wenn du `_txlock=immediate` testweise entfernst?

---

← [Teil 09: Projektstruktur und Architektur](09-projektstruktur.md) · [Übersicht](../README.md) · [Teil 11: Testing](11-testing.md) →
