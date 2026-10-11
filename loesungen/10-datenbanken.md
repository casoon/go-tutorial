# Lösungen zu Teil 10: Datenbanken mit database/sql

Zu den Aufgaben: [Teil 10](../kapitel/10-datenbanken.md#übungen)

## Übung 1: Index finden und ON DELETE CASCADE prüfen (Aufwärmen)

Für den Lauf reicht upwatch allein: Als Ziel dient sein eigenes `/healthz`, dann braucht es keine zweite Website. Gebaut wird ein Binary, damit `kill` den richtigen Prozess trifft:

```bash
DIR=$(mktemp -d)
go build -o "$DIR/upwatch" ./cmd/upwatch
export UPWATCH_DB="$DIR/upwatch.db"
UPWATCH_ADDR=127.0.0.1:18300 UPWATCH_TICK=1s UPWATCH_ALLOW_PRIVATE_TARGETS=true "$DIR/upwatch" &
PID=$!
curl -s -X POST localhost:18300/targets -d '{"url":"http://127.0.0.1:18300/healthz","interval_seconds":10}'
sleep 2
sqlite3 "$UPWATCH_DB" .schema
```

```text
CREATE TABLE schema_migrations (name TEXT PRIMARY KEY);
CREATE TABLE targets (
    id               INTEGER PRIMARY KEY,
    url              TEXT    NOT NULL UNIQUE,
    interval_seconds INTEGER NOT NULL,
    created_at       INTEGER NOT NULL, -- unix milliseconds, UTC
    next_check_at    INTEGER NOT NULL  -- unix milliseconds, UTC
);
CREATE INDEX targets_next_check_at ON targets (next_check_at);
CREATE TABLE results (
    ...
);
CREATE INDEX results_target_checked ON results (target_id, checked_at DESC);
```

Der gesuchte Index ist `targets_next_check_at`. Dass SQLite ihn für die Abfrage aus `ClaimDue` tatsächlich benutzt, zeigt `EXPLAIN QUERY PLAN`:

```bash
sqlite3 "$UPWATCH_DB" "EXPLAIN QUERY PLAN SELECT id, url, interval_seconds, created_at FROM targets WHERE next_check_at <= 0 ORDER BY next_check_at LIMIT 10"
```

```text
QUERY PLAN
`--SEARCH targets USING INDEX targets_next_check_at (next_check_at<?)
```

Er deckt `WHERE` und `ORDER BY` gleichzeitig ab: Die Zeilen kommen schon sortiert aus dem Index, ein eigener Sortierschritt (`USE TEMP B-TREE FOR ORDER BY`) taucht im Plan nicht auf. Dann das Löschen:

```bash
sqlite3 "$UPWATCH_DB" 'SELECT COUNT(*) FROM results'    # 1
curl -s -i -X DELETE localhost:18300/targets/1 | head -1  # HTTP/1.1 204 No Content
sqlite3 "$UPWATCH_DB" 'SELECT COUNT(*) FROM results'    # 0
kill $PID
```

Das Ergebnis des Checks ist mit dem Ziel verschwunden, `ON DELETE CASCADE` hat gegriffen.

Ein Gegenversuch zeigt, warum das `_pragma` im DSN wichtig ist. Löscht man das Ziel nicht über die API, sondern direkt mit dem `sqlite3`-Werkzeug, bleibt das Ergebnis liegen:

```bash
sqlite3 "$UPWATCH_DB" 'SELECT COUNT(*) FROM results; DELETE FROM targets; SELECT COUNT(*) FROM results;'
sqlite3 "$UPWATCH_DB" 'PRAGMA foreign_keys;'
```

```text
1
1
0
```

**Warum so:** `foreign_keys` ist in SQLite eine Einstellung pro Verbindung und standardmäßig aus. upwatch setzt sie für jede Verbindung seines Pools, das `sqlite3`-Werkzeug tut es nicht – dort wird der Fremdschlüssel weder geprüft noch kaskadiert. Für die Praxis heißt das: Wer neben der Anwendung mit einem anderen Werkzeug in die Datenbank schreibt, muss `PRAGMA foreign_keys = ON;` selbst setzen. `EXPLAIN QUERY PLAN` ist die schnellste Art, eine Index-Vermutung zu prüfen, statt sie aus dem Schema abzuleiten.

## Übung 2: Abfrage über db statt über tx (Kern)

Das ist ein Experiment, deshalb gibt es keinen Patch. Die Änderung im Beispiel, direkt nach `LastInsertId` in `addWithResult`:

```go
var n int
if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM targets").Scan(&n); err != nil {
	return fmt.Errorf("count outside tx: %w", err)
}
```

Mit `db.SetMaxOpenConns(1)` hängt das Programm zehn Sekunden und bricht dann ab:

```text
all      3 targets
tx fail  count outside tx: context deadline exceeded
2026/10/11 11:40:13 context deadline exceeded
exit status 1
```

`time` meldet für den Lauf rund 10,4 Sekunden. Die Transaktion hält die einzige Verbindung des Pools. `db.QueryRowContext` wartet auf eine freie Verbindung, die erst frei wird, wenn die Transaktion endet – und die endet erst, wenn `addWithResult` zurückkehrt, also nach der Abfrage. Das ist ein Deadlock innerhalb einer einzigen Goroutine, den nur der Context nach zehn Sekunden auflöst. Weil `main` denselben, jetzt abgelaufenen Context weiterverwendet, scheitert auch das folgende `count` und `log.Fatal` beendet das Programm.

Ohne `SetMaxOpenConns(1)` kommt die Antwort sofort, aber falsch:

```text
tx fail  count outside tx: SQL logic error: no such table: targets (1)
all      3 targets after rollback
inject   Sprintf: 3 rows, placeholder: 0 rows
```

Jetzt darf der Pool eine zweite Verbindung öffnen. Jede Verbindung zu `":memory:"` bekommt aber ihre eigene, leere Datenbank, in der es die Tabelle `targets` nicht gibt. Die Transaktion wird mit dem Fehler zurückgerollt, und der Rest des Programms läuft wieder über die erste Verbindung, die der Pool wiederverwendet.

**Warum so:** Beide Ergebnisse haben dieselbe Ursache – ein Statement, das außerhalb der Transaktion läuft, holt sich eine *andere* Verbindung aus dem Pool. Mit einer Verbindung gibt es keine andere, also wartet es; mit mehreren bekommt es eine, die etwas anderes sieht. Bei einer Datei-Datenbank im WAL-Modus würde die zweite Verbindung übrigens die Tabelle finden, aber die noch nicht committete Zeile der Transaktion nicht mitzählen. Der Fehler fällt in jeder Variante anders auf, und die Regel aus dem Kapitel bleibt dieselbe: Innerhalb einer Transaktion läuft jedes Statement über `tx`.

## Übung 3: UpdateInterval und Migration 002 (Kern)

```go
// UpdateInterval changes how often a target is checked. The next check
// stays where it is; the new interval applies from the following claim on.
// It returns monitor.ErrNotFound if the target does not exist.
func (s *Store) UpdateInterval(ctx context.Context, id int64, interval time.Duration) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE targets SET interval_seconds = ? WHERE id = ?`,
		int64(interval/time.Second), id)
	if err != nil {
		return fmt.Errorf("update interval of target %d: %w", id, err)
	}
	// SQLite counts matched rows, so setting the same interval again
	// still reports 1. MySQL by default counts changed rows only.
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update interval of target %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("target %d: %w", id, monitor.ErrNotFound)
	}
	return nil
}
```

