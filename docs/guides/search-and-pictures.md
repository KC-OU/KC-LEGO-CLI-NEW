# Search, detail pages and pictures

## Search

**LEGO → Search Sets / Search Parts** (or `wms-go lego search sets|parts|minifigs <words>`). Words match in any order and by prefix;
part numbers work; `2x4` is understood as `2 x 4`. Exact matches come first.

--8<-- "docs/assets/screens/set-search.html"

Press **`/`** on any list to filter what is shown (every word must match, in order, so `bk 2x4` finds *Brick 2 x 4*). **Enter** keeps the
filter, **Esc** clears it, **R** refreshes without losing it. **D** opens the detail page for a number.

## Detail pages

**LEGO → Part / Set Detail** (key **D** on the LEGO menu and on result lists, or **Ctrl-K** then type a number). Type a part number, a set
number (`75192`, `75192-1`, or `set 75192`), or an element ID.

--8<-- "docs/assets/screens/set-detail.html"

A set page shows the picture, theme, year, pieces, whether you own it, its minifigures and how much of it your loose parts cover.
A part page shows the colours it comes in, what you hold (with `LOW` marks), and how many sets use it. Keys: **A** add it to your
collection, **M** missing parts (sets), **P** fetch the BrickLink price (one call, needs [BrickLink](bricklink.md)).

--8<-- "docs/assets/screens/part-detail.html"

## Pictures

Pictures are drawn as **text**, so they work over telnet, in Termux, and in the web terminal:

- **Half-blocks** (`▀` with two colours per character) in any colour terminal.
- **ASCII shading** when colour is off (`NO_COLOR`) or you ask for it.

Set `MODERNWMS_TUI_IMAGES` to `auto` (default), `blocks`, `ascii`, `kitty` or `off`. Pictures are downloaded **once** from
Rebrickable's image host, cached (up to 200 MB, least recently used removed first) and shown from the cache afterwards, so
they also work offline once seen. Only `https` from an allow-list of hosts, PNG or JPEG, capped in size and pixels.

!!! info "Kitty graphics"
    `auto` now picks real Kitty-protocol images, not half-blocks, but **only on a genuinely local terminal session** — one
    that isn't telnet or the web gateway (neither carries the protocol: telnet can't, and the web terminal's xterm.js doesn't
    implement it) — and whose emulator identifies itself as Kitty, Ghostty, WezTerm or Konsole. Everyone else keeps
    half-blocks, which still look good enough to recognise a part or a box. Sixel still isn't offered (no session here runs
    a Sixel-capable terminal to test against). Each redraw re-sends the image fresh rather than trying to keep a placement
    alive across the TUI's own full-screen redraws — if it ever looks glitchy in your terminal, set `MODERNWMS_TUI_IMAGES=blocks`
    and it's exactly as if Kitty mode didn't exist.

A page is sized to your window (it fits a plain 80x24 telnet window; larger windows get a bigger picture).
