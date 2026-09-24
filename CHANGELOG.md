# Changelog

All notable changes. The format follows [Keep a Changelog](https://keepachangelog.com/); versions follow [SemVer](https://semver.org/).

## [Unreleased]

### Added
- **Reports**: printable "clean form" reports — missing parts (one set or every incomplete set), full collection, one or
  more sets' full parts lists, and stock-check history — plus a blank printable stock-check sheet. `wms lego report
  missing|collection|set|history`, `wms lego stocksheet`; Set Workshop → **7 Reports** in the TUI.
- **Metrics**: an opt-in `/metrics` endpoint (`WMS_METRICS_PORT`) for Prometheus — sign-ins, 2FA outcomes, exports and
  downloads (all via the audit log), telnet connections/throttling, and notification deliveries.
- **Perceived speed**: the BrickLink/BrickOwl price fetch on Missing Parts runs in the background with a spinner instead
  of freezing the screen; `wms lego value --refresh` shows one on the CLI too. Benchmarks added for `CanBuild`, the xlsx
  export and the sorting sheet, to catch a future regression.
- Security audit published: [docs/guides/security-audit.md](docs/guides/security-audit.md).
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