Der Test prüft drei Fälle: Das neue Intervall kommt über `Target` zurück, dasselbe Intervall ein zweites Mal zu setzen ist kein Fehler, und eine unbekannte ID liefert `ErrNotFound`.

Die Migration ist eine neue Datei neben `001_init.sql`; die bestehende bleibt unverändert:

```sql
-- A display name for targets. Existing rows get the empty string.
ALTER TABLE targets ADD COLUMN name TEXT NOT NULL DEFAULT '';
```

`TestMigrateIsIdempotent` öffnet dieselbe Datei zweimal und bleibt grün. Dass das wirklich etwas beweist, zeigt ein Gegenversuch: Löscht man in einem Wegwerftest die Zeile `migrations/002_target_name.sql` aus `schema_migrations` und öffnet die Datenbank erneut, läuft die Migration ein zweites Mal und scheitert:

```text
migration migrations/002_target_name.sql: SQL logic error: duplicate column name: name (1)
```

Würde `migrate` also eine schon gelaufene Datei nicht überspringen, schlüge `TestMigrateIsIdempotent` mit genau dieser Meldung fehl. Zusätzlich prüft ein neuer Test `TestMigrationAddsName`, dass die Spalte existiert, für vorhandene Zeilen `''` enthält und `schema_migrations` zwei Einträge hat.

