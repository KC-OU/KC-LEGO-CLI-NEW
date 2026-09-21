# Adding parts

One flow serves **Part-DB → Create New Part**, **LEGO → Add Owned Part** and **Quick Add**.

1. **Type a part number.** The name, category and the colours that part exists in are looked up: the offline catalog first
   (instant), live Rebrickable only if the catalog doesn't know it. A LEGO **element ID** (the 6–8 digit number on a bag or brick)
   works too and also fills in the colour.
2. **Choose the colour** from the colours that part really comes in (with a swatch). Type part of a name to narrow the list.
   Anything not listed is kept as typed text. A blank answer means "no colour".
3. **Category.** LEGO parts go under `Lego > <Rebrickable category>` in Part-DB, created if missing. If the lookup has no
   category you pick one of your Part-DB categories (type to filter) or type a new name.
4. **Quantity**, and optionally **Warn me when below** (a minimum; see [low stock](#low-stock)). The quantity defaults to what you
   already hold; typing replaces it (Backspace edits it), so an update only changes the number.
5. Type **yes** to save.

--8<-- "docs/assets/screens/add-part-confirm.html"

**F9 undoes** the last add or change in this session.

## What gets written

- Your **LEGO collection** (`lego.db`) always gets the row first, so nothing you typed is lost.
- **Part-DB** gets one part per part **and colour**: name `Brick 2 x 4 - Red`, IPN `3001-4` (part number plus Rebrickable colour id;
  a typed colour becomes `3001-x-teal`), colour in the tags, manufacturer number left empty. Adding the same part and colour again updates
  the stock instead of creating a second part.
- If Part-DB can't be reached or no API token is set, the row is kept and flagged *not in Part-DB yet*; `wms lego sync-parts` sends it later.

!!! note "Part-DB is written through its REST API"
    Never by editing its database file. You need an **Edit-level API token** for a user with API access
    (*Part-DB → user menu → API tokens*), pasted under *Admin → Settings & API Keys*.

## A part nobody knows

Electronics, or anything not in the LEGO catalog: the same screen turns the name, description and manufacturer number into
fields you type, and the part is created in Part-DB only.

## From a script

```bash
wms-go lego add-part 3001 --color red --qty 25 --yes
wms-go lego add-part 3001 --color red --qty 25 --dry-run --json     # what would be saved
```

`--color` takes a colour name or a Rebrickable colour id and must be one the part comes in.

## Low stock

Set a minimum on the confirm screen, or `wms-go lego set-min 3001 --color red --min 50`. A part below its minimum is marked
`LOW`, counted on the LEGO hub and Overview, listed by `wms-go lego low` (`--fail` exits 1, for cron), included in
[alerts](alerts.md), and set as Part-DB's own *minimum amount* at the next sync.

## Recents

The part prompt lists your recent parts. Type `!1` to reuse the first.
