# Lösungen

Musterlösungen zu den Übungen aller 16 Teile. Sie liegen bewusst auf einem eigenen Branch: Auf `main` steht nur der Code des Tutorials, hier zusätzlich, wie die Übungen gelöst werden können.

Versuch jede Übung zuerst selbst. Die Lösungen zeigen jeweils *eine* gute Lösung und begründen sie – bei vielen Übungen gibt es mehrere vertretbare Wege.

## So benutzt du die Lösungen

Jede Datei `NN-kapitel.md` enthält zu jeder Übung die Lösung und einen Abschnitt **Warum so**. Übungen, die Code ändern, haben zusätzlich einen Patch unter `NN-kapitel/uebung-N.patch`. Jeder Patch gilt für sich allein gegen `main`. Am einfachsten wendest du ihn im Branch `loesungen` an – der Code dort ist derselbe wie auf `main`:

```bash
git switch loesungen
git apply loesungen/06-concurrency/uebung-3.patch
go test ./...
git restore . && git clean -fd -- internal cmd beispiele   # wieder zurück
```

## Prüfen

`check.sh` wendet jeden Patch einzeln auf einen sauberen Checkout von `main` an und führt `go vet` und `go test` aus:

```bash
loesungen/check.sh                  # alle Patches
loesungen/check.sh 06-concurrency   # nur ein Kapitel
```

## Übersicht

- [Teil 01: Warum gibt es Go?](01-warum-go.md)
- [Teil 02: Toolchain und Module](02-toolchain-und-module.md)
- [Teil 03: Die Sprache, kompakt](03-die-sprache-kompakt.md)
- [Teil 04: Go-Denkweise statt OOP-Denkweise](04-go-denkweise.md)
- [Teil 05: Fehlerbehandlung](05-fehlerbehandlung.md)
- [Teil 06: Concurrency](06-concurrency.md)
- [Teil 07: Context richtig verstehen](07-context.md)
- [Teil 08: Eine HTTP-API mit net/http](08-http-api.md)
- [Teil 09: Projektstruktur und Architektur](09-projektstruktur.md)
- [Teil 10: Datenbanken mit database/sql](10-datenbanken.md)
- [Teil 11: Testing](11-testing.md)
- [Teil 12: Logging, Konfiguration, Observability](12-logging-konfiguration-observability.md)
- [Teil 13: Security](13-security.md)
- [Teil 14: Performance und Speicher](14-performance.md)
- [Teil 15: Deployment](15-deployment.md)
- [Teil 16: Go Best Practices für Umsteiger](16-best-practices.md)