```bash
git apply loesungen/10-datenbanken/uebung-3.patch
go test ./...
```

**Warum so:** Die Methode übersetzt „keine Zeile getroffen“ in `monitor.ErrNotFound`, genau wie `DeleteTarget`; so bleibt `httpapi` frei von `database/sql`. Sie validiert das Intervall nicht – die Grenzen gehören in die Domäne (`monitor.MinInterval`, `MaxInterval`), und ein künftiger `PATCH`-Handler würde sie dort prüfen, bevor er den Store aufruft. Offen ist eine fachliche Entscheidung: Soll ein kürzeres Intervall sofort wirken? Dann setzt das `UPDATE` zusätzlich `next_check_at` neu; die Lösung lässt den geplanten Check unverändert, weil das am wenigsten überrascht. Die neue Spalte stört keinen bestehenden Code, weil jedes `SELECT` in upwatch seine Spalten ausdrücklich nennt – mit `SELECT *` würde `scanTarget` plötzlich fünf statt vier Werte bekommen und scheitern.

## Übung 4: Zwei gleichzeitige ClaimDue-Aufrufe (Profi)

Der Test lässt zwei Goroutinen an einem geschlossenen Channel starten, damit sie möglichst gleichzeitig `BEGIN` erreichen, und wiederholt das in 20 Runden mit je einem neuen fälligen Ziel:

```go
for round := range 20 {
	target := mustCreate(t, s, fmt.Sprintf("https://example.com/%d", round), time.Minute)
	now := time.Now().Add(time.Second)

	start := make(chan struct{})
	var (
		wg      sync.WaitGroup
		claimed [2][]monitor.Target
		errs    [2]error
	)
	for i := range 2 {
		wg.Go(func() {
			<-start
			claimed[i], errs[i] = s.ClaimDue(ctx, now, 10)
		})
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("round %d: ClaimDue in goroutine %d: %v", round, i, err)
		}
	}
	if n := len(claimed[0]) + len(claimed[1]); n != 1 {
		t.Fatalf("round %d: target %d claimed %d times, want exactly once (got %v and %v)",
			round, target.ID, n, claimed[0], claimed[1])
	}
}
```

```bash
git apply loesungen/10-datenbanken/uebung-4.patch
go test -race -count=5 -run TestClaimDueConcurrent -v ./internal/sqlitestore
```

Alle fünf Läufe sind grün. Kommentiert man in `Open` die Zeile `q.Set("_txlock", "immediate")` aus, scheitert jeder Lauf nach wenigen Millisekunden:

```text
--- FAIL: TestClaimDueConcurrent (0.00s)
    store_test.go:129: round 1: ClaimDue in goroutine 0: schedule target 2: database is locked (5) (SQLITE_BUSY)
```

Bemerkenswert ist, *was* passiert: Das Ziel wird nicht doppelt beansprucht, sondern ein Aufruf bricht mit `SQLITE_BUSY` ab, und zwar beim `UPDATE` („schedule target“), ohne die fünf Sekunden von `busy_timeout` abzuwarten. Ohne `immediate` beginnen beide Transaktionen als Leser und lesen dasselbe fällige Ziel. Die erste holt sich beim `UPDATE` die Schreibsperre und committet. Die zweite will danach ebenfalls schreiben, aber ihr Lesestand ist inzwischen veraltet; SQLite kann sie nicht mehr sicher zum Schreiber machen und bricht sofort ab, weil Warten daran nichts ändern würde.

**Warum so:** Ein einzelner Lauf mit zwei Goroutinen ist kein guter Test für eine Race, weil die beiden Aufrufe zufällig nacheinander laufen können; die Startschranke und die Runden erhöhen die Chance auf echte Überlappung, und der Gegenversuch zeigt, dass der Test den Fehler tatsächlich findet. Geprüft wird das Ergebnis („genau einmal, kein Fehler“), nicht die Reihenfolge. In upwatch ruft heute nur ein Scheduler `ClaimDue` auf; gleichzeitige Aufrufe entstehen erst mit einer zweiten Instanz auf derselben Datei oder einem zweiten Aufrufer im Prozess. Ohne `immediate` wäre das nicht katastrophal – der Scheduler loggt den Fehler und versucht es beim nächsten Tick –, aber jeder Konflikt kostet einen Tick. Bei PostgreSQL löst man dasselbe Problem mit `SELECT … FOR UPDATE SKIP LOCKED`, das gesperrte Zeilen überspringt, statt zu warten.
