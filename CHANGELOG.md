# Changelog

All notable changes. The format follows [Keep a Changelog](https://keepachangelog.com/); versions follow [SemVer](https://semver.org/).

## [Unreleased]

### Added
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
