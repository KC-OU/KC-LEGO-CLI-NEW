# Missing parts, prices and orders

## What a set is short

*Set Workshop → 2 Completion dashboard* lists every checked set with a progress bar, what is missing and on order, what you
have spent on it, and who checked it last. **Enter** opens a set's missing parts:

| Key | What it does |
|-----|--------------|
| **P** | fetch prices: **BrickLink** average (your API keys) and **BrickOwl** (set `BRICKOWL_API_KEY`); the cheapest is shown |
| **Enter** | where to buy it: links and a **QR code** for BrickLink, BrickOwl, Rebrickable and LEGO **Pick a Brick** (by element id) |
| **T** | take it from spares you hold elsewhere |
| **O** | start an **order** for everything still short |
| **W** | the shopping list as a BrickLink wanted list (XML upload), Excel, CSV, JSON or a web page |

Prices are stored for a week, so looking again costs no API calls. LEGO Pick a Brick and Rebrickable have no price API, so
those are links. `wms-go lego shopping 75192 --prices` does the same from the shell.

BrickOwl: create a key at *brickowl.com → My Account → API* and put it in `BRICKOWL_API_KEY` (the live price lookup needs
BrickOwl to approve the key for catalog access). `BRICKOWL_COUNTRY` (default `GB`) picks the sellers.

## Orders

*Set Workshop → 4 Parts orders* (or **O** on the missing list). An order records:

- where from: `bricklink`, `brickowl`, `pab` (Pick a Brick), `rebrickable` or `other`, and the store / seller name;
- order number, **invoice number**, **tracking number** and carrier (all optional);
- currency and **shipping**;
- lines: part, colour, quantity and the **price you paid** each (**P** on a line). One order can cover several sets.

Move it along with **O** ordered, **S** shipped, **R** received (or **R** on a single line for a partial delivery).
Receiving puts the parts into the set they were for — its missing count drops, its Part-DB lot is updated, and when nothing
is missing the set is **COMPLETE** and you get an alert.

```bash
wms-go lego orders new --kind brickowl --supplier "Bricks R Us" --shipping 2.50 --from-set 75192
wms-go lego orders status 3 shipped
wms-go lego orders status 3 received
wms-go lego orders list --status open
```

## Spend

*Set Workshop → 5 Spend report* totals parts and shipping **by set, supplier or month** (keys 1, 2, 3; **X** exports it).
Shipping is shared across an order's sets by their share of its parts. `wms-go lego spend --by supplier`.
