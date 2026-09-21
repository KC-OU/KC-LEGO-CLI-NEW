# Offline mode

Everything you do most often works with **no internet and no API key**, because the data is on your machine.

## What is stored

`wms lego catalog refresh` downloads Rebrickable's free data files into your local database:

| File | Gives you |
|------|-----------|
| parts, part categories, colours, elements | part names, categories, which colours each part comes in, element IDs |
| sets, themes | set names, years, piece counts, full theme paths |
| minifigures | figure names |
| inventories and their parts | every set's parts list (for missing parts and *what can I build*) |
| part relationships | alternates, moulds and prints (for missing parts) |

About 16 MB to download and 90–120 MB on disk. Rebrickable asks for at most one automated download a day; the tool
enforces that. *Data: Rebrickable.*

```bash
wms-go lego catalog status          # what is loaded, per file
wms-go lego catalog refresh         # download (or reload what changed)
```

## How lookups fall back

Every search and add uses the same chain and **says where the answer came from** in the screen title:

```mermaid
flowchart LR
    A[offline catalog] -->|not found| B[live Rebrickable<br/>only if you set a key]
    B -->|down or not found| C[old imported lookup files]
    C -->|nothing| D[type it yourself]
```

If a live source is down or rate-limited you get a note ("Rebrickable is unreachable...") and the offline answer, never a blank screen.

## No internet at all: load the files by hand

Download the `.csv.gz` files from <https://rebrickable.com/downloads/> on any machine, copy them into a folder, then:

```bash
wms-go lego catalog refresh --from-dir /path/to/folder
```

Files that aren't there are skipped. Nothing is downloaded and the once-a-day limit doesn't apply.

## Safe by construction

- A refresh is **all-or-nothing**: files are downloaded first, then loaded in one transaction. A failed or truncated download,
  a changed file format (columns are matched by name), or a file that suddenly has far fewer rows leaves your old catalog exactly as it was.
- Search is full-text (prefix and multi-word): `bri 2x4`, `brick 2 x 4` and `3001` all work.

## Your old lookup files

`wms-go lego import` reads the legacy `legolookup.json` / `legolookup-part.json` (used as a last-resort fallback) and your old collection exports.

## Backup

The catalog can always be downloaded again; **your collection** can't. `wms-go lego backup` writes a small consistent copy of just your
sets, parts and history (the catalog tables are left out); `--encrypt` seals it with a passphrase. See [Backups](backups.md).
