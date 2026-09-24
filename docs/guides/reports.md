# Reports

--8<-- "docs/assets/screens/reports.html"

Where exports give you the raw table, reports give you something meant to be read or printed: a title page, a summary,
and a clean layout — plus a blank checklist for counting a set by hand. Six reports:

| Report | Covers |
|---|---|
| **Parts Stock-take** | a **blank** checklist (part, colour, name, expected qty, an empty box) to print and count against by hand |
| **Set (ID) Parts Lists** | one or more sets' full parts lists, side by side |
| **List of Sets on Collection** | every set you own — no loose parts (for sets *and* loose parts together, use `wms lego export`) |
| **Missing Parts** | every incomplete set, or just the ones you name — totals, and what's on order |
| **Extra Parts** | spares left over from completed set checks, grouped by which set they came from |
| **Order List** | every parts order (or just open ones), most recent first, grouped by order |

Check history (past stock checks, newest first) is a separate, seventh command — it doesn't fit the same "here's a
snapshot of your collection" shape as the other six. There's also an eighth, unrelated to your collection: `wms-go lego
help-sheet` prints the same key bindings F1 shows in the app, as a page you can keep next to your keyboard.

## From the CLI

```bash
wms-go lego report stocktake <set...> [-o file]
wms-go lego report setparts <set...> [-o file]
wms-go lego report setlist [-o file]
wms-go lego report missing [set...] [-o file]
wms-go lego report extra [set...] [-o file]
wms-go lego report orders [--open] [-o file]
wms-go lego report history [set] [--limit N] [-o file]
wms-go lego help-sheet [-o file]
```

`--format` defaults to the clean, printable report; it also accepts anything `lego export` does (`xlsx`, `csv`, `json`, plain
`html`) — except **List of Sets**, where plain `csv` is empty (it's parts-only): use `sets-csv` instead. `report history`
prints a table to the terminal unless `-o` is given, which writes the printable page instead. Without `-o`, every other
report's body is printed to stdout. `report stocktake` writes `<set>-stocktake.html` per set unless `-o` is given (one set
at a time only).

The stock-take checklist reuses your last check's parts list if the set has one, so a recount sheet matches exactly what
was last recorded; a set that's never been checked falls back to its catalog parts list, so you can print a blank sheet
before the first digital check too.

Every report command also takes `--discord`/`--discord-expires` — see
[Discord notifications](discord-notifications.md).

## From the interface

**Set Workshop → 7 Reports** offers all six directly (two ask which set(s) first), plus the archive and the key cheat
sheet. It reuses the same export screen as everywhere else in the app (**X** on Missing Parts, Owned Parts, etc.) — pick
**P** for the printable report, or any of the other formats. Saved files and download links/QR codes work exactly as
described in [Export and import](export-import.md#download-it-with-a-qr-code).

Check history isn't wired into the TUI yet — use the CLI for that one.
