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
| `50x30`, `40x30` | small thermal labels for bags and bins |
| `a4` | A4 sheet, 21 labels (Avery L7160) — HP, Canon, Brother inkjet / laser |
| `letter` | US Letter sheet, 30 labels (Avery 5160) |

## Printing

In the terminal: **L** on a set's detail page, **L** on the completion dashboard, or *Set Workshop → 6* (`all`,
`incomplete`, or set numbers). Pick the size; you get a **PDF** and a web page, with the one-time download link and QR code
([exports](export-import.md#download-it-with-a-qr-code)) — scan it with your phone or open the link on the PC the printer is
connected to.

```bash
wms-go lego labels 75192 --size 4x6 -o falcon.pdf
wms-go lego labels incomplete --size a4 -o sheet.pdf
wms-go lego labels all --size 50x30 --format html -o labels.html
```

Print at **100 % / actual size** with **margins set to none**. For a thermal printer choose the matching label size in its
driver (for the Labelnize PM260: 4 × 6 in, 203 dpi). Barcodes and QR codes are drawn as solid shapes sized for a 203 dpi head,
and are tested to decode at that resolution.
