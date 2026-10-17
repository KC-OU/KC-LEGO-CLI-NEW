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
wms-go lego report history [set] [--by checker] [--limit N] [-o file]
wms-go lego help-sheet [-o file]
```

`--format` defaults to the clean, printable report; it also accepts anything `lego export` does (`xlsx`, `csv`, `json`, plain
`html`) — except **List of Sets**, where plain `csv` is empty (it's parts-only): use `sets-csv` instead. `report history`
prints a table to the terminal unless `-o` is given, which writes the printable page instead. Without `-o`, every other
report's body is printed to stdout. `report stocktake` writes `<set>-stocktake.html` per set unless `-o` is given (one set
at a time only).

`--by` tracks down a past check by who did it — a part of a username is enough (a case-insensitive match, same as the
other search fields in this app), so you don't need to remember or type someone's exact username to find their checks.

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

**Set Workshop → 7 Reports → 9 Check History** lists every finished check, newest first. Type `/` to search — part of a
set number, a checker's username, a date, or "complete" — the list narrows as you type, same as every other searchable
list in the app; there's no separate picker to fiddle with and no row number to hunt for. The phone app has the same
search, admin-only, via `GET /mobile/admin/check-history?set=&by=&limit=`.

## Paper-first: check on paper, enter it in the CLI later

If you'd rather count a set on paper first and type it into the CLI afterwards, three commands tie the two together so
nothing finished on paper gets forgotten, and a proof scan protects a correct paper count if the CLI entry ever looks
wrong:

```bash
wms-go lego paper-done <set>                  # the paper copy is done; the CLI entry is still to come
wms-go lego attach <set> <scan.jpg|png|pdf>   # attach a photo/scan of the finished paper sheet as proof
wms-go lego checks pending                    # every set that's paper-done but not yet entered
```

`paper-done` opens (or resumes) that set's check and stamps it, without touching any part counts — it's the *same*
check session `wms-go lego check` resumes into later, so finishing the CLI entry for that set automatically clears it
from `checks pending`. The login banner and `wms-go sys status` also show a running count ("N need CLI entry") so a
paper sheet left unentered is hard to miss.

`attach` checks the actual file content (not just its extension) before accepting it — a genuine jpg, png or PDF only,
up to 25 MB — then stores a copy next to `lego.db` (or `LEGO_ATTACHMENTS_DIR`, if set) and records it against that
set's current or most recent check. If the CLI's own count is ever questioned, that photo is the record of what the
paper copy actually said. Add `--flag "reason"` if you notice the CLI and paper counts disagree — it marks the check
disputed, for whoever reviews accuracy to see alongside the proof, without changing any numbers on its own.

The **Parts Stock-take** sheet (above) also carries this: its header, condition, cost, and part/minifig counts prefill
from whatever's already known, and its sign-off line now shows whether a proof photo/scan was filed, and its reference,
once one has been attached.

In the interface: **Set Workshop → 9 Paper done, CLI pending** lists the same sets the CLI command does.
