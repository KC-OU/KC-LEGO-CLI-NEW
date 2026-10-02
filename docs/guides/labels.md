# Labels

Printable labels for your sets, for a thermal label printer or sheets on an ordinary printer.

Each label shows the **set number, name, year, total pieces, what is missing / on order, who checked it and when**, the
**location**, a **QR code** (the set on Rebrickable) and a **Code 128 barcode** of the set number — which the stock-check
scanner mode reads.

| Size | For |
|------|-----|
| `4x6` | 4 × 6 in thermal labels — **Labelnize PM260**, Zebra, Rollo, MUNBYN and most shipping-label printers |
| `100x150` | 100 × 150 mm thermal |
| `62x100`, `62x29` | **Brother QL** 62 mm continuous roll (DK-22205) |
| `38x90` | **Brother QL-600** 38 mm continuous roll |
| `50x30`, `40x30` | small thermal labels for bags and bins |
| `a4` | A4 sheet, 21 labels (Avery L7160) — HP, Canon, Brother inkjet / laser |
| `letter` | US Letter sheet, 30 labels (Avery 5160) |

## Printing

In the terminal: **L** on a set's detail page, **L** on the completion dashboard, or *Set Workshop → 6* (`all`,
`incomplete`, or set numbers). Pick the size; you get a **PDF** and a web page, with the one-time download link and QR code
([exports](export-import.md#download-it-with-a-qr-code)) — scan it with your phone or open the link on the PC the printer is
connected to. Pick exactly **one** set at a non-sheet size and three more files are saved alongside them automatically: a
**PNG** image, **Zebra ZPL**, and a **Brother `.lbx`** — see Formats below.

```bash
wms-go lego labels 75192 --size 4x6 -o falcon.pdf
wms-go lego labels incomplete --size a4 -o sheet.pdf
wms-go lego labels all --size 50x30 --format html -o labels.html
wms-go lego labels 75192 --size 38x90 --format png -o falcon.png
wms-go lego labels 75192 --size 38x90 --format lbx -o falcon.lbx
```

Print at **100 % / actual size** with **margins set to none** — a PDF viewer defaulting to "Fit to page" instead silently
rescales the page to whatever the printer driver guesses, which is the single most common cause of a label printing the
wrong size. For a thermal printer choose the matching label size in its driver (for the Labelnize PM260: 4 × 6 in, 203 dpi).
Barcodes and QR codes are drawn as solid shapes sized for a 203 dpi head, and are tested to decode at that resolution.

## Formats

`--format pdf` (default) and `--format html` support every size, including sheets, and any number of sets at once.
Three more formats are **one label at a time, no sheet stock** — a sheet is genuinely multiple separate pages, and a
single PNG/Brother file can't be that:

| `--format` | What it is | Why you'd use it |
|---|---|---|
| `png` | A pixel-exact rasterized image (203 dpi) | Printed via a plain "Print Picture" dialog, which has no page-size-matching step to get wrong — sidesteps the PDF-viewer-scaling problem above entirely. |
| `zpl` | Plain-text Zebra Printer Language commands | For Zebra-style label printers (not a Brother QL), over a raw socket/USB connection. A batch of sets produces one `^XA…^XZ` block per label, concatenated — not restricted to one at a time like PNG/`.lbx`. |
| `lbx` | A Brother P-touch Editor label file (a ZIP of Brother's own XML) | Brother's own software trusts the file's exact dimensions completely, so there's no print-dialog scaling step at all. **Lower confidence than the others**: Brother has never published a spec for this format, so it's built from community reverse-engineering, not an official reference — a real text block, a real QR code and a real Code 128 barcode as native objects, but a simpler single-block text layout rather than the precise multi-field layout the other formats use. Test-print it on your own printer before trusting it, and expect to report back what needs adjusting. |
