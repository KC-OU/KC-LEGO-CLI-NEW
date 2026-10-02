# Changelog

All notable changes. The format follows [Keep a Changelog](https://keepachangelog.com/); versions follow [SemVer](https://semver.org/).

## [Unreleased]

### Security
- **Web terminal: tmux prefix-key escape to a root shell.** The shared web terminal (`/`) runs
  inside a `tmux` session for persistence across reconnects; tmux's own prefix key (Ctrl-B by
  default) was still live inside that session, so anyone with the web terminal open could press
  Ctrl-B then `c` and get a brand-new tmux window running a root shell — tmux handles its own key
  bindings before `wms tui` ever sees the keystroke, so none of the app's permission checks were
  ever in that path. Fixed by disabling the prefix key (and tmux's own mouse handling) scoped to
  just that one session (`set-option -t wms-web`, never `-g`/global, so an admin's own unrelated
  tmux sessions on the same box are untouched). `/solo` and telnet were never affected — neither
  ever wraps in tmux. See [Telnet and the web terminal](docs/guides/telnet-and-web.md).

### Added
- **LEGO Collection/Part-DB browsing on the picker/checker hub**: both now show there for
  searching and looking things up (never editing) — `checker`/`picker` already carried the read
  permissions by default, they just had no menu path to them before.
- **Menu tabs checklist** (*Admin → Access Control → Users → B*): per-user on/off for Part-DB Hub,
  Operations (view-level), Script Hub and LEGO Collection — real access control, not cosmetic
  hiding, each backed by the exact permissions that already gate the tab. **Admin** is the one row
  this screen can only turn *off*, reflecting whatever admin-ish permission a person already has
  from their role/group/overrides — fixes a real report of a `checker` account showing `9=Admin`
  with no admin permission named anywhere; granting admin back stays a deliberate choice via the
  permission grid or Groups, never a single checkbox. Replaces the narrower LEGO/Part-DB-only
  browse toggle shipped a few commits ago.
- **Request a new theme** (*My display theme → R*): send an admin a theme you found (name + a
  link/description) — reaches *Admin → Recent Activity* and any configured notification channel
  (Discord, Slack, ...), the same infrastructure every other admin-relevant event already uses.
- **More label export formats**: `--format png` (a pixel-exact rasterized image — sidesteps the
  PDF-viewer "Fit to page" scaling mismatch that caused a real print failure), `--format zpl`
  (Zebra Printer Language, for Zebra-style hardware), and `--format lbx` (a real Brother P-touch
  Editor file — native QR/Code128 objects, not a rasterized image, so Brother's own software
  trusts the dimensions completely; lower confidence than the others since Brother has never
  published a spec for it, built from community reverse-engineering — test-print before trusting
  it). `png`/`zpl`/`lbx` are one label at a time, no sheet stock; picking exactly one set at a
  non-sheet size in the terminal now **asks which format(s)** you want (PDF alone, or also PNG/
  ZPL/`.lbx`, or all of them) instead of always generating every extra format silently.
- **Brother QL-600, 38mm** (`--size 38x90`) alongside the existing 62mm label sizes.
- **Live/Test picker on the gateway**: with `WMS_TEST_BINARY_PATH`/`WMS_TEST_ENV_FILE` set (off by
  default, and never on the test instance itself), the already-exposed telnet/web gateway offers a
  second, dev-build session reading the test instance's own data — no new network port. See
  [Telnet and the web terminal](docs/guides/telnet-and-web.md#reaching-the-dev-build-from-elsewhere-the-livetest-picker).
- **Force off a job** (*Admin → Assign Work*): take a claimed ticket away from whoever has it —
  to the open queue or straight to a named person — and always send a reassurance message (a
  default quick-pick, or your own). Never touches accuracy; the underlying check/order draft is
  untouched. If the person's mid-job right now, their own screen notices within ~15s and returns
  them to their hub with the message waiting.
- **Admin → Recent Activity**: a scannable feed (last 50) of messages sent, accuracy docked or
  escalated, missing-parts, and forced-off/reassigned jobs — separate from the tamper-evident
  audit log, and (when configured) also routed to the existing notification channels.
- **Shelf-order pick lists**: a check/order's lines now display in Part-DB shelf-location order
  instead of catalog order.
- **Fix**: the shared "type to narrow" picker (Assign Work, Message a User, Dock Accuracy, …)
  couldn't be searched by a numeric username — any parseable number was always read as a row
  choice, not a search term. Typing a numeric id now falls back to a label search when it isn't a
  valid row number.
- **`/solo`, a second web terminal**: the default web terminal (`/`) is one shared session for
  every tab/device on purpose (see its own doc section) — that's what was actually behind "two
  people can't use it at once without slowing each other down," not the gateway or the database.
  `/solo` runs a completely independent `ttyd` process with a fresh `wms tui` per connection
  (the same isolation telnet already had), so a second person — or one person testing as a second
  account — can use the web terminal at the same time as whoever's on the shared one. Trade-off:
  no persistence there, a dropped `/solo` connection is a fresh sign-on next time, 2FA included.
- **Assigned work for pickers and checkers**: a new `picker` role alongside `checker`, each with a
  dedicated hub (Overview, Request, My Current Jobs, a Query submenu, My accuracy, My Exports).
  Admins assign a set/order to a named person or the open queue (*Admin → Assign Work*); claiming
  one just opens the normal check/order screen — a barcode on a printed ticket claims it too
  (palette: "Scan a job ticket"). Leaving a ticket-backed job mid-way asks save-and-return,
  finish now, or abandon without saving (needs a second admin's sign-on and a reason; fully
  audited, never touches accuracy).
- **Picker/checker accuracy**: starts at 100% each day, deducted per completed check/order by how
  many parts were missing (linear within 1–5/6–15/16–20 bands), recovered partially by a
  following clean one; 21+ missing never auto-deducts, it's flagged for a manual conversation.
  Docking is always an admin action with a reason (*Admin → Dock Accuracy*), never automatic.
  *My Accuracy* (also in the palette) shows today's number and the last 7 days, per role.
- **Admin → user messages**: a one-way note (quick picks or your own text) that shows up
  **full-screen**, the same "read it, press a key to clear it" page the first-run tour uses — no
  cramped popup, the whole message is always readable. Shows up right at sign-on if one was
  waiting (not up to 15s later on the next background check), pages through several one at a
  time, **Enter** for the next, **Q** dismisses all of them at once.
- **My Rebrickable API key** (My Settings, or the palette): use a personal key instead of the
  shared one, or switch back with an explicit menu option — no magic keyword to remember.
- **My Exports** (My Settings, or the palette): your own recent exports, with a one-key "get me a
  fresh download link" for one whose link or file has expired.
- **Shift handover note** (*Admin*): one free-text note for whoever's on next, shown at sign-on.
- **Live Sessions** (*Admin*): who's connected, from where, on what screen — also the tool that
  actually answers whether two sessions really block each other, instead of guessing at it.
- **V** key, switch to Admin: a checker/picker whose own account isn't also an admin is asked for
  a *different* admin account's username and password — deliberately no 2FA code, this session is
  already signed in — and switches to it; pressing **V** again returns to exactly where they were,
  no credentials needed going back down. An account that already holds both just jumps straight
  there and back (mirrors **G**'s Main Hub ↔ LEGO Collection). Every switch is audited.
- Picking a user for Assign Work / Message a User / Dock Accuracy now also matches on their
  numeric user ID, not just their username, while narrowing the list as you type.
- Fixed a real Part-DB deadlock, found while investigating sessions slowing each other down:
  `looseLots` looked up a part's stock locations (a second query) while its own first query's
  rows were still open — invisible with an unlimited connection pool, but a guaranteed hang the
  moment anything constrains it to one connection. Reordered, with a regression test that forces
  a single connection and fails in 5s if it regresses, instead of hanging for minutes. (A
  same-process `SetMaxOpenConns(1)` was tried as a further fix for the original complaint — the
  gateway itself has no shared lock between sessions, so this was the next suspect — but reverted:
  this one instance is fixed and tested, but `internal/lego` has ~40 more query-loop sites an
  afternoon can't responsibly audit for the same pattern, and a silent production deadlock is far
  worse than "feels slow." Live Sessions remains the actual diagnostic tool for this.)
- **A `checker` role**, and a real fix for a genuine freeze: Finish Check, Receive, and a detail-screen BrickLink
  price look-up all ran a multi-minute Part-DB/network call directly inside key handling with zero feedback,
  blocking the session until it returned or timed out. All three now run through the existing background-job
  spinner, which is also now used everywhere else a call can be slow (sign-on, docker status, a Rebrickable
  look-up) and is toggleable per user (Settings → My loading indicator, on by default — turning it off only
  hides the spinner, the underlying fix always applies). Also added a read-deadline on the raw telnet socket.
  Fixed two bugs in the first cut of the `checker` role while trying it live: a starter group added to the code
  after an install had already been seeded once never reached it (fixed for future starters too — see Starter
  groups); and if the same username exists in both ModernWMS and Part-DB, sign-in always uses the ModernWMS one,
  so the policy entry has to be on that source, not Part-DB's, or the exemption is never actually read.
- **Colour, then category ordering** now covers every parts list you browse — Owned Parts, a set's parts check,
  and Missing Parts & Prices in Set Workshop — sorted black, red, blue, then alphabetically, then by part
  category within each colour. Tried this with visible banner rows first; real Rebrickable categories are far
  more granular than expected (distinct "Technic Axles"/"Technic Beams"/etc. bands), so on an actual set it was
  mostly banners, and they buried the `▶` selection cursor — reverted to the plain flat table with the improved
  order underneath, no banners. (A set's Missing Parts *report* stays sorted by biggest shortfall for shopping,
  deliberately.)
- **On-screen alert popups**: a set coming up short on a check now also pops up on screen (not just external
  notifications), **Q** to dismiss; queued rather than replacing or being lost if another comes up first.
- **A bordered look for hub/section menus**: the same rounded, theme-coloured panel the sign-on card has always
  used now wraps every hub and section menu too (classic and modern layouts alike) — a menu too long for the
  panel's two extra lines on your terminal falls back to the plain list automatically.
- **Barcode badge sign-in**: a username can be swapped for a scanned (or typed) badge token at sign-on — the
  password (and 2FA) is still required exactly as before. An unguessable, revocable token, never the literal
  username. `wms access user badge <user> [--regenerate|--clear|--qr]`.
- **Optional parts**: sticker sheets default to optional and never count toward missing parts or completion,
  anywhere (Missing Parts report, stock-take totals, a set's INCOMPLETE flag) — on or off, even before you've
  counted them. Any part can be marked optional or required, either way: **O** on the parts check screen, or
  `wms lego optional <part> [on|off]`.
- **Self-serve promotion**: `bash scripts/promote-dev.sh` merges chosen commits from `dev` into `main` and
  deploys them live, gated on a full `wms preflight deploy` (tests, vet, staticcheck, gosec) and a typed
  `CONFIRM` — no separate conversation needed to ship what's already been tried on the test instance.
- **Reports**: printable "clean form" reports — missing parts (one set or every incomplete set), full collection, one or
  more sets' full parts lists, and stock-check history — plus a blank printable stock-check sheet. `wms lego report
  missing|collection|set|history`, `wms lego stocksheet`; Set Workshop → **7 Reports** in the TUI.
- **Metrics**: an opt-in `/metrics` endpoint (`WMS_METRICS_PORT`) for Prometheus — sign-ins, 2FA outcomes, exports and
  downloads (all via the audit log), telnet connections/throttling, and notification deliveries.
- **Perceived speed**: the BrickLink/BrickOwl price fetch on Missing Parts runs in the background with a spinner instead
  of freezing the screen; `wms lego value --refresh` shows one on the CLI too. Benchmarks added for `CanBuild`, the xlsx
  export and the sorting sheet, to catch a future regression.
- Security audit published: [docs/guides/security-audit.md](docs/guides/security-audit.md).
- **Report archive**: every report generated (CLI or TUI) also gets a 90-day-retained copy, separate from the
  normal 7-day export lifecycle — "the QR/link expired, can I still get that report" now has an answer.
  `wms lego report archive list|get`; Set Workshop → 7 Reports → 7 Archive.
- **Reports restructured**: six reports (Parts Stock-take, Set Parts Lists, List of Sets, Missing Parts, Extra Parts,
  Order List) replacing the earlier four-report menu — Extra Parts and Order List are new, reading `part_origins`
  and the existing orders data respectively. `wms lego report stocktake|setparts|setlist|missing|extra|orders`;
  Set Workshop → 7 Reports offers all six.
- **Share links**: a read-only, multi-use page for someone without the CLI — your BrickLink watch list
  (`wms lego watch`) read as a wishlist, or your full collection — viewable until it expires, not consumed on
  first view like a download link. `wms lego share wishlist|collection`. See
  [docs/guides/share-links.md](docs/guides/share-links.md).
- **Discord export notifications**: any export can also be DMed to Discord as a real bot message — the QR code as
  an actual image attachment, plus the link, with its own expiry chosen at send time (not tied to the site-wide
  download-link default). `--discord`/`--discord-expires` on every export command; **D** on the TUI's export result
  screen. See [docs/guides/discord-notifications.md](docs/guides/discord-notifications.md).
- **Retirement dates**: reads a community-maintained spreadsheet (Rebrickable has no such field) and flags what's
  retiring soon among your owned sets and BrickLink price watches, or just browses everything. Fail-soft by design —
  a manual CSV import is always available if the live sheet is ever unreachable. `wms lego retirement
  refresh|import|list`; Set Workshop → **8 Retiring soon** in the TUI.
- **Onboarding**: a first-run tour (three short screens) on an account's first sign-in, shown once and skippable
  (Esc); empty-state hints on the screens most likely to be genuinely empty for a new account (Set Workshop, Missing
  Parts, Orders, Owned Parts, Reports); and a printable key cheat sheet built from the same F1 help content —
  `wms lego help-sheet`, Set Workshop → 7 Reports → 8 Key cheat sheet.

### Fixed
- **Web terminal**: switching tabs or reconnecting no longer asks for 2FA again or loses your place — the session now
  persists in a `tmux` session across reconnects (still ends on explicit sign-out). See
  [Telnet and the web terminal](docs/guides/telnet-and-web.md#the-web-terminal-is-one-persistent-shared-session).

- **Set checks**: adding a set opens its whole parts list with every line marked have-all; mark what is **missing** (M) or
  **extra** (E), type counts, filter, undo, save for later. Finishing records who checked it, puts the set's parts into
  Part-DB in their own location (`LEGO / Sets / <num> <name>`, synced on to ModernWMS), turns extras into loose parts that
  remember their set (other sets short of that part can take them), and flags the set **INCOMPLETE**. Stock checks recount
  a set, with a **barcode-scanner mode**. `wms lego check`.
- **Missing parts and orders**: BrickLink and **BrickOwl** prices, shop links and QR codes (BrickLink, BrickOwl, Rebrickable,
  Pick a Brick), a shopping list export, **orders** with supplier, invoice and tracking numbers, shipping and prices paid,
  through to received (which completes the set). **Completion dashboard** and **spend report**. `wms lego shopping`,
  `wms lego orders`, `wms lego spend`.
- **Labels** for thermal printers (4x6, 100x150, Brother QL 62 mm, 50x30, 40x30) and A4 / Letter sheets, with QR code and
  Code 128 barcode, as PDF and HTML. `wms lego labels`.
- **Control-room sign-on and dashboard**: logo, live system status, collection figures, alerts, message of the day, clock,
  last sign-in and failed attempts since. `wms motd`.
- **Notifications** to Discord, Slack, Telegram, Teams, Google Chat, email, Pushover, Gotify and webhooks (WhatsApp),
  configured with a projectdiscovery/notify provider file and routed per event. `wms notify channels|route|test`.
- **`wms publish`**: one new commit on top of the public repository; a much faster preflight (parallel checks, cached tools,
  cached tests). Docs are hosted by GitHub Pages only.
- Set location and condition (`wms lego set-info`), a sorting-friendly parts list per set.
- **Access control** ([guide](docs/guides/access-control.md)): groups with a Part-DB-style allow/deny grid (12 areas), per-user
  overrides, six starter groups (admin, operator, viewer, builder, exporter, stock-clerk). Hidden options, audited denials. A 2FA policy
  per user (required / exempt / default), exemptions limited to restricted groups, networks, channels and an expiry date. Per-user and
  global 2FA remember window, idle lock and a new maximum session length; export retention and download-link lifetime. Managed in
  *Admin → Access Control* and with `wms access`. Users without an entry keep their ModernWMS role's access.
- **Export from the interface (X)** on Missing Parts, Part / Set Detail, Owned Parts and What Can I Build: JSON with pictures embedded,
  a real **Excel .xlsx** (picture link per part), CSV, a web page with pictures, BrickLink and Rebrickable lists.
- **QR-code downloads**: an export made over telnet shows a one-time, 15-minute https link as a QR code to scan onto your phone
  (`WMS_PUBLIC_URL`; served by the web gateway at `/dl/`, audited).
- `wms lego export --format xlsx`, `--with-images`; `wms lego detail <part|set> --export json|xlsx|html`.
- **Themes**: nord, gruvbox, catppuccin, tokyo-night, ibm-3270, matrix, lego, dracula and half-life; a **live-preview picker**, and
  **per-user themes** (Ctrl-K → My display theme).
- **Achievements** (Collection Stats → A, `wms lego achievements`) and **Set of the Day**, a daily build challenge (LEGO hub → T,
  `wms lego today`).
- A themed **sign-on animation** (skippable; `MODERNWMS_TUI_SPLASH=0`), and the `export_done` plugin event.
- `GATEWAY_LISTEN_HOST` (default `127.0.0.1`): telnet and the web terminal no longer listen on every interface.
- **Offline-first LEGO data.** The whole Rebrickable catalog (parts, colours, sets, themes, minifigures, every set's parts list,
  part equivalences) is kept locally; search, adding parts and sets, and missing-parts all work with no API key and no network.
  `wms lego catalog refresh [--from-dir]`, `wms lego search`.
- **Lookup-first add flow**: type a part number, pick a colour, enter a quantity; written to Part-DB through its REST API.
- **BrickLink API** (lookups by number, price guide, where-used), collection value, price watch with alerts.
- **Pictures** of parts and sets as text (half-blocks or ASCII), detail pages, element-ID lookup.
- **What can I build?**, alternates/moulds in missing-parts, history with daily snapshots and restore, a Ctrl-K command palette.
- **Plugins** (pinned, sandboxed, audited) and **export** to Rebrickable CSV, BrickLink XML, Excel CSV, JSON and printable HTML.
- Security hardening (single-use 2FA codes and lockout, hash-chained audit log, telnet throttling, idle lock), `wms doctor`,
  encrypted backups, alerts, accessibility themes, `--json`/`--quiet`/`--dry-run`/`--yes` and exit codes.
- `wms version` and `wms update`.

- **`wms menu` (alias `q`)**: a compact launcher for every shell task (status, logs, restarts, backups, users, LEGO, shipping) with
  filter-as-you-type, pinned favourites, recents, a live status line, yes/no before risky tools and an audit line per run; the TUI
  **Script Hub** runs the same tools (admin-gated, audited) inside telnet and web sessions. Your own tools go in `tools.json`.
- **`wms preflight`**: an animated audit that ends in GO or NO-GO for deploy and publish (code, secrets and personal data in the
  exported tree, clean build, docs, live machine, backups). `scripts/deploy.sh` and `scripts/publish.sh` refuse to run without a
  fresh GO for the same commit.
- `wms sys status|banner|audit|connect|guide`, and a 38-line `scripts/bashrc.sh` (one-line login banner) with an installer that
  backs up the old file.

### Changed
- Every `wms` command used to ask the terminal for its background colour at start-up (bubbletea does this) and could stall for
  five seconds on a terminal that never answers; that probe is now suppressed.
- Public addresses and the viewonly seed account are settings, not compiled-in values.
- On a form, **Q is an ordinary letter** (a username, password or search starting with q can be typed one key at a time over
  telnet); type `q` and Enter in the first field, or press Esc, to leave.
- Quantity fields that start with a suggested value are **replaced by the first character you type** (typing 12 over a
  suggested 1 gives 12); Backspace edits it.
- Screens fit an 80x24 terminal: long hints wrap, long messages end in an ellipsis, wide tables narrow their widest columns,
  long lists page (PgUp/PgDn/Home/End), the colour list and detail pages size to the window. A telnet client that never
  reports its size is treated as 80x24.
- `lego.db` uses SQLite WAL mode so searching keeps working while the catalog refreshes; back it up with `wms lego backup`.
- Part numbers are validated (letters, digits, `.` `-` `_`) where you or a file supply them.
