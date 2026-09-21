# History and restore

Every change to your collection is recorded, and a **snapshot is taken before the first change of each day**.

--8<-- "docs/assets/screens/history.html"

## The journal

**LEGO → History of Changes** or:

```bash
wms-go lego history                  # newest first: when, who, what, item, colour, before -> after
wms-go lego history --part 3001
```

Adds, quantity changes, deletes, minimum changes, bulk imports and restores are recorded with the user who did it (the signed-in
user in the interface, `cli:<name>` from the command line).

## Snapshots and the growth chart

```bash
wms-go lego snapshots                # restore points, with a pieces-over-time sparkline
wms-go lego snapshots take --label "before the big sort"
```

## Restore

```bash
wms-go lego restore 2026-09-20 --dry-run     # exactly what would change
wms-go lego restore 14 --yes
```

Restore puts your owned parts and sets back as they were. It **saves the present first** (as the newest snapshot), so a restore can itself be
undone by restoring that snapshot. Part-DB isn't touched: run `wms-go lego sync-parts` afterwards to bring it back in line. Parts that exist
both then and now keep their Part-DB link.

Up to 400 daily snapshots are kept (about a year).
