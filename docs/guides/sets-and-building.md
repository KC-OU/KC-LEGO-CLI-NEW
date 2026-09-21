# Sets, missing parts and what to build

## Your sets

**LEGO → Add / Update a Set**: type the set number; name, year, piece count and theme are filled in (offline catalog first). You give the
number of copies. `wms-go lego stats` (or *LEGO → Collection Stats*) summarises everything.

--8<-- "docs/assets/screens/stats.html"

## Missing parts for a set

**LEGO → Missing Parts for a Set** or `wms-go lego missing 75192 [--copies 2]`. Your loose parts are compared with the set's parts
list, matching **part and colour**.

--8<-- "docs/assets/screens/missing.html"

It counts **equivalent parts** too: alternates and moulds (the same part under another number) by default. The *Covered by* column
names what covered a shortfall so you can check it. Change it with `--equivalents none|default|alt,mold,print` or `WMS_EQUIVALENTS`.
Each piece you hold is used only once. Parts you hold in a *typed* colour can't be matched.

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
