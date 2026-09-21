# KC-LEGO-CLI-NEW (`wms-go`)

A LEGO collection manager for the terminal, built to keep working when the internet doesn't — plus tools for
[ModernWMS](https://github.com/modernwms) and [Part-DB](https://github.com/Part-DB/Part-DB-server) inventories.
Successor to the original bash `KC-LEGO-CLI`.

- **Offline-first.** The whole Rebrickable catalog (parts, colours, sets, themes, minifigures, every set's parts list) lives in
  a local database. Search, adding parts, adding sets and "what am I missing" all work with **no API key and no network**.
- **Lookup-first adding.** Type a part number; name, category and the colours it comes in are filled in. You give colour and
  quantity. Parts go to Part-DB through its API, one Part-DB part per part and colour.
- **Live where it helps.** Rebrickable and the BrickLink API (by number, price guide, where-used) fill gaps and add prices.
- **What can I build?** Sets your loose parts cover, missing-parts lists that count alternates and moulds, BrickLink wanted lists.
- **Pictures in the terminal** (Unicode half-blocks, or plain ASCII), **detail pages**, a **Ctrl-K palette**, **history and restore**.
- **Works over telnet.** A telnet + web-terminal gateway serves the same TUI to scanners, tablets and old terminals.
- **Careful by default.** Single-use 2FA with lockout, a hash-chained audit log, encrypted backups, sandboxed plugins,
  `wms doctor`, and `--json` / `--dry-run` / exit codes for scripts.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/KC-OU/KC-LEGO-CLI-NEW/main/scripts/install.sh | sh
wms-go doctor
```

Also available as `.deb`/`.rpm`/`.apk` packages, a Docker image, an AUR package, or `go install
github.com/KC-OU/KC-LEGO-CLI-NEW/cmd/wms@latest`. Linux and macOS (amd64, arm64).

## First ten minutes

```bash
wms-go lego catalog refresh          # one download (~16 MB); offline from then on
wms-go lego search sets falcon       # try it
wms-go lego add-part 3001 --color red --qty 25
wms-go lego build                    # what could my loose parts build?
wms-go                               # the full-screen interface (Ctrl-K jumps anywhere)
wms-go menu                          # a compact launcher for everyday tasks (alias it to q)
wms-go preflight all                 # is it safe to ship? GO or NO-GO
```

Full documentation: **<https://kc-ou.github.io/KC-LEGO-CLI-NEW/>**

## Documentation and contributing

The docs are Markdown in [`docs/`](docs/) (MkDocs Material); screenshots and the command reference are generated from the
program itself. See [CONTRIBUTING.md](CONTRIBUTING.md). Security reports: [SECURITY.md](SECURITY.md). Changes: [CHANGELOG.md](CHANGELOG.md).

Data: [Rebrickable](https://rebrickable.com) (catalog and images), [BrickLink](https://www.bricklink.com) (prices, optional).
LEGO® is a trademark of the LEGO Group, which does not sponsor or endorse this project. MIT licensed.
