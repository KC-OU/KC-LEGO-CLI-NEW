# The launcher and the Script Hub

Everything you do from a shell (status, logs, restarts, backups, users, LEGO, shipping) is one list of tools. Two
front ends show it:

- **`wms menu`** (alias `q`) is a compact list in your terminal: about twelve lines, not a full screen.
- **Script Hub** (option 4 in the TUI) is the same list inside the TUI, so it works over telnet and the web terminal too.

## `wms menu`

```text
 [WMS+PartDB] · 7/7 up · gateway up · backup 3h · 1 low   q launcher · guide
 ──────────────────────────────────────────────────────────────────────────
 › █
 1   Status   System health
 2   Status   Doctor: check the whole install
 3   Services Containers
 ...
 1 of 37 · type to filter · Enter runs · Ctrl-P pin · Esc quits
```

| Key | Does |
|---|---|
| type | filter by any words of the group, title or hint |
| Up / Down, PgUp / PgDn | move |
| Enter | run the highlighted tool (digits 1-9 run that row) |
| Ctrl-P | pin or unpin a favourite; favourites, then recent tools, float to the top |
| Ctrl-U | clear the filter |
| Esc | quit |

Tools that restart or change something (a restart, a backup, a password reset, a deploy) show the exact command and ask
**y** first. A tool that needs input asks for it on the spot. Every run is written to the [audit log](security.md).
The status line at the top is filled in the background, so the list opens at once even if Docker is slow.

In scripts: `wms menu list` shows every tool with its id, and `wms menu run <id> [answers...] [--yes]` runs one.

## The Script Hub in the TUI

The same list, minus what must not run inside a remote session: deploys, restarts, log followers and anything that asks
questions. Enter runs the tool with the screen handed over to it, then waits for Enter before returning. Tools marked
admin-only need an administrator; risky ones ask first; every run is audited. Pins and recents are shared with `wms menu`.

## Your own tools

Put a `tools.json` next to `settings.json` (or set `WMS_TOOLS_FILE`). The file must belong to you or root and not be
writable by anyone else.

```json
[
  {"id": "df-root", "title": "Disk usage", "argv": ["df", "-h", "/"]},
  {"id": "greet", "title": "Say hello", "argv": ["echo", "hello {0}"], "ask": ["Who"], "where": "both", "risky": false}
]
```

`argv` is the program and its arguments (no shell). `{self}` is `wms`, `{repo}` the source checkout, `{0}`, `{1}` the answers
to `ask`. Answers become single arguments, so they cannot add options or run other commands. Your tools appear only in the
shell launcher unless you say `"where": "both"`. An entry with a built-in id replaces the built-in tool.

## The login banner and `guide`

`wms sys banner` prints the one-line status shown at login (containers up, gateway, newest backup, low-stock count);
`guide` prints a twelve-line card of the commands worth remembering. `scripts/bashrc.sh` is a 38-line `~/.bashrc` that uses
both; `scripts/install-bashrc.sh` installs it after saving the old one (`--dry-run` shows what changes).
