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

## Where the set is kept

The add/confirm form and *Completion dashboard → I* record a **location** (shelf, box, bin) and a **condition** (sealed,
built, in pieces, displayed). Both appear on labels. CLI: `wms-go lego set-info 75192 --location "Shelf B2" --condition built`.

## From the shell

```bash
wms-go lego check 75192 --missing 3001:red:2,3023:1:1 --extra 3710:black:3   # colour = name or Rebrickable id
wms-go lego check 75192 --stocktake --by alex
```
