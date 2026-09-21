# Export and import

## Export

```bash
wms-go lego export --format <format> [-o file] [--set N --missing]
```

| Format | For |
|--------|-----|
| `rebrickable-csv` | `Part,Color,Quantity`: Rebrickable's parts list, and what `import-parts` reads back |
| `bricklink-xml` | your parts as a BrickLink inventory (needs the [colour map](bricklink.md#set-it-up)) |
| `csv`, `sets-csv` | open in Excel or LibreOffice. Text that could be read as a formula is neutralised (`=`, `+`, `-`, `@` are prefixed) |
| `json` | everything, structured |
| `html` | a printable page: open it in a browser and **Print → Save as PDF** |

With `--set 75192 --missing` it exports what that set still needs instead of what you own. Parts in a typed colour have no colour id and are
left out of the Rebrickable and BrickLink formats, and counted in the warnings. `-o` refuses to overwrite unless `--force`.

## Import a parts list

```bash
wms-go lego import-parts my-parts.csv --dry-run          # the plan first
wms-go lego import-parts inventory.xml --mode set --yes
```

Reads a **Rebrickable parts-list CSV** or a **BrickLink inventory / wanted-list XML** (parts only). `--mode add` (default) adds to what you hold;
`--mode set` replaces it. Names and categories come from the offline catalog. BrickLink colour numbers are translated through Rebrickable
(needs a key). Everything is written in **one transaction**. Nothing goes to Part-DB until `wms-go lego sync-parts`.

## Your old collection

`wms-go lego import --collection 0.json --ref-sets legolookup.json --ref-parts legolookup-part.json` reads the original bash tool's files.
It is safe to repeat.
