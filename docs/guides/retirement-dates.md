# Retirement dates

Rebrickable has no retirement-date field, so this reads a community-maintained spreadsheet instead — currently the
["Brick Tap" LEGO retirement tracker](https://docs.google.com/spreadsheets/d/1rlYfEXtNKxUOZt2Mfv0H17DvK7bj6Pe0CuYwq6ay8WA/htmlview).
**This is someone's personal public document, not an official API** — it can move, be restructured, or go private at
any time without warning. Every use of it here is deliberately fail-soft: a bad or unreachable fetch changes nothing
(whatever was imported last stays in place), and there's a manual import as a fallback.

## Fetch it

```bash
wms-go lego retirement refresh              # LEGO_RETIREMENT_SHEET_URL (the Brick Tap sheet, by default)
wms-go lego retirement refresh --url <csv>   # a different sheet, tab, or mirror
```

Set `RETIREMENT_AUTO_REFRESH_HOURS` (Admin → Settings) to have the gateway fetch it on a schedule, the same way
`CATALOG_AUTO_REFRESH_HOURS` keeps the offline catalog current. Off by default.

## Import it manually

If the live sheet is ever down, moved, or you'd rather not depend on the gateway reaching it automatically: download
the sheet as CSV yourself and import that file directly.

```bash
wms-go lego retirement import brick-tap-export.csv
```

Columns are matched by **header name**, not position, so a reordered or hand-edited copy still imports — only a
`Set #` column is required. Recognised headers: `Theme`, `Subtheme`, `Set #`, `Set Name`, `Retirement Date`, `Notes`
(various spellings of each are accepted; anything else in the file is ignored).

## What it's used for

```bash
wms-go lego retirement list                 # everything retiring within 180 days (--within N to change it)
```

Every row is flagged **owned** (a copy is in your collection), **watching** (you have a [BrickLink price watch](missing-parts-and-orders.md)
on it), both, or neither — the same list read three ways, rather than three separate features:

- **Owned**: a heads-up on a set you already have.
- **Watching**: the more actionable one — something you want, at a price you're waiting for, that's also about to
  disappear. Combine with `wms lego watch add set <num> --max <price>` if you haven't already.
- **Everything**: just browsing what's retiring, regardless of whether it's yours or on your radar.

In the interface: **Set Workshop → 8 Retiring soon**.

`wms doctor` reports when the retirement data was last imported (or that it hasn't been yet — this is entirely
optional, nothing else depends on it).
