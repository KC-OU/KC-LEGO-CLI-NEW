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
| `json` | everything, structured; with `--with-images` each part carries its picture URL, and pictures already cached are embedded (base64) |
| `xlsx` | an Excel workbook: a Parts sheet with a **Picture** link per row, and a Sets sheet |
| `html` | a printable page with pictures: open it in a browser and **Print → Save as PDF** |

With `--set 75192 --missing` it exports what that set still needs instead of what you own. Parts in a typed colour have no colour id and are
left out of the Rebrickable and BrickLink formats, and counted in the warnings. `-o` refuses to overwrite unless `--force`.

### A part's or set's page, with its picture

```bash
wms-go lego detail 75192-1 --export json -o falcon.json   # the set, its picture and every part in it
wms-go lego detail 3001 --color 4 --export html -o brick.html
```

The same page as *Part / Set Detail* in the interface. The main picture is always embedded in `json` and `html` (linked in `xlsx`);
part pictures are linked, and embedded when they are already in the picture cache (an export never downloads one per part).

## Export from the interface (X)

Press **X** on **Missing Parts**, **Part / Set Detail**, **List Owned Parts** or **What Can I Build?**, then a letter:
**J** JSON, **E** Excel (.xlsx), **C** CSV, **H** web page, **B** BrickLink wanted list, **R** Rebrickable CSV (what each list offers).
The file is saved in `WMS_EXPORT_DIR` (default `/root/docker-server/wms/exports`, kept 7 days).

### Download it with a QR code

A file made over telnet or the web terminal is on the server, not your device. When `WMS_PUBLIC_URL` is set (your web terminal's
https address, e.g. `https://lego-tui.example.com`), the export screen also shows a **download link and a QR code** — point your
phone's camera at the terminal and the file downloads.

- The link works **once**, for **15 minutes**, and only for that file. Only a hash of it is stored on the server.
- It is served by the web gateway at `/dl/…`; a wrong, used or expired link is a plain 404.
- Every export and download is in the audit log with the user who made it, and plugins hear `export_done`.

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
