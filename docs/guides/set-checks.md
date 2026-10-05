# Set checks and stock checks

When you add a set, go through its parts so you know it is complete — and so a stock check or audit later has something
accurate to compare with.

--8<-- "docs/assets/screens/workshop.html"

## The parts check

Add a set (*LEGO → 4 Add / Update a Set*) and answer **yes** to *Check the parts now?* — or open it any time: *LEGO → 8 Set
Workshop → 1*, **K** on a set's detail page, or `wms-go lego check <set>`.

The whole parts list opens, **every line already marked as have-all**, so you only mark the exceptions:

| Key | What it does |
|-----|--------------|
| **M** | this line is **missing** some — type how many (default 1) |
| **E** | you have **extra** of this part — type how many |
| **0-9** / Enter | type how many you have (more than the set needs = the rest are extras) |
| **H** | have all of this line again; **A** every line |
| **T** | take it from **spares** you hold elsewhere (loose parts or another set's extras) |
| **/** | filter by part, colour, name, category — or type `missing` / `extra` |
| **Z** | **scanner mode** (below) |
| **U** | undo · **S** save for later (resume where you left off) · **F** finish · **Esc** leave |

On **F**:

- the check is recorded with **who did it and when**;
- the set's parts go into **Part-DB** in their own storage location, `LEGO / Sets / <number> <name>` (one lot per part and
  colour, reusing parts you already have) — the existing sync carries them to ModernWMS;
- **extras** become loose parts that remember the set they came from, so another set that is short of that part shows
  `2 spare (from 10696)` and **T** moves them across;
- a set with anything missing is flagged **INCOMPLETE** everywhere (lists, detail page, dashboard, labels) and an alert goes
  out (see [Notifications](notifications.md)).

## Stock checks

**K** on a checked set (or `wms-go lego check <set> --stocktake`) is a **recount**: the same screen, starting from what the
last check found. Differences are applied to the database and the set's Part-DB lots, and the set page shows
`Last checked 22 Sep 2026 by alex (recount)`.

### Barcode scanner mode

Press **Z** and scan (or type) a part number or LEGO element id followed by Enter — a USB barcode scanner does exactly that.
The matching line goes up by one and flashes; a part that is not in the set, or a line already complete, beeps. **Z** or
**Esc** leaves scanner mode.

A scanner needs something to scan: **P** prints a barcode sheet for the open check — one row per part, a Code 128 barcode,
its name, shelf location and how many are needed — so you're not relying on each part's own retail packaging. From the
shell, `wms-go lego parts-sheet <set> -o sheet.pdf` prints the same sheet without opening a check at all (read-only; it
never changes anything). The two differ in where the file lands: **P** in the TUI saves it through the same
[exports mechanism](export-import.md) everything else does, so it also shows up on the **[exports dashboard](export-import.md#download-it-with-a-qr-code)**
for grabbing from your phone — the CLI's `-o` just writes the file wherever you point it, since a shell session already has
direct filesystem access and has no need for a download link.

By default it's an A4 sheet, several parts per page. `--size` takes any stock [`wms lego labels`](labels.md) does, so
small sticky/thermal labels (`50x30`, `40x30`, `62x29`, ...) print **one part per label** instead — peel one straight
onto a bin or box. `--format html` also draws each part's picture (the default PDF is vector-only — text and barcode,
no images at all): `wms-go lego parts-sheet 75192 --size 50x30 --format html -o falcon-labels.html`, then print it from
a browser at 100%, no margins, same as any other picture-bearing export.

Every barcode here is held to a minimum module width a 203 dpi thermal head can actually resolve, even when that means
letting it run slightly wider than its nominal box — a barcode that overflows its box but scans beats one that fits and
doesn't. Very long content (a long set number) on the smallest stock is a real physical limit no software fixes
outright: pick a size with more room, or a shorter `--barcode` value on `wms lego labels`, if one specific label still
won't scan.

Restocking several incomplete sets at once? `wms-go lego missing-sheet <set> [<set>...]` combines everything **missing**
(not the full parts list) across every set given onto one sheet, so one shelf walk covers all of them instead of
printing and carrying a separate sheet per set. Each set needs a check on record already; one that doesn't is skipped
with a warning rather than failing the whole run. Same `--size`/`--format` as `parts-sheet`.

### Guided walk and pictures

Press **W** for the guided view: one line at a time with a big "GO TO: &lt;location&gt;" banner, instead of the full table —
meant for walking the shelves rather than reading a list on screen. Turn on **Show/hide pictures while checking or picking**
(*My Settings*, or the palette — off by default) and this guided view also shows a colour swatch next to the colour name and
the part's (or set's) picture underneath, the same [half-blocks/ASCII rendering](search-and-pictures.md#pictures) used
everywhere else — meant for anyone who finds a picture and a colour swatch faster to confirm against than reading "Dark
Bluish Gray" or a part number off the screen.

## Where the set is kept

The add/confirm form and *Completion dashboard → I* record a **location** (shelf, box, bin) and a **condition** (sealed,
built, in pieces, displayed). Both appear on labels. CLI: `wms-go lego set-info 75192 --location "Shelf B2" --condition built`.

The same screen has an **image URL override** — when Rebrickable's own picture for a set is wrong or missing, an admin
can replace it there (`--image-url` from the shell; blank clears it, back to the catalog's own). It's picked up
everywhere that set's picture shows — the TUI, label sheets, the mobile app's check/pick screen — the next time any of
them loads it, with nothing to re-sync by hand.

## From the shell

```bash
wms-go lego check 75192 --missing 3001:red:2,3023:1:1 --extra 3710:black:3   # colour = name or Rebrickable id
wms-go lego check 75192 --stocktake --by alex
```
