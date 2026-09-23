# Reports

Where exports give you the raw table, reports give you something meant to be read or printed: a title page, a summary,
and a clean layout — plus a blank checklist for counting a set by hand.

## From the CLI

```bash
wms-go lego report missing [set...] [-o file] [--format <report|html|xlsx|csv|json>]
wms-go lego report collection [-o file]
wms-go lego report set <set...> [-o file]
wms-go lego report history [set] [--limit N] [-o file]
wms-go lego stocksheet <set...> [-o file]
```

| Command | Covers |
|---|---|
| `report missing` | every incomplete set, or just the ones you name — totals, and what's on order |
| `report collection` | every set and loose part you own, with a summary |
| `report set` | one or more sets' full parts lists, side by side |
| `report history` | past stock checks, newest first — for one set, or across the collection |
| `stocksheet` | a **blank** checklist (part, colour, name, expected qty, an empty box) to print and count against by hand |

`--format` defaults to the clean, printable report (`report`); it also accepts anything `lego export` does (`xlsx`, `csv`, `json`,
plain `html`). `report history` prints a table to the terminal unless `-o` is given, which writes the printable page instead.
Without `-o`, the report body is printed to stdout. `stocksheet` writes `<set>-stocksheet.html` per set unless `-o` is given (one
set at a time only).

The stock sheet reuses your last check's parts list if the set has one, so a recount sheet matches exactly what was last
recorded; a set that's never been checked falls back to its catalog parts list, so you can print a blank sheet before the first
digital check too.

## From the interface

**Set Workshop → 7 Reports** offers the missing-parts and collection reports directly, and a set-parts report after asking which
set(s). It reuses the same export screen as everywhere else in the app (**X** on Missing Parts, Owned Parts, etc.) — pick
**P** for the printable report, or any of the other formats (Excel, CSV, JSON, plain HTML). Saved files and download
links/QR codes work exactly as described in [Export and import](export-import.md#download-it-with-a-qr-code).

The check-history report and the stock sheet aren't wired into the TUI yet — use the CLI for those.
