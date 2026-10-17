# Set checks and stock checks

When you add a set, go through its parts so you know it is complete — and so a stock check or audit later has something
accurate to compare with.

--8<-- "docs/assets/screens/workshop.html"

## The parts check

Add a set (*LEGO → 4 Add / Update a Set*) and answer **yes** to *Check the parts now?* — or open it any time: *LEGO → 8 Set
Workshop → 1*, **K** on a set's detail page, or `wms-go lego check <set>`.

The whole parts list opens, **every line already marked as have-all**, so you only mark the exceptions:

| Key | What it does |
|-----|--------------|
| **M** | this line is **missing** some — type how many (default 1) |
| **E** | you have **extra** of this part — type how many |
| **0-9** / Enter | type how many you have (more than the set needs = the rest are extras) |
| **H** | have all of this line again; **A** every line |
| **T** | take it from **spares** you hold elsewhere (loose parts or another set's extras) |
| **/** | filter by part, colour, name, category — or type `missing` / `extra` |
| **Z** | **scanner mode** (below) |
| **U** | undo · **S** save for later (resume where you left off) · **F** finish · **Esc** leave |

A **MISSING N / EXTRA N** badge sits at the top of the screen the whole time — red for missing, blue for
extra — the check's running totals, updated the moment you mark a line, in both the table and the
**W** guided-walk view. Shown even at zero, so you never have to scan the whole list (or ask someone
else) to know where a check currently stands. The mobile app carries the same two numbers on every
line (`total_missing`/`total_extra`) for its own equivalent.

The table view's own Miss/Extra columns carry the same colours per line — red/blue, not just plain
text — so scanning down the list itself (not only the badge at the top) shows at a glance which lines
still need attention.

On **F**:

- the check is recorded with **who did it and when**;
- the set's parts go into **Part-DB** in their own storage location, `LEGO / Sets / <number> <name>` (one lot per part and
  colour, reusing parts you already have) — the existing sync carries them to ModernWMS;
- **extras** become loose parts that remember the set they came from, so another set that is short of that part shows
  `2 spare (from 10696)` and **T** moves them across;
- a set with anything missing is flagged **INCOMPLETE** everywhere (lists, detail page, dashboard, labels) and an alert goes
  out (see [Notifications](notifications.md)).

## Stock checks

**K** on a checked set (or `wms-go lego check <set> --stocktake`) is a **recount**: the same screen, starting from what the
last check found. Differences are applied to the database and the set's Part-DB lots, and the set page shows
`Last checked 22 Sep 2026 by alex (recount)`.

### Barcode scanner mode

Press **Z** and scan (or type) a part number or LEGO element id followed by Enter — a USB barcode scanner does exactly that.
The matching line goes up by one and flashes; a part that is not in the set, or a line already complete, beeps. **Z** or
**Esc** leaves scanner mode.

A scanner needs something to scan: **P** prints a barcode sheet for the open check — one row per part, a Code 128 barcode,
its name, shelf location and how many are needed — so you're not relying on each part's own retail packaging. From the
shell, `wms-go lego parts-sheet <set> -o sheet.pdf` prints the same sheet without opening a check at all (read-only; it
never changes anything). The two differ in where the file lands: **P** in the TUI saves it through the same
[exports mechanism](export-import.md) everything else does, so it also shows up on the **[exports dashboard](export-import.md#download-it-with-a-qr-code)**
for grabbing from your phone — the CLI's `-o` just writes the file wherever you point it, since a shell session already has
direct filesystem access and has no need for a download link.

By default it's an A4 sheet, several parts per page. `--size` takes any stock [`wms lego labels`](labels.md) does, so
small sticky/thermal labels (`50x30`, `40x30`, `62x29`, ...) print **one part per label** instead — peel one straight
onto a bin or box. `--format html` also draws each part's picture (the default PDF is vector-only — text and barcode,
no images at all): `wms-go lego parts-sheet 75192 --size 50x30 --format html -o falcon-labels.html`, then print it from
a browser at 100%, no margins, same as any other picture-bearing export.

Every barcode here is held to a minimum module width a 203 dpi thermal head can actually resolve, even when that means
letting it run slightly wider than its nominal box — a barcode that overflows its box but scans beats one that fits and
doesn't. Very long content (a long set number) on the smallest stock is a real physical limit no software fixes
outright: pick a size with more room, or a shorter `--barcode` value on `wms lego labels`, if one specific label still
won't scan.

Restocking several incomplete sets at once? `wms-go lego missing-sheet <set> [<set>...]` combines everything **missing**
(not the full parts list) across every set given onto one sheet, so one shelf walk covers all of them instead of
printing and carrying a separate sheet per set. Each set needs a check on record already; one that doesn't is skipped
with a warning rather than failing the whole run. Same `--size`/`--format` as `parts-sheet`.

### Guided walk and pictures

Press **W** for the guided view: one line at a time with a big "GO TO: &lt;location&gt;" banner, instead of the full table —
meant for walking the shelves rather than reading a list on screen. Turn on **Show/hide pictures while checking or picking**
(*My Settings*, or the palette — off by default) and this guided view also shows a colour swatch next to the colour name and
the part's (or set's) picture underneath, the same [half-blocks/ASCII rendering](search-and-pictures.md#pictures) used
everywhere else — meant for anyone who finds a picture and a colour swatch faster to confirm against than reading "Dark
Bluish Gray" or a part number off the screen.

## Where the set is kept

The add/confirm form and *Completion dashboard → I* record a **location** (shelf, box, bin) and a **condition** (sealed,
built, in pieces, displayed). Both appear on labels. CLI: `wms-go lego set-info 75192 --location "Shelf B2" --condition built`.

The same screen has an **image URL override** — when Rebrickable's own picture for a set is wrong or missing, an admin
can replace it there (`--image-url` from the shell; blank clears it, back to the catalog's own). It's picked up
everywhere that set's picture shows — the TUI, label sheets, the mobile app's check/pick screen — the next time any of
them loads it, with nothing to re-sync by hand.

## Bags: small, main, extra — and scanning the right one

Some parts are too easy to lose loose in with everything else — a loose 1x1, a minifig accessory.
Mark them once:

```bash
wms-go lego bag-size 3024              # show where it's currently filed
wms-go lego bag-size 3024 small        # goes in the small bag from now on
wms-go lego bag-size 3024 main         # back to the ordinary bag
```

Every part defaults to the main bag until marked otherwise. `small`, `main`, and `extra` are the
three built-in **bag kinds** — a global registry, not a per-set setting — and an admin can add more
(`wms-go lego bag-kind add wheels "Wheels & Tyres"`) for anything that deserves its own container
across every set. `wms-go lego bag-kind list` shows what exists.

Every set gets a bag **instance** per kind it actually needs: `75192-A` (small), `75192-B` (main),
and — only once a checker actually finds something extra, never printed up front — `75192-E`
(extra). `wms-go lego bag-labels 75192` prints every label that exists for a set so far in one run;
`wms-go lego small-bag-label 75192 BAG-0042 -o b.pdf` still works for the single small-bag label,
with its own chosen code, same as before.

**By default, every set requires scanning the right bag before a part in it can be marked.** In the
TUI, press **B** to scan or type a bag's code (the same barcode-wedge entry as **Z**'s part scanner);
checking any line first checks that the right bag is active. Switch bags freely mid-session — small
parts into the small bag, big ones into main, back and forth — there's no restriction on which order,
only that *some* scan of the matching kind has happened before that line can be marked. Scanning a
bag's code is also enough to open or resume its set's check directly, without already knowing the
set number. The mobile app's `/mobile/scan-bag` does the same thing for an already-claimed check, and
`/mobile/confirm` takes an optional `bag_code` to scan and mark in one request.

Scanning the *wrong* bag (right set, wrong kind) never blocks anything — it's a warning ("wrong bag —
scan 75192-A") that you can see and override, logged quietly for the audit trail, not scored against
accuracy. Scanning a code from a different set names which one it belongs to; a code that matches no
bag at all is a plain "unknown bag code" error. **Finishing a check holds** until every bag kind that
check actually needs has been scanned at least once — small and main always, extra only if something
was actually marked Extra during the session — the same `409`-and-structured-summary shape the old
single small-bag prompt used, just covering every bag instance instead of one.

A set normally gets one instance of each kind (one small bag, one main bag). A huge set that needs a
second small bag, say, needs an admin to raise that set's limit first:

```bash
wms-go lego set-info 75192 --max-bags 3         # this set can have up to 3 instances of a kind
wms-go lego set-info 75192 --bag-gating off     # turn the scan requirement off for this one set
```

Both are admin-only, gated the same way the **V** key's Admin/Picker-Checker toggle already is — no
extra password or 2FA step, just the existing permission check. A checker who needs a second instance
of a kind tells an admin, who raises the limit, then registers and prints the next one:

```bash
wms-go lego set-info 75192 --max-bags 3     # was 2; this set can now have 3 instances of a kind
wms-go lego bag-add 75192 small             # registers + prints "75192-A2"
```

Gating only applies to **new** checks started after it shipped — nothing changes for a check already
in progress — and only to the interactive TUI/mobile flows; `wms-go lego check` from the shell stays
the non-interactive admin bypass it always was, no bag scanning involved.

### A reusable bag pool

Printing a fresh one-off code for every check is the default, but if you'd rather keep a handful of
real, permanent containers (labeled bins, say) and hand them out instead, register each one under its
own stable code and assign it to whichever set currently needs it:

```bash
wms-go lego bag-pool add BIN-07 small          # a real bin, registered once
wms-go lego bag-pool assign BIN-07 75192       # handed to 75192's current (or last) check
wms-go lego bag-pool list                      # see what's out and what's free
```

A pool bag scans and gates exactly like any other bag once assigned — nothing about scanning, warnings,
or the finish gate treats it differently. It's freed back to available automatically the moment that
check finishes, ready to hand to the next set. `wms-go lego bag-pool release <code>` frees one by hand
(a check abandoned rather than finished, say).

### Reprinting a damaged or unscannable label

`wms-go lego bag-labels <set>` is always safe to run again — it reads every bag actually registered
on the set (a pool-assigned bag's own code, a second instance, a custom kind, included) rather than
regenerating codes from scratch, so rerunning it after a label gets damaged, smudged, or just won't
scan prints the exact same labels, byte for byte. The Bluetooth print page and `/mobile/print/bags`
work the same way — pick the bag from the list and print it again, as many times as you need.

### Finding a lost bag

`wms-go lego bag-find <code>` looks up any code — a per-check instance (scanned in or not, on a
finished check or a live one), a pool bag's own identity, or even a label that was printed but never
actually scanned into anything — and says what it belongs to, independent of any active checking
session. Useful for a bag that turns up without its checker, or for double-checking a label before it
goes on. The Set Workshop's **F** key does the same from the TUI, and the mobile app has
`GET /mobile/bag-find?code=...`. Unlike scanning a bag into a check, this never registers or marks
anything — it only reports what's already known.

An admin scanning or typing a bag's code also sees **who printed it and when**, and — once its check
is finished — that check's own result (missing/extra counts, and whether it was flagged disputed):
"what mess-ups did this bag make", without a separate incident log to maintain. Everyone else just
sees what the bag is, same as before.

### Per-bag breakdown

The **Completion dashboard** (*Set Workshop → 2*) splits an incomplete set's shortfall by which bag
each short part actually belongs in — "Main 5 · Small Parts 3" — instead of just one set-wide missing
count, so you know which physical bag to go looking in first. The missing-parts report
(`wms-go lego report missing`) and the extra-parts report (`wms-go lego report extra`) tag each row
the same way — a missing row with the bag it'll go in once restocked, an extra row with the dedicated
extra bag — shown as their own column when a report actually has one to show.

### Printing straight from a Labelnize/Phomemo PM260 over Bluetooth

The CLI command above writes a file; if you'd rather skip the file and print directly from whatever
phone or laptop is actually next to the printer, open `https://<your-mobile-api-address>/mobile/print`
(the same address the mobile app already points at) in **Chrome or Edge** — Safari/iOS has no Web
Bluetooth support at all, and the page needs HTTPS to use Bluetooth in the first place. Sign in the
same way the app does, tap **Connect to printer** and pick your PM260 from the Bluetooth list, then
type a set number — the bag dropdown fills in with whichever of that set's bags exist so far (small
and main always, extra once it's been created) — pick one and tap **Print**. The page talks to the
printer directly over Bluetooth LE — this server is never in the loop, which is why it works wherever
the printer actually sits, not wherever `wms-go` happens to be running. Only the 50x30 and 40x30
small-bag sizes are offered there; for anything else (the 4x6 set label, a sheet of Avery labels) use
the CLI/TUI export as before.

## From the shell

```bash
wms-go lego check 75192 --missing 3001:red:2,3023:1:1 --extra 3710:black:3   # colour = name or Rebrickable id
wms-go lego check 75192 --stocktake --by alex
```
