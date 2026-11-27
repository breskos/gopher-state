# gopher-state für urbs42 – Anforderungen an den Umbau

Stand: 27.11.2026. Ziel ist Version **v0.3.0** des Pakets `workflow`.

## Kontext

[urbs42](https://github.com/breskos/urbs42) ist eine dauerhaft laufende Lebenssimulation mit LLM-gesteuerten
Figuren, geschrieben in Go. urbs42 nutzt gopher-state für **Abläufe in Stufen**, nicht für die
Entscheidungen von Minute zu Minute (die trifft dort eine Nutzen-Heuristik bzw. ein LLM):

| Einsatz in urbs42 | Graph | ab urbs42-Meilenstein |
|---|---|---|
| LLM wählt nur aus Optionen, die der Code erzeugt | `Options` → LLM → `StepTransition` | M2 |
| LLM schlägt neue Schritte vor, Code prüft | `ApplyAction` mit Patch | M2 |
| Kennenlernen, Bekanntheitsstufen (fremd → flüchtig → bekannt → befreundet → eng) | ein Lauf pro gerichteter Beziehung | M3 |
| gemeinsame Aktivitäten, Gruppen | `Encounter`, `Together` | M3 |
| Routine des Avatars während Abwesenheit | Tagesablauf als Graph | M8 |
| Gründung (Idee → … → Eröffnung), Romantik-Phasen, Konflikt- und Gewalteskalation, Intrigen | je ein Graph | M9 |

Daraus ergeben sich Anforderungen, die der heutige Stand (v0.2.0, Paket `workflow`) nicht erfüllt.
Wichtigste Randbedingungen aus urbs42:

- **Spielstände** werden gespeichert und geladen; ein Lauf muss danach **exakt identisch** weitergehen.
- **Zufall** leitet urbs42 pro Entscheidung aus (Seed, Figur, Simulationszeit) ab, nicht aus einem laufenden Generator.
- **Alles ist erklärbar**: Zu jeder Veränderung muss die Ursache nachvollziehbar sein, auch warum etwas *nicht* ging.
- **Spielstände sind lesbare Dateien** mit stabiler Reihenfolge (git-freundlich).

## Kennzeichnung

- IDs `GST-NNN`. In Commits und Tests darauf verweisen.
- **MUSS** / **SOLL** / **KANN** wie in urbs42.
- In Klammern die urbs42-Anforderung, aus der es folgt, z. B. `(← PST-001)`.
- Code, Bezeichner und Paketdokumentation auf Englisch (wie bisher). Die API-Skizzen sind Vorschläge;
  Namen dürfen mit Begründung abweichen.

## Grundsätze

**GST-001 MUSS** Weiterhin nur Standardbibliothek, kein cgo.

**GST-002 MUSS** Das Repository ist privat und urbs42 der einzige Abnehmer. API-Brüche sind erlaubt, wenn sie
im README und in einem neuen `CHANGELOG.md` mit Migrationshinweis stehen. Bestehende Beispiele und Tests werden
angepasst, nicht gelöscht.

**GST-003 MUSS** `Step` bleibt ohne Heap-Allokation, solange keine der neuen Optionen genutzt wird
(Benchmark `BenchmarkStep` als Nachweis).

**GST-004 MUSS** Alle neuen Teile sind nebenläufig so sicher wie die bestehenden (`go test -race`).

**GST-005 MUSS** Die Bibliothek kennt keine Simulationszeit, keine Personen und kein Dateiformat von urbs42.
Alles Anwendungsspezifische kommt über Fakten, Metadaten und den Kontext aus GST-200.

## 1 Läufe speichern und fortsetzen (← urbs42 PST-001, ADR 005)

Heute beginnt `World.NewRun` immer am Startzustand; Zustand und Sequenznummer eines `Run` sind privat.

**GST-100 MUSS** Zustand eines Laufs auslesen:

```go
type RunSnapshot struct {
    ID       string `json:"id"`
    State    string `json:"state"`
    Sequence uint64 `json:"sequence"`
}
func (r *Run) Snapshot() RunSnapshot
```

**GST-101 MUSS** Lauf an einem gespeicherten Zustand fortsetzen:

```go
func (w *World) ResumeRun(s RunSnapshot, draw Draw, recorder Recorder) (*Run, error)
```

Prüft, dass der Zustand im aktuellen Graphen existiert (sonst `ErrUnknownState`) und die ID nicht leer ist.
Die nächste Aufzeichnung trägt `s.Sequence + 1`.

**GST-102 MUSS** Fortgesetzte Läufe funktionieren in allen Operationen wie neue: `Step`, `Options`,
`StepTransition`, `ApplyAction`, `Encounter`, `Together`.

**GST-103 MUSS** Test der Gleichwertigkeit: Ein Lauf mit N Ereignissen ergibt dieselben Aufzeichnungen wie ein
Lauf, der nach k Ereignissen per `Snapshot` gesichert, über JSON serialisiert, mit `ResumeRun` (in einer aus
GST-110 wiederhergestellten Welt) fortgesetzt und mit den restlichen Ereignissen weitergeführt wird. Gilt für
`first` und `weighted`, mit Wahrscheinlichkeiten und mit Patches zwischendurch.

## 2 Welt speichern und wiederherstellen (← urbs42 PST-002, PST-008, WIR-042, SCH-013)

Heute ist die Welt nur anfügbar und kann ihren aktuellen Graphen nicht ausgeben.

**GST-110 MUSS** Aktuellen Graphen als eine `Definition` ausgeben:

```go
func (e *Engine) Definition() Definition   // e = world.Snapshot()
```

Stabile Reihenfolge: Zustände und Kanten in Einfügereihenfolge (Basisgraph, dann Patches). `Compile` auf das
Ergebnis ergibt einen gleichwertigen Graphen (gleiche Optionen, gleiche Auswahl bei gleichem Zufall).

**GST-111 MUSS** Zweimal ausgeben ergibt dieselben Bytes (`json.Marshal` auf `Definition`).

**GST-112 MUSS** Protokoll der angenommenen Patches mit Herkunft:

```go
type PatchInfo struct {
    ID     string `json:"id"`               // vom Aufrufer vergeben, eindeutig
    Source string `json:"source,omitempty"` // z. B. "welt", "figur:mara-albers", "schoepfer"
    Note   string `json:"note,omitempty"`
}
type Patch struct {
    Info        PatchInfo    `json:"info,omitzero"`
    States      []string     `json:"states,omitempty"`
    Transitions []Transition `json:"transitions,omitempty"`
    // Retire: siehe GST-400
}
func (w *World) Patches() []Patch  // angenommene Patches in Reihenfolge, Kopien
```

`Info.ID` ist optional für Rückwärtskompatibilität; wenn gesetzt, wird eine doppelte ID abgelehnt
(schützt vor doppeltem Anwenden nach einem Neustart).

**GST-113 MUSS** Welt aus Basisgraph und Patch-Folge wiederherstellen:

```go
func RestoreWorld(base *Engine, patches []Patch) (*World, error)
```

Ergibt dieselbe `Definition` wie die ursprüngliche Welt. Ein ungültiger Patch bricht mit Fehler ab, der den
Index und die `Info.ID` nennt.

**GST-114 SOLL** Herkunft je Kante abrufbar (aus welchem Patch sie stammt), für die Anzeige
„im Spiel gegründet“ vs. „vom Schöpfer eingefügt“ (← urbs42 WIR-042).

## 3 Kontext und Zufall pro Schritt (← urbs42 LP-5, ADR 005)

Heute bekommt ein `Run` seinen `Draw` einmal bei der Erzeugung, und `Record` hat kein Feld für Kontext.

**GST-200 MUSS** Optionen pro Aufruf:

```go
type StepOptions struct {
    Draw Draw                         // überschreibt den Draw des Runs für diesen Aufruf
    Meta map[string]json.RawMessage   // wird unverändert in den Record übernommen
}
func (r *Run) StepWith(event string, facts Facts, opt StepOptions) (Decision, bool, error)
func (r *Run) StepTransitionWith(event, transition string, facts Facts, opt StepOptions) (Decision, bool, error)
func (r *Run) ApplyActionWith(action Action, opt StepOptions) (Decision, bool, error)
func (e *Encounter) StepWith(r *Run, scope Scope, event, transition string, facts Facts, opt StepOptions) (Decision, bool, error)
```

Die bestehenden Methoden bleiben und rufen die neuen mit leeren Optionen auf.

**GST-201 MUSS** `Record` bekommt `Meta map[string]json.RawMessage \`json:"meta,omitempty"\``. urbs42 schreibt dort
z. B. Simulationszeit, Ursachen-ID und ID des LLM-Aufrufs hinein.

**GST-202 MUSS** `Together` erhält einen Meta-Parameter für die Gruppe (oder eine `TogetherWith`-Variante), der in
jeden Record der Gruppe übernommen wird.

**GST-203 MUSS** Test: Gleicher Draw pro Aufruf ergibt gleiche Entscheidung, unabhängig vom Draw des Runs.

## 4 Begründete Optionen (← urbs42 KOG-004, KOG-012, LP-5)

Heute liefert `Options` nur erfüllbare Kanten. Für den Inspektor von urbs42 und für LLM-Prompts
(„das Konzert kannst du dir nicht leisten“) braucht es auch die abgelehnten mit Grund.

**GST-300 MUSS** Alle Kanten eines Zustands für ein Ereignis mit Bewertung:

```go
type Candidate struct {
    Option      Option     // wie bei Options; Scope und Ziele gefüllt
    Eligible    bool
    Failures    []Failure  // leer, wenn Eligible
}
type Failure struct {
    Kind     string `json:"kind"`     // "missing", "type", "unit", "compare", "requirement", "retired"
    Field    string `json:"field,omitempty"`
    Op       string `json:"op,omitempty"`
    Expected Value  `json:"expected,omitzero"`
    Actual   Value  `json:"actual,omitzero"`
    Path     string `json:"path,omitempty"` // Lage in verschachtelten all/any/not, z. B. "when.all[1]"
}
func (e *Engine) Explain(current, event string, facts Facts) []Candidate
func (r *Run) Explain(event string, facts Facts) []Candidate
func (enc *Encounter) Explain(r *Run, event string, facts Facts) []Candidate
```

**GST-301 MUSS** `Explain` zieht keinen Zufall und ändert nichts. Für `Eligible == true` stimmt das Ergebnis mit
`Options` überein (Test).

**GST-302 MUSS** Bei `any` werden die Gründe aller Zweige gemeldet, bei `not` der Hinweis, dass die negierte
Bedingung erfüllt war.

**GST-303 SOLL** Kosten: `Explain` darf allokieren; `Step` und `Options` bleiben unverändert schnell (GST-003).

## 5 Stilllegen statt nur anhängen (← urbs42 WIR-011, WEL-052)

Ein pleitegegangener Betrieb oder ein geschlossener Ort soll aus den Optionen verschwinden, aber in Graph und
Historie bleiben.

**GST-400 MUSS** Patches können Kanten und Zustände stilllegen:

```go
type Retire struct {
    States      []string `json:"states,omitempty"`
    Transitions []string `json:"transitions,omitempty"`
}
// Patch.Retire Retire `json:"retire,omitzero"`
```

**GST-401 MUSS** Stillgelegte Kanten sind nie wählbar und erscheinen in `Explain` mit `Kind: "retired"`.
Kanten **in** einen stillgelegten Zustand sind nicht wählbar; Kanten **aus** ihm heraus bleiben wählbar, damit
Läufe, die dort stehen, ihn verlassen können.

**GST-402 MUSS** `Definition` (GST-110) enthält stillgelegte Teile mit Markierung (z. B. `"retired": true` an der
Kante, `retiredStates` in der Definition), damit Wiederherstellung und Historie vollständig sind.

**GST-403 MUSS** Stilllegen ist endgültig; Reaktivieren geschieht über eine neue Kante mit neuer ID.

## 6 Graph für Darstellung (← urbs42-Oberfläche)

**GST-500 SOLL** Graph als Knoten und Kanten für eine Oberfläche:

```go
type Graph struct {
    Start string      `json:"start"`
    Nodes []GraphNode `json:"nodes"` // id, retired, patch (Herkunft)
    Edges []GraphEdge `json:"edges"` // id, from, to, event, selection, weight, probability,
                                     // condition, requires, metadata, retired, patch
}
func (e *Engine) Graph() Graph
```

**GST-501 KANN** Ausgabe als Graphviz DOT für Fehlersuche.

## 7 Beispiel und Dokumentation

**GST-600 MUSS** Neues Beispiel `examples/acquaintance`: Bekanntschaft als Graph
fremd → flüchtig → bekannt → befreundet → eng befreundet, mit Rückstufung bei Kontaktmangel. Fakten
z. B. `zuneigung`, `vertrauen`, `begegnungen`, `tageOhneKontakt`. Das Beispiel zeigt:
1. `Explain` vor einer Entscheidung,
2. `StepWith` mit Meta (Simulationszeit) und eigenem Draw,
3. Speichern (`Snapshot`, `Definition`, `Patches`) als JSON und Fortsetzen (`RestoreWorld`, `ResumeRun`),
4. Stilllegen einer Kante per Patch.

**GST-601 MUSS** README-Abschnitte für alle neuen Funktionen; `CHANGELOG.md` mit Einträgen und Migration für v0.3.0.

## 8 Tests

**GST-700 MUSS** Tests zu GST-103, GST-111, GST-113, GST-203, GST-301 und GST-401 wie dort beschrieben.

**GST-701 MUSS** Fuzz-Test für `ResumeRun` mit beliebigen Snapshots (keine Panik, sauberer Fehler).

**GST-702 MUSS** Fuzz-Test für `RestoreWorld` mit beliebigen Patch-Folgen aus JSON.

**GST-703 MUSS** `go test -race ./...`, `go vet ./...`, Benchmarks ohne Rückschritt gegenüber v0.2.0.

## Nicht-Ziele

- Kein Dateiformat und kein Schreiben auf die Platte für Läufe oder Welten; das macht urbs42.
- Keine Simulationszeit, keine Zeitgeber, keine Personen in der Bibliothek (GST-005).
- Keine Änderung an der Auswahl-Semantik (`first`, `weighted`, `probability`, `requires`).

## Offene Fragen

| ID | Frage | Stand |
|---|---|---|
| OFS-01 | Bleibt der Automaten-Teil (DFA, NFA, Matcher, `dfa/`) im Repository oder wird gopher-state auf `workflow` verschlankt? urbs42 braucht ihn nicht. | offen |
| OFS-02 | Lizenz: Die GPL-3.0-Datei `LICENSE` ist im Arbeitsstand vom 27.11.2026 gelöscht (noch nicht committet); ohne Lizenzdatei gilt „alle Rechte vorbehalten“. Repository ist privat, Auftraggeber ist alleiniger Urheber. So lassen, oder eine Lizenz (z. B. Apache-2.0 wie gopher-learn) bzw. ein proprietärer Hinweis? | offen |
| OFS-03 | Typ von `Meta`: `map[string]json.RawMessage` (Vorschlag, wie `Metadata`) oder ein eigener Typ | Vorschlag |
| OFS-04 | Go-Version: bleibt 1.25; urbs42 zieht von 1.24 nach | Vorschlag |

## Einbindung in urbs42 (zur Orientierung)

- Modulpfad `github.com/breskos/gopher-state`, privat: urbs42 braucht `GOPRIVATE=github.com/breskos/*` und in der
  CI ein Token mit Lesezugriff.
- urbs42 speichert pro Spielstand: Basisgraphen, `Patches()`, `Snapshot()` aller Läufe; die Aufzeichnungen
  landen in den Protokollen und Gedächtnissen der Figuren.
- Draw in urbs42: ein PCG-Generator, abgeleitet aus Welt-Seed, Figur und Simulationszeit, pro Aufruf über
  `StepOptions.Draw` übergeben.
