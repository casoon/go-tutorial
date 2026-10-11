# Lösungen zu Teil 16: Go Best Practices für Umsteiger

Zu den Aufgaben: [Teil 16](../kapitel/16-best-practices.md#übungen)

## Übung 1: Ein eigenes Projekt durchgehen (Aufwärmen)

Diese Übung hat keine Musterlösung im engeren Sinn, weil sie auf deinem Code läuft. Was sich zeigen lässt, ist ein Vorgehen. Einen Teil der Tabelle findest du mechanisch, mit Suchmustern, die grob, aber schnell sind:

```bash
grep -rn --include='*.go' -E '^func init\(\)' .                              # init magic
grep -rnE --include='*.go' '^func \([a-z]+ \*?[A-Za-z]+\) Get[A-Z]' .        # getters
grep -rnE --include='*.go' '^\s+[a-z]\w*\s+context\.Context$' .              # ctx in a struct
grep -rnE --include='*.go' '^\s*go (func|[a-zA-Z_.]+\()' .                   # every go statement
grep -rn  --include='*.go' 'panic(' . | grep -v _test.go                     # panic as control flow
go list ./... | grep -E '/(util|utils|common|helpers?|shared)$'               # util packages
grep -rnE --include='*.go' '^type \w+ interface' .                           # where interfaces live
```

Zur Probe auf `internal/`, `cmd/` und `beispiele/` dieses Repositorys angewendet: kein `init`, kein Getter, kein Context im Struct, kein util-Package. Das einzige `panic(` außerhalb von Tests ist das Weiterwerfen von `http.ErrAbortHandler` in `recoverPanics`. Die Interfaces stehen beim Verbraucher (`Store` und `Checker` in `scheduler`, `Store` in `httpapi`) oder sind klein und unexportiert (`scanner` in `sqlitestore`). Die `go`-Anweisungen:

```
internal/scheduler/scheduler.go:73:	go func() {
cmd/upwatch/main.go:83:		go serveDebug(cfg.DebugAddr, logger)
cmd/upwatch/main.go:87:	go func() {
beispiele/06-concurrency/leak/main.go:19:		go func() { ch <- fetch(u) }()
beispiele/06-concurrency/leak/main.go:29:		go func() { ch <- fetch(u) }()
…
```

Hier hört die Mechanik auf, denn jede Fundstelle braucht die Frage aus dem Kapitel: Wie endet diese Goroutine, und wer wartet auf sie? In `scheduler.go:73` ist es der Writer, auf den `Run` über `writerDone` wartet; `main.go:87` endet mit `ListenAndServe` und meldet sich über `serveErr`; `serveDebug` ist die im Kapitel begründete Ausnahme; die Treffer in `beispiele/06-concurrency/leak` sind das absichtliche Leak-Beispiel aus Teil 06. Für Muster wie „großes Interface beim Anbieter“, „Pointer überall“ oder „Generics zu früh“ gibt es keine verlässliche Suche, die bleiben Lesearbeit.

**Warum so:** Die Suche liefert Kandidaten, keine Urteile – ein `init`, das einen `database/sql`-Treiber registriert, ist in Ordnung, ein Interface beim Anbieter kann eine bewusste Plugin-Schnittstelle sein. Notiere deshalb bei jedem Fund, woher das Muster kommt (etwa „`IUserRepository` neben einer Implementierung: ASP.NET-Core-DI“) und ob der ursprüngliche Grund in Go noch existiert. Fehlt der Grund, ist der Fund ein Kandidat zum Vereinfachen; existiert er, ist es eine begründete Abweichung, die einen Kommentar verdient.

## Übung 2: Ein großes StoreInterface beim Anbieter (Kern)

Kein Patch, weil die Aufgabe das Rückgängigmachen verlangt. Das Experiment lief in einem temporären Worktree: In `sqlitestore` ein `StoreInterface` mit allen neun Methoden von `*Store` (inklusive `Close`, `ClaimDue`, `SaveResult`), in `httpapi` das eigene `Store`-Interface durch `type Store = sqlitestore.StoreInterface` ersetzt.

Das Ergebnis ist zuerst überraschend:

```
$ go vet ./internal/... && go test ./internal/httpapi
ok  	github.com/casoon/go-tutorial/internal/httpapi	0.292s
```

An `httpapi_test.go` ändert sich nichts, auch nicht an `failingStore`. Der Grund ist das Embedding: `type failingStore struct{ Store }` übernimmt die Methodenmenge des eingebetteten Interfaces, egal wie groß es ist. Der Fake erfüllt das neue Interface automatisch – mit einem `nil`-Wert dahinter, sodass jeder Aufruf einer nicht überschriebenen Methode panict. Genau darauf baut `TestPanicBecomes500` schon heute.

Sichtbar wird der Unterschied bei einem Fake, der seine Methoden selbst schreibt, etwa einem In-Memory-Store mit genau den sechs Methoden, die die Handler aufrufen:

```
vet: internal/httpapi/fake_test.go:22:15: cannot use memStore{} (value of struct type memStore) as Store value in variable declaration: memStore does not implement sqlitestore.StoreInterface (missing method ClaimDue)
```

Mit dem Verbraucher-Interface kompiliert derselbe Fake. Mit dem Anbieter-Interface muss er `ClaimDue`, `SaveResult` und `Close` implementieren, Methoden, die kein Handler je aufruft. Dazu kommt eine Änderung, die man in den Tests nicht sieht: `httpapi` importiert jetzt `sqlitestore` und damit den SQLite-Treiber. `go list -deps ./internal/httpapi` zählte vorher 201 Packages, danach 250.

**Warum so:** Das Embedding im Test kaschiert das Problem, statt es zu lösen: Der Compiler prüft nicht mehr, welche Methoden die API tatsächlich braucht, und ein Fake mit drei überschriebenen Methoden kann zur Laufzeit an einer vierten mit einem nil-Pointer scheitern. Das kleine Interface beim Verbraucher ist Dokumentation, die der Compiler prüft – es sagt, dass die API nie `ClaimDue` aufruft. Und die Importrichtung kehrt sich um: Mit dem Verbraucher-Interface weiß `httpapi` nichts von SQLite, mit dem Anbieter-Interface hängt die HTTP-Schicht am Treiber, und ein `pgstore` müsste sich nach einem Interface richten, das in `sqlitestore` definiert ist.

## Übung 3: Eine Erweiterung umsetzen (Profi)

Umgesetzt ist **Retention**, weil sie mehrere Packages berührt, aber ohne neue Abhängigkeit und ohne neue Angriffsfläche auskommt. Die übrigen vier Erweiterungen sind unten als Lösungsweg skizziert.

### Retention, vollständig

Vier Teile:

```sql
-- internal/sqlitestore/migrations/002_results_checked_at.sql
-- Retention deletes by age across all targets. The index on
-- (target_id, checked_at) cannot serve that range scan.
CREATE INDEX results_checked_at ON results (checked_at);
```

```go
// internal/sqlitestore/store.go
func (s *Store) DeleteResultsBefore(ctx context.Context, before time.Time, batch int) (int64, error) {
	var total int64
	for {
		res, err := s.db.ExecContext(ctx,
			`DELETE FROM results WHERE id IN (
			   SELECT id FROM results WHERE checked_at < ? LIMIT ?)`,
			before.UnixMilli(), batch)
		if err != nil {
			return total, fmt.Errorf("delete results: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, fmt.Errorf("delete results: %w", err)
		}
		total += n
		if n < int64(batch) {
			return total, nil
		}
	}
}
```

```go
// internal/retention/retention.go
type Store interface {
	DeleteResultsBefore(ctx context.Context, before time.Time, batch int) (int64, error)
}

const batchSize = 1000

func Run(ctx context.Context, store Store, logger *slog.Logger, maxAge, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		n, err := store.DeleteResultsBefore(ctx, time.Now().Add(-maxAge), batchSize)
		switch {
		case err != nil && ctx.Err() == nil:
			logger.Error("delete old results", "err", err)
		case n > 0:
			logger.Info("deleted old results", "count", n, "max_age", maxAge)
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}
```

```go
// cmd/upwatch/main.go
wg.Go(func() { sched.Run(ctx) })
if cfg.Retention > 0 {
	// Part of wg, so store.Close waits for a running delete.
	wg.Go(func() { retention.Run(ctx, store, logger, cfg.Retention, time.Hour) })
}
```

`config.Load` liest `UPWATCH_RETENTION` mit dem vorhandenen Helfer `dur`; nicht gesetzt heißt 0, und 0 heißt „nichts löschen“. Die Tests: `TestDeleteResultsBefore` legt 25 alte und 3 neue Ergebnisse an und löscht mit Batchgröße 10, also in drei Runden; übrig bleiben genau die 3 neuen, ein zweiter Lauf löscht nichts. `TestRunDeletesPeriodically` läuft mit `testing/synctest` und einem Fake: Nach zwei Stunden und einer Minute virtueller Zeit gab es genau drei Aufrufe (Start, nach 1 h, nach 2 h), jeweils mit Stichtag „jetzt minus 24 Stunden“.

Dass der neue Index gebraucht wird, zeigt `EXPLAIN QUERY PLAN` für die Löschabfrage:

```
ohne 002:  `--SCAN results USING COVERING INDEX results_target_checked
mit 002:   `--SEARCH results USING COVERING INDEX results_checked_at (checked_at<?)
```

```bash
git apply loesungen/16-best-practices/uebung-3.patch
go test ./...
```

**Warum so:** Zu den drei Fragen der Aufgabe. *Neues Interface:* ja, `retention.Store` mit einer Methode, beim Verbraucher – aus demselben Grund wie in Übung 2; `*sqlitestore.Store` erfüllt es, ohne davon zu wissen. *Neues Package:* ja, `internal/retention`, weil Aufräumen ein eigener Zweck mit eigenem Takt ist. Im Scheduler hätte es dessen `Store`-Interface um eine Methode erweitert, die mit Checks nichts zu tun hat, und die Tests des Schedulers hätten sie mitfaken müssen. *Neue Abhängigkeit:* nein. Das Löschen in Batches über `id IN (SELECT … LIMIT ?)` hält jede Schreibsperre kurz; weil jedes Statement eine eigene Transaktion ist, kommt der Writer des Schedulers zwischen zwei Batches an die Reihe. Die Goroutine hängt an derselben `WaitGroup` wie der Scheduler und hat damit ein Ende und einen Wartenden – die Regel aus diesem Kapitel. Default „aus“ ist eine Abwägung: Ein Update, das ohne Zutun Daten löscht, überrascht mehr als eine Tabelle, die wächst. Der feste Takt von einer Stunde heißt, dass Ergebnisse bis zu einer Stunde länger liegen als konfiguriert; für eine Retention in Tagen spielt das keine Rolle.

### Benachrichtigungen bei Statuswechsel, skizziert

Den letzten Status braucht der Writer im Scheduler, weil nur er alle Ergebnisse sieht. Ein `map[int64]bool` im Writer genügt zur Laufzeit, geht aber beim Neustart verloren und meldet dann jeden Ausfall erneut; robuster ist eine Spalte `last_up` in `targets`, die `SaveResult` in derselben Transaktion aktualisiert und den alten Wert zurückgibt. Wechsel gehen über einen gepufferten Channel an eine eigene Goroutine (`notifier`), die per HTTP sendet; ist der Puffer voll, wird verworfen und gezählt (expvar), statt den Writer zu blockieren. Der Webhook-Client ist `check.NewClient(false)` mit eigenem Timeout, damit der SSRF-Schutz auch hier greift, und die Webhook-URL wird mit `monitor.NormalizeURL` validiert. Getestet wird mit `synctest`, einem Fake-Sender und einem Fake-Checker, der abwechselnd 200 und 503 liefert. Neu: ein Interface `Notifier` beim Scheduler, eine Migration, keine Abhängigkeit.

### PostgreSQL mit pgx, skizziert

Ein Package `internal/pgstore` mit denselben Methoden wie `sqlitestore.Store` und `github.com/jackc/pgx/v5` als einziger neuer Abhängigkeit – begründet, weil die Standardbibliothek keinen Postgres-Treiber hat. `ClaimDue` wird zu einem Statement: `UPDATE targets SET next_check_at = … WHERE id IN (SELECT id FROM targets WHERE next_check_at <= $1 ORDER BY next_check_at LIMIT $2 FOR UPDATE SKIP LOCKED) RETURNING …`; `SKIP LOCKED` lässt mehrere Instanzen sich die Ziele teilen. Der Unique-Fehler wird über `errors.As` auf `*pgconn.PgError` mit Code `23505` erkannt und auf `monitor.ErrDuplicate` abgebildet. Die Migrationen brauchen eigene SQL-Dateien (`BIGSERIAL`, `TIMESTAMPTZ` statt Millisekunden). Testen lassen sich die Store-Tests als gemeinsame Tabelle, die gegen beide Implementierungen läuft, für Postgres mit `DATABASE_URL` und `t.Skip`, wenn die Variable fehlt. Scheduler und API ändern sich nicht – wenn doch, steckt SQLite-Wissen in einem Interface.

### Prometheus-Endpunkt, skizziert

Ohne Bibliothek: ein Handler auf dem Debug-Listener (nicht auf der öffentlichen API), der die expvar-Zähler im Textformat schreibt, `# TYPE upwatch_checks_total counter` gefolgt von `upwatch_checks_total 42`. Für das Histogramm der Check-Dauer ein kleiner Typ mit festen Bucket-Grenzen und `atomic.Int64` pro Bucket, plus `_sum` und `_count`; der Writer im Scheduler ruft `Observe(r.Duration)` auf. Mit `github.com/prometheus/client_golang` wird es kürzer und korrekter (Escaping, Labels), kostet aber eine Abhängigkeit mit eigenem Baum. Für drei Zähler und ein Histogramm reicht die eigene Fassung; mit Labels pro Ziel kippt die Rechnung zugunsten der Bibliothek.

### Web-Oberfläche mit html/template, skizziert

Ein Package `internal/web` mit eingebetteten Templates (`//go:embed templates/*.html`), einem Handler für die Liste und einem für das Formular, das intern dieselbe Logik wie `createTarget` nutzt (`monitor.NewTarget`, `Store.CreateTarget`). `html/template` escapt kontextabhängig, URLs der Ziele landen also nicht als Skript im Browser. `http.CrossOriginProtection` (seit Go 1.25) schützt das Formular gegen CSRF, davor gehört eine Authentifizierung; für einen Browser eignet sich eine Session mit Cookie oder Basic Auth am Reverse Proxy besser als der Bearer-Token aus Teil 13, Übung 1. Neu: ein Package, keine Abhängigkeit; und `govulncheck` meldet die `html/template`-Treffer aus Teil 13, Übung 4 dann als erreichbar.
