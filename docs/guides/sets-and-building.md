# Sets, missing parts and what to build

## Your sets

**LEGO → Add / Update a Set**: type the set number; name, year, piece count and theme are filled in (offline catalog first). You give the
number of copies. `wms-go lego stats` (or *LEGO → Collection Stats*) summarises everything.

A set can also be registered with a second way in: its retail box's own barcode (UPC/EAN) — useful when the set number
printed on the box is small or awkward to scan, but the barcode is right there. Set it with
`wms-go lego set-info <set> --box-barcode <code>`, or the "Box barcode" field on **Set Workshop → Where the Set Is
Kept**. Once registered, scanning or typing that barcode anywhere a set number works — the CLI, the TUI, the mobile
app — opens the same set, exactly as if you'd typed its set number.

A set's instruction book number(s) are normally filled in from catalog import, but you can set or correct them
yourself too: `wms-go lego set-info <set> --instruction-book <number> --instruction-book-count <n>`, or the matching
fields on **Set Workshop → Where the Set Is Kept**. A multi-book set uses the same slash-separated convention the
catalog data already does — `6644777/6644778` for two booklets.

A set can also carry its own photos — the box, the built model, anything worth keeping a picture of — any number of
them, not just one override image. `wms-go lego set-photo <set> <file.jpg>` attaches one (verified by its actual
bytes, same as a check's proof photo), `wms-go lego set-photos <set>` lists what's attached, and
`wms-go lego set-photo-remove <id>` removes one. The set detail screen shows how many are attached. This is CLI-only
— the same reasoning as attaching a check's proof photo (`wms-go lego attach`): it needs a real file on disk, which a
remote telnet/web session has no access to.

--8<-- "docs/assets/screens/stats.html"

## Missing parts for a set

**LEGO → Missing Parts for a Set** or `wms-go lego missing 75192 [--copies 2]`. Your loose parts are compared with the set's parts
list, matching **part and colour**.

--8<-- "docs/assets/screens/missing.html"

It counts **equivalent parts** too: alternates and moulds (the same part under another number) by default. The *Covered by* column
names what covered a shortfall so you can check it. Change it with `--equivalents none|default|alt,mold,print` or `WMS_EQUIVALENTS`.
Each piece you hold is used only once. Parts you hold in a *typed* colour can't be matched.

### Optional parts (stickers)

Sticker sheets default to **optional**: they never count toward a set's missing parts or completion, on the
Missing Parts report or anywhere else, even before you've counted them. Any part can be marked optional or
required, either way, from the parts check screen (**O**) or the CLI:

```bash
wms-go lego optional 78256          # shows the current state
wms-go lego optional 78256 on       # mark it optional (won't count as missing)
wms-go lego optional 78256 off      # mark it required again
```

A blank stock-take checklist still lists optional lines to count against by hand — marked *(optional)* — just
without them in the printed total.

## What can I build?

**LEGO → What Can I Build?** or `wms-go lego build`. Ranks sets by how much of each your loose parts cover, entirely offline.

--8<-- "docs/assets/screens/build.html"

```bash
wms-go lego build --min-percent 85 --max-missing 40 --limit 20
```

It matches exact part and colour (use *missing* on one set for the equivalent-parts view) and ignores tiny sets (`--min-pieces`, default 20).
On a large collection it takes a few seconds.

## Shopping lists

```bash
wms-go lego wanted --set 75192 -o falcon.xml       # BrickLink wanted list (XML)
wms-go lego wanted --low -o restock.xml            # what you are running out of
wms-go lego export --format csv --set 75192 --missing -o shopping.csv
```

Upload the XML at BrickLink: *Wanted → Upload Wanted List Items*. BrickLink colour numbers come from the colour map
(`wms-go bricklink colors sync`); lines without one get their colour left out and are counted in the summary so you know what to check.
