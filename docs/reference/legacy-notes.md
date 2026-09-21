# Notes from the original suite

!!! note "Historical"
    These are the running notes from before the project had a documentation site: what changed from the Python suite,
    what was verified against a real ModernWMS/Part-DB, and the security fixes made along the way. Newer,
    task-oriented pages are under **Guides**; the generated **Command reference** is always current.

# ModernWMS & Part-DB Suite (Go)

A from-scratch Go rewrite of the ModernWMS/Part-DB management suite, drawn in the green-screen
terminal style of the original Python suite (full-width panels, a tab bar, a function-key legend, a
`Selection or command ===>` prompt; the older fixed 80-column IBM i 5250 frame is still available)
and built as one CLI binary instead of a handful of Python scripts. It talks to the exact same two dockerized apps as before: **ModernWMS**
(the warehouse system) and **Part-DB** (the parts/inventory catalog). Nothing about those two apps
changed; only the tool that manages and syncs them did.

This lives at `/root/modernwms-partdb-go`, a separate project from the existing
`/root/modernwms-partdb-suite` Python suite. It does not touch that suite, its systemd services, or
its containers — both can run side by side, and switching over is your call, whenever you're ready.

---

## Update — 2026-09-20

**"Add / Update a part" is now lookup-first, and it writes to Part-DB through Part-DB's own API.**

- **One flow, three doors.** Part-DB Hub → *Create New Part*, LEGO → *Add Owned Part* and Quick Add all
  open the same screen: type a part number, and the name, category and the colours that part exists in
  are filled in from the offline Rebrickable catalog first (instant, no key, no rate limit), then live
  Rebrickable for anything the catalog lacks. You pick the colour (with a swatch; type part of a name to
  narrow the list; anything not listed is kept as typed text) and type how many you hold, then confirm
  with `yes`. If no source knows the number (an op-amp, say) you type the name/description/manufacturer
  no. yourself and pick or create the category — Part-DB only, nothing LEGO about it.
- **Category.** LEGO parts are filed under `Lego > <Rebrickable category>`, created if missing. If a
  lookup gives no category you pick one of your Part-DB categories (type to filter) or type a new name.
- **One Part-DB part per part + colour**: name `Brick 2 x 4 - Red`, IPN `3001-4` (part number + Rebrickable
  colour id; a typed colour becomes `3001-x-teal`), manufacturer part no. left empty so ModernWMS's item
  code follows the IPN, colour in the tags. Adding the same part+colour again updates the quantity.
- **Writes go through Part-DB's REST API, not its SQLite file.** The old direct-write path failed on the
  live schema (a new category or any stock quantity hit `NOT NULL constraint failed`), so it is gone.
  Create an API token in Part-DB (user menu → API tokens, *Edit* level) and paste it under
  **Admin → Settings & API Keys → Part-DB API Token** (it is tested on save). Without a token, LEGO parts
  are still saved in your collection and flagged *not in Part-DB yet*; `wms lego sync-parts` pushes them
  later. F9 undoes the last add.
- **Offline catalog.** `wms lego catalog refresh` downloads Rebrickable's free CSV files (parts,
  categories, colours, part/colour pairs; at most once a day; `--force` to override) and
  `wms lego catalog status` shows what is loaded. *Data: Rebrickable.* Live Rebrickable calls share one
  rate limiter and cache across every session, so several telnet users cannot get the server banned.
- **CLI:** `wms lego add-part 3001 --color red --qty 25` (`--yes` skips the prompt, `--dry-run` shows what
  would be saved, `--category` overrides the category).
- **Tests no longer touch live data**: the TUI tests run against a scratch Part-DB built from the live
  schema behind a fake API, with temp settings/2FA/audit files.

Config: `PARTDB_API_URL` (default `http://127.0.0.1:8081`, the local container, not the public host) and
`PARTDB_API_TOKEN` (normally set from the TUI, stored in `settings.json`, mode 0600).

**Security hardening (same date).**

- **2FA codes are single-use and guessing is capped.** A code that already signed someone in is refused
  ("that code was already used — wait for the next one"; it does not count as a guess). Five wrong codes
  in a row lock that account for 5 minutes, doubling each time up to an hour; while locked even the right
  code is refused and the 30-minute "same address" shortcut is off. Clear it with
  `wms users 2fa unlock <user>` (or type `unlock` on Admin → Settings → Two-Factor Authentication);
  `wms users 2fa status <user>` shows a lock. The 2FA store is now written atomically and guarded by a file
  lock, so several sessions at once can no longer lose an attempt count.
- **The audit log is tamper-evident.** Every new line ends in `| CHAIN:<hash>` covering the line and the one
  before it; the first chained line pins the whole log as it was. `wms audit verify` names the first line
  that was edited, removed or reordered (exit code 1); `wms audit head` prints the newest hash. Field values
  are sanitised, so a username with a newline in it cannot forge an entry. **Limit:** the gateway runs as
  root, so someone with root can rewrite the file *and* the chain, or drop the newest lines. Keep a copy of
  `wms audit head` somewhere this machine cannot write. Lines written by the old Python suite are reported
  as "no hash" rather than as a break.
- **Telnet is throttled per address.** At most 4 open sessions per address; a session that burns its 5
  sign-in attempts is a strike, and 3 strikes in an hour block that address for 15 minutes (doubling to an
  hour). This machine's own address is never throttled. A gateway session left at sign-on for 5 minutes is
  closed. The web terminal cannot see client addresses, so it is not throttled this way.
- **Plaintext telnet is labelled.** The sign-on screen says telnet is not encrypted — quietly on a private
  network, as a red warning when the client is on a public address.
- **Idle auto-lock.** A signed-in session with no key or tap for 15 minutes locks itself
  (`TUI_IDLE_LOCK_MINUTES`, `0` = never) and is audited as `SESSION_IDLE_LOCK`.

**Operations, accessibility and backups (same date).**

- **`wms doctor`** checks ModernWMS, the Part-DB file and API token, the Rebrickable key, the offline
  catalog's age, that the clock is NTP-synchronised (2FA codes depend on it), the gateway, the permissions of
  the settings/2FA/audit files, the audit chain and the newest backup — OK / WARN / FAIL with what to do.
  It exits 1 on any FAIL (a backup older than a week is one), so cron or a monitor can run it.
  `wms-go-doctor` now runs it after its own tool checks.
- **Scripting conventions** on `users`, `lego`, `backup`, `receive`, `sync status`, `audit` and `doctor`:
  `--json` (one JSON document on stdout; errors as `{"error","code"}` on stderr), `--quiet`/`-q`,
  `--dry-run` (say what would happen, change nothing) on the commands that change something, and `--yes`
  (`users delete` now asks first, and refuses to guess when there is no terminal). Exit status: `0` ok, `1`
  failure, `2` usage, `3` authentication/permission, `4` a service unreachable or erroring, `5` not found.
  Tab completion is dynamic: `source <(wms completion bash)` completes user names, `wms`/`partdb`, part
  numbers (yours first, then the catalog) and the colours of the part you already typed.
- **Encrypted backups.** `wms backup --encrypt` seals the backup with a passphrase (age; `age -d` opens it
  too), checks that the sealed copy decrypts to the same bytes, and only then deletes the plaintext.
  The passphrase comes from `--passphrase-file`, `WMS_BACKUP_PASSPHRASE` or a hidden prompt, never from an
  argument. `wms backup decrypt file.db.age` restores it (it will not overwrite a file). **Lose the
  passphrase and the backup is gone.** `wms backup --dry-run` shows where it would go.
- **Accessibility.** `NO_COLOR=1` turns colour off entirely. Admin → Settings → *Display Theme* (or
  `MODERNWMS_TUI_THEME`) also offers `high-contrast` (nothing dimmed, reverse video for problems) and
  `colorblind` (blue and orange instead of green and red). Both spell out `OK:` / `FAIL:` / `WARN:` beside
  the icons, so meaning never depends on colour alone. **F1** (or **?** on any screen that is not a form)
  shows the keys for the current screen.
- **Opt-in alerts.** Set `NOTIFY_URL` for the gateway service (an ntfy topic, or any webhook that takes JSON;
  `NOTIFY_FORMAT=ntfy|json` overrides the guess) and it reports a burst of failed sign-ins, a broken audit
  chain, a stale or missing backup, low stock and, daily, the audit chain's head hash (your off-box
  copy). At most one alert per kind per cooldown. Off by default; `wms notify test` sends a test alert.
  Messages never include passwords, tokens or 2FA codes.

**Collection insights (same date).**

- **Low-stock minimums.** The part confirm screen has a *Warn me when below* field (0 = don't track); the
  minimum is also set on the part in Part-DB (its own *minimum amount*) at the next sync. A part below its
  minimum shows as `3 LOW (min 5)` in *List Owned Parts*, the LEGO hub and the Overview say how many are low
  (the word LOW is spelled out, never colour alone), and the alert monitor lists them. `wms lego set-min
  3001 --color red --min 20`, `wms lego low [--fail]` (exit 1 when anything is low, for cron).
- **Collection Stats** (LEGO menu 7, `wms lego stats`): sets, copies and pieces in sets, loose pieces,
  distinct parts, and the top themes, colours and categories.
- **Missing Parts for a Set** (LEGO menu 8, `wms lego missing 75192 [--copies 2]`): the set's parts list
  from Rebrickable (cached for a month) compared with the loose parts you hold, matched on part number and
  Rebrickable colour id — "you hold 31% (5 of 16 pieces)", biggest shortfalls first. Parts held in a typed
  (free-text) colour cannot be matched, and printed or alternate variants count as different parts.
- **BrickLink wanted list, no BrickLink API needed.** `wms lego wanted --set 75192 -o falcon.xml` (or
  `--low` for what you are running out of) writes the XML BrickLink's *Wanted > Upload Wanted List Items*
  takes. BrickLink part and colour numbers come from Rebrickable; where it lists none the Rebrickable part
  number is used and/or the colour is left out, and the summary counts both so you know what to check.
- **Import a parts list.** `wms lego import-parts file --dry-run` reads a Rebrickable parts-list CSV or a
  BrickLink inventory/wanted XML, shows exactly what would be added or changed, and only then writes it, in
  one transaction (`--mode add` adds to what you hold, `--mode set` replaces it). Names and categories
  come from the offline catalog; BrickLink colour numbers are translated through Rebrickable (needs the key).
  Send the result to Part-DB with `wms lego sync-parts`.
- **`/` filters any list** (fuzzy: every word must match in order, so `bk 2x4` finds *Brick 2 x 4*; Enter keeps
  the filter, Esc clears it, R refreshes without losing it). The part prompt lists your **recent parts**;
  type `!1` to reuse the first.

---

## Update — 2026-09-18

This is the build now running behind `wms-gateway.service` (telnet 23/2323, web 7681) and behind the
`tui` / `wms` commands in `~/.bashrc`.

- **The TUI now looks like the original Python suite, at full screen.** The sign-on screen is a
  full-width box with the title in its top border, a red `[Q] Quit / Exit Suite` line and inline
  `Username / User ID (or 'q' to Quit):` / `Password:` prompts — nothing else. After sign-on every
  screen has the header band (date at the right edge), a tab bar (`1=Overview 2=PartDB Hub ...`), the
  F-key legend under it, `>   1. Label` menu lists and an inline prompt; the Overview and the Part/Set
  detail screens are grid tables. Rules, panels and the header follow the real terminal width instead of a
  fixed 80 columns. `MODERNWMS_TUI_CLASSIC=0` brings back the older fixed 80-column 5250 frame;
  `MODERNWMS_TUI_THEME=amber` still switches green to amber and works with either layout.
- **Telnet uses the whole window.** The gateway now reads the client's window size (Telnet NAWS,
  RFC 1073) and resizes the session to match, including when you resize the window mid-session. A
  client that never reports a size still gets 80x25. The web terminal (ttyd) always sized itself to the
  browser.
- **`tui` / `wms` now start the Go TUI.** `wms` with no arguments is the TUI; with a command it is the
  whole CLI (`wms users ...`, `wms lego ...`). The Python suite is still installed as `tui-py` /
  `wms-py`, and `scripts` / `script-runner` stay on it because the Go Script Hub lists scripts but
  cannot run them.
- **New: Settings & API Keys** (`Admin` → `Settings & API Keys`, **admins only** — enforced on entry,
  not just hidden). Change the Rebrickable API key and the sync dashboard's admin login, and check or
  disable a user's 2FA, without editing files. Values are saved to `WMS_SETTINGS_FILE` (default
  `/root/docker-server/wms/settings.json`, mode `0600`) and the sync login to its existing
  `credentials.json`; both take effect immediately, no restart. Every change is written to the audit log.
  Enrolling a user in 2FA still happens from a shell (`wms users 2fa enable <user>`) because it needs the QR
  code.
- **Menu regrouped.** The hub is Overview, PartDB Hub, Operations (Inbound ASN, Warehouse Ops, Inventory,
  Master Data, Outbound), Script Hub, LEGO Collection (`e`, or `G` from anywhere) and Admin (Users,
  Settings & API Keys, Containers). Each entry is shown to exactly the roles that could see it before;
  only an extra menu level was added, nothing about who has access changed.
- **Touch: tap to select.** On the web terminal a tap on a menu option selects it, and on the sign-on
  screen a tap on the Quit line quits. Mouse reporting is enabled only for the web gateway
  (`WMS_TOUCH_MODE=1`, set by the gateway), never for telnet.
- **Two bugs fixed** (see "Security fixes" below): a shortcut that skipped the 2FA prompt, and status
  messages that vanished.
- **Tooling** (`~/.bashrc`): `wms-go-deploy` builds, backs up the current binary to `/root/backups/`,
  swaps the new one in atomically and restarts the gateway; `wms-go-doctor` also checks the gateway
  service and the settings store; the quick-start guide (`guide`) lists all of this.

**Later the same day:**

- **LEGO: search sets on Rebrickable.** `LEGO Collection` → `2. Search Sets (local + Rebrickable)`.
  It uses the live Rebrickable API when a key is set (Admin → Settings & API Keys) and the imported local
  catalog otherwise, and says so if a live lookup fails instead of quietly showing less. Results include an
  `Owned` column, and `A` adds one to your collection (Rebrickable's `75192-1` is stored as `75192`, like
  your existing data; adding a set you already own adds copies rather than resetting the count). The rest
  of the LEGO menu moved down one number: 3 Search Parts, 4 Add Set, 5 Add Owned Part, 6 List Owned Parts
  (4 and 5 were then reworked, below).
- **Lock is now `F10`** (it was `F19`, which most keyboards and tablet key rows don't have). `F19` and
  `L` still work.
- Tests no longer write to the real audit log (`AUDIT_LOG_FILE` is pointed at a temp file).

**Then:**

- **2FA grace window.** After you enter a valid authenticator code, signing in again — after logging out,
  locking, or fixing something — skips the code prompt for 30 minutes (`TWOFA_GRACE_MINUTES`, `0` turns it
  off). It is pinned to where you came from: a sign-in from a shell on the server, or from the same
  address over telnet. A different address, and every web-terminal sign-in (ttyd hides the client's
  address), still get the prompt. The window starts only from a real code (or backup code), never from a
  grace sign-in, so it can't be stretched, and it is cleared by `2fa disable` or re-enrolling. Each skipped
  code is written to the audit log as `LOGIN_2FA_GRACE`. See [Two-factor authentication](#two-factor-authentication-totp).
- **LEGO: add and update from Rebrickable.** `LEGO Collection` → `4. Add / Update a Set` takes just a set
  number, fills in the name, theme (written like your existing data: `Star Wars - The Book of Boba Fett`),
  year and piece count from Rebrickable, shows how many you already own, and asks how many you have; you
  type a number and then `yes` to save. An existing set keeps your details and only its quantity changes.
  `5. Add / Update an Owned Part` does the same for parts (name and category). Theme and category stay
  editable so you can match your own naming. If Rebrickable has no such item, the key is wrong, or you are
  rate-limited, the screen says which and falls back to the local catalog, then to typing the details in.
  Rebrickable has no minimum-age or instruction-book data, so those aren't filled in; the old by-hand set
  form (with instruction-book fields) is gone, and `wms lego import` still carries those fields over.
- **Overview error fixed.** The ModernWMS panel failed with `no such column: is_valid` because the
  dispatch count queried a column `dispatchlist` doesn't have (the Python suite had the same bug). A new
  test, `TestSQLMatchesModernWMSSchema`, compiles every SQL statement sent to ModernWMS (including the
  sync engine's) against a structure-only copy of the live schema (`internal/wmsdb/testdata/wms_schema.sql`),
  so a query naming a missing column now fails in `go test` instead of on a screen. Refresh that file when
  ModernWMS is upgraded.
- **Typing no longer triggers shortcuts on forms.** With an empty field focused, a capital `L` (as in
  "Lloyd's Ninja Bike") locked the session, and `U`/`G`/`+` did their jobs instead of being typed. On
  screens with an input field only `Q` remains a letter shortcut; the F-keys work everywhere.

Still not done: a tappable tab bar, the on-screen numpad overlay the Python suite has, and running
scripts from the Script Hub.

---

## For existing users — what's new, what's the same

**Nothing was taken away.** `wms`, `tui`, `wms-tui`, `modernwms`, `partdb` and `partdb-tui` now start
the Go TUI; the original Python TUI is one command away as `tui-py` / `wms-py`. The other Python helpers
(`manage-users`, `receive-stock`, `reset-modernwms-password`, `wms-backup`, and `scripts` /
`script-runner`) are unchanged, and each has a Go equivalent under a separate name:

| Python command | Go equivalent | Notes |
| :--- | :--- | :--- |
| `wms` / `tui` / `modernwms` (now the Go TUI; Python = `tui-py`) | `wms-go` / `gowms` | Launches the TUI |
| `manage-users` | `wms-go-users` (or `wms-go users ...`) | Same dual-DB user management |
| `receive-stock <id> <qty>` | `wms-go-receive <id> <qty>` | Same ASN receiving |
| `wms-backup` | `wms-go-backup` | Same SQLite online-backup approach |
| — | `wms-go-sync` | New: `sync status\|trigger\|serve` from the CLI |
| — | `wms-go-gateway` | New: telnet + web terminal gateway |
| — | `wms-go-doctor` | New: health check for the Go install |
| `KC-LEGO-CLI` / `/root/Lego` scripts | `wms-go-lego` (or `wms-go lego ...`) | New: LEGO collection tracking, folded into this binary — same login/2FA, no more shell/jq/Python |

Run `guide` (or just open a new shell) to see both toolsets listed side by side.

**What's actually different if you sit down at the Go version:**
- The screen looks like the Python suite's, but is a real full-screen program: it redraws in place
  and follows the terminal size, with the same header band, tab bar, F-key legend and menu lists (see
  the Update section above), and it also runs over telnet and the web terminal.
- One binary, `wms-go`, with subcommands (`wms-go users ...`, `wms-go receive ...`, ...) instead of
  five separately-symlinked scripts.
- **New: optional 2FA — mandatory over the gateway.** You can add a TOTP authenticator code (Google
  Authenticator, Authy, 1Password, etc.) as a second login factor. For a local `wms-go tui` session
  it's opt-in and off by default; for anything reaching in through `wms-go gateway serve` (telnet or
  the web terminal) **it's required** — an account with no 2FA enrolled is refused there, even if it
  can still log in locally. Enroll before you need it:
  ```bash
  wms-go users 2fa enable <your-username>   # shows a QR code + 8 one-time backup codes
  ```
  See [Two-factor authentication](#two-factor-authentication-totp) below for details.
- **New: `G` switches you straight to LEGO Collection from anywhere**, and back again, with a
  "Switching to..." status line — one key, no matter how deep you are in either area, no reconnect and
  no re-login/re-2FA (it's an in-session screen jump, not a new connection). `e` from the main hub
  still works too.
- The sync engine, REST API, and Prometheus `/metrics` all still work the same way and feed the same
  Grafana dashboard — no changes needed there. The sync dashboard's web page is gone; `wms-go sync
  status` / `trigger` cover that from the terminal instead.

---

## For new users — quickstart

```bash
cd /root/modernwms-partdb-go
go build -o /usr/local/bin/wms-go ./cmd/wms   # or just: wms-go-build (see ~/.bashrc)
wms-go
```

You'll land on the sign-on screen. The three seeded demo accounts (same ones the Python suite uses):

| Role | User ID | Password | Access |
| :--- | :--- | :--- | :--- |
| Admin | `7354` | *(ask an existing admin)* | Full read/write, Users, Containers |
| Picker | `10932` | *(ask an existing admin)* | Warehouse operations |
| View-Only | `viewonly` | `viewonly` | Read-only — **note:** the Python suite's sync dashboard displays a stale demo password (`view123`) for this account; the real one is `viewonly`, confirmed against the live database |

From the main hub, numbers/letters pick a menu option (an option like Operations or Admin opens a submenu), `Q`/`Esc` backs out one screen (or exits at
the hub, or logs out at any deeper screen), `U` undoes the last reversible change, `+` opens a
quick-add drawer, `L` locks the session, `G` jumps straight to LEGO Collection from anywhere (and
back again from inside it) without backing out screen-by-screen. The hub has six entries — Overview, PartDB
Hub, Operations, Script Hub, LEGO Collection and Admin — and the eleven areas behind them (Inbound ASN,
Warehouse Ops, Inventory, Master Data, Outbound under Operations; Users, Settings & API Keys, Containers
under Admin) show up only if your role has access. Overview, PartDB Hub, Script Hub and LEGO Collection are
visible to every logged-in account; Settings & API Keys is admin-only.

### Two-factor authentication (TOTP)

Off by default, per-account opt-in for a local `wms-go tui` session — **but mandatory for any session
that comes in through `wms-go gateway serve`** (telnet or the web terminal). An account with no 2FA
enrolled will still log in locally, but is refused at the gateway with "2FA is required for remote
access." Enroll before anyone needs remote access:

```bash
wms-go users 2fa enable <username>     # scan the QR with any authenticator app, confirm a code
wms-go users 2fa status <username>
wms-go users 2fa disable <username>
```

Enrolling shows a QR code, a manual-entry secret (for apps that can't scan), and — once you confirm
your first code — **8 one-time backup codes, shown exactly once.** Save them somewhere safe; if you
lose your device, a backup code logs you in once and an admin can `2fa disable` your account to
re-enroll. From then on, logging in through `wms-go tui` asks for your 6-digit code right after your
password. This is TOTP (the same standard behind Google Authenticator/Authy/1Password), not SMS —
no third-party service, no phone number, no per-login cost, and it isn't vulnerable to SIM-swap
attacks the way SMS codes are. It only applies to interactive TUI logins (and therefore telnet/web
gateway sessions, since both run the same TUI); one-shot commands like `wms-go receive` are unaffected.

**Grace window.** Logging out and back in (to fix something) shouldn't mean re-entering a code every
time, so once a code has been accepted the same origin may sign in again for `TWOFA_GRACE_MINUTES`
(default 30; `0` disables) with the password alone. "Origin" is `local` for a shell on this box or
`telnet:<client address>` for telnet (the gateway hands the address to the TUI as `WMS_REMOTE_ADDR`, which is
only believed on gateway sessions). The web terminal has no visible client address, so it is never given a
window. The window is measured from the last real code, not from the last sign-in, `2fa disable` and
re-enrolling clear it, and each skipped prompt is audited as `LOGIN_2FA_GRACE`. It weakens 2FA by
design: someone with your password, coming from your address within the window, does not need a code.

The gateway tells a session it's remote by setting `WMS_GATEWAY_SESSION=1` in the child process
environment (`internal/gateway/telnet.go`'s `childEnv()` and `internal/gateway/web.go`'s `ttydCmd.Env`)
before `wms-go tui` starts; the TUI reads that once at startup (`cmd/wms/tui.go`) and, if set, refuses
any login where `twofa.IsEnabled` comes back false instead of falling through to the hub
(`internal/uiapp/login.go`). Nothing about local `wms-go tui` changes.

---

## Verified against production

Before shipping, this was driven against the real `modernwms` and `partdb` containers on this box,
not just built and unit-tested:

- **Sync engine**: `wms-go sync trigger` correctly synced 252 categories, 16 parts, and 16 stock
  records from the live Part-DB data into ModernWMS. (This caught and fixed one real bug — see
  below.)
- **Login & role gating**: logged in as the real `viewonly` account; confirmed it sees exactly the
  7 tabs it should (Overview, PartDB Hub, Script Hub, Inbound ASN, Inventory, Master Data, Outbound)
  and not the other 3 (Warehouse Ops, Users, Containers).
- **Write-gating**: confirmed a View-Only login is denied when attempting to create a part, and that
  the denial is correctly written to the audit log.
- **A real write**: `wms-go receive 2780 1` against the live ModernWMS container — confirmed the
  resulting ASN record and the stock quantity (44 → 45) in the database directly.
- **2FA end-to-end**: enrolled a test account, drove a full login through a real TOTP code, confirmed
  both the login screen and the audit log behaved correctly, then removed the test enrollment.
- **Telnet + web gateway**: confirmed the correct 5-byte RFC 854 IAC negotiation over a raw socket,
  and that the web gateway serves `/manifest.json` (PWA) with a 200. Also connected with a bare `nc`
  socket (no telnet protocol support at all — harder than any real telnet client) on scratch ports:
  the 5250 sign-on panel, function-key row, and field-level keystroke echo all render correctly, and
  the spawned session cleans up with no leaked processes on disconnect.

**Bugs found and fixed during this pass:**
- The sync engine's bootstrap SQL was missing several `NOT NULL` columns the live ModernWMS schema
  actually has (the earlier build only had the abbreviated field list from documentation, not the
  exact live schema), and a soft-delete helper returned a `nil` slice that serialized to JSON `null`
  and crashed the generated script whenever there was nothing to soft-delete (the common case).
- **Every telnet connection hung on a black screen for up to 5 seconds** before the sign-on panel
  appeared. Cause: `bubbletea`'s package `init()` unconditionally queries the terminal's background
  color over OSC and waits up to `termenv.OSCTimeout` (5s) for a reply; real terminal emulators
  answer instantly (invisible when run locally), but a bare/basic telnet client — Windows
  `telnet.exe`, BusyBox telnet, or any minimal client of the kind used for Cisco/IBM-style console
  access — never answers it. Fixed in `internal/gateway/telnet.go` (`childEnv`) by setting
  `TERM=dumb` (which `termenv` special-cases to skip the query) plus `COLORTERM=truecolor` (to keep
  full color despite that). The sign-on panel now renders in under a second, with byte-identical
  output otherwise.

All fixed, covered by regression tests, and confirmed working against the live data / a real
connection above.

**Not yet exercised**: user create/reset/modify/delete against the live ModernWMS/Part-DB accounts
(the unit tests cover this logic against fixture data; a real supervised run is still worth doing
before relying on it for account changes you can't easily undo).

---

## Build

```bash
go build -o /usr/local/bin/wms-go ./cmd/wms
```

Static Go binary, no interpreter or `pip`/`apt` dependency install step — unlike the original, which
needed Python + `rich` + `ttyd` set up per-host. `ttyd` and `docker` are still required on PATH for
the web/telnet gateway and the ModernWMS `docker exec` bridge respectively (both already present on
this box).

## Commands

```
wms-go                                   # launches the TUI (same as `wms-go tui`)
wms-go users list|create|reset|set-password|delete|toggle
wms-go users 2fa enable|disable|status <user>
wms-go receive <id-or-code-or-name> <qty> [--as <user>]
wms-go backup
wms-go sync status|trigger|serve         # serve = REST API + /metrics, no HTML dashboard
wms-go sync set-password
wms-go gateway serve                     # telnet daemon (barcode scanners) + ttyd web terminal + PWA
wms-go lego import [--collection ...] [--ref-sets ...] [--ref-parts ...]
wms-go lego add-part <part_num> --color <name|id> --qty <n> [--yes] [--dry-run] [--category <name>]
wms-go lego sync-parts                   # push parts that never reached Part-DB, via its API
wms-go lego catalog refresh [--force]    # offline Rebrickable parts/colours catalog
wms-go lego catalog status
wms-go lego low [--fail] | set-min <part> --min N | stats | missing <set> | wanted --set N|--low -o f.xml
wms-go lego import-parts <file.csv|.xml> [--mode add|set] [--dry-run] [--yes]
wms-go users 2fa unlock <user>          # clear a 2FA lockout
wms-go audit verify|head                # check the audit log's hash chain
wms-go doctor                           # health check; exit 1 on any FAIL
wms-go backup [--encrypt] [--dry-run]   # ... and: wms-go backup decrypt <file.age>
wms-go notify test                      # send a test alert to NOTIFY_URL

# every command: --json, --quiet; exit codes 0 ok / 1 failure / 2 usage / 3 auth / 4 network / 5 not found
```

Every subcommand has `--help`. Config is env-var driven (`internal/config`), same variable names as
the original's `.env.example` — `MODERNWMS_CONTAINER`, `MODERNWMS_DB_PATH`, `PARTDB_DB_PATH`,
`MODERNWMS_BACKUP_DIR`, `AUDIT_LOG_FILE`, `PORT`, `CREDENTIALS_FILE`, `LISTEN_PORTS`, `GATEWAY_PORT`,
`TTYD_PORT`, `MODERNWMS_TUI_THEME` (`green` default, or `amber`), `MODERNWMS_TUI_CLASSIC` (the TUI
uses the original Python TUI's full-width layout by default; set to `0` for the fixed 80-column 5250
frame), `WMS_TOUCH_MODE=1` (tap-to-select; set automatically for the web/ttyd gateway), `TWOFA_GRACE_MINUTES`
(minutes a verified code covers later sign-ins from the same origin; default `30`, `0` = always ask),
`TWOFA_FILE` (2FA store, default
`/root/docker-server/wms/2fa.json`), `LEGO_DB_PATH` (LEGO collection store, default
`/root/docker-server/wms/lego.db`), `REBRICKABLE_API_KEY` (optional — falls back to the offline
catalog when unset), `PARTDB_API_URL` / `PARTDB_API_TOKEN` (how parts are written to Part-DB), `TUI_IDLE_LOCK_MINUTES` (idle auto-lock, default `15`, `0` = off), `NOTIFY_URL` / `NOTIFY_FORMAT` (alerts), `WMS_BACKUP_PASSPHRASE` (encrypted backups, for unattended runs), `NO_COLOR`, etc. **`SYNC_ADMIN_PASS` has no built-in default** — set it explicitly before the
sync API's admin login can be used (see "Security fixes" below).

---

## Connecting remotely (Telnet or a web browser — no SSH, no special client)

`wms-go gateway serve` starts two ways in from any other machine, LAN device, or a genuinely
different OS/distro, without logging into this box over SSH first — the same way you'd reach a
Cisco/IBM console or any other network appliance's CLI:

**Option 1 — a web browser (easiest, works from anything with a browser, including a phone):**

```
http://<this-server's-IP-or-hostname>:7681
```

That's it — no client software to install. It opens a real terminal (via `ttyd`) running the exact
same TUI, sized to your browser window, with tap-to-select on touch screens.

**Option 2 — a Telnet client (for barcode scanners, PuTTY, terminal emulators, or just the classic
`telnet` command):**

```bash
telnet <this-server's-IP-or-hostname> 2323
```

- **Linux / macOS**: `telnet` is either already installed or one `apt install telnet` /
  `brew install telnet` away. Open a terminal and run the command above.
- **Windows**: the built-in `telnet.exe` client is disabled by default. Enable it once via
  `Control Panel → Programs → Turn Windows features on or off → Telnet Client` (or
  `dism /online /Enable-Feature /FeatureName:TelnetClient` in an admin PowerShell), then run
  `telnet <server> 2323` from Command Prompt/PowerShell. Alternatively, open **PuTTY**, set
  "Connection type" to **Telnet**, enter the host and port `2323`, and click Open.
- Port `23` (the standard Telnet port) also works and needs no port number:
  `telnet <this-server's-IP-or-hostname>`.

Telnet fills your whole terminal window: the gateway reads the size your client reports (NAWS) and
follows it if you resize. A client that can't report a size (some scanners, very old terminals) is
given 80x25.

Either way, you land directly on the same sign-on screen `wms-go tui`
shows locally — log in with your normal ModernWMS/Part-DB username and password. **2FA is required
here** (see [Two-factor authentication](#two-factor-authentication-totp) above) even for accounts that
don't need it for a local session — enroll first with `wms-go users 2fa enable <username>` or you'll
be refused at the sign-on screen. Once in, `e` from the hub or `G` from anywhere gets you to the LEGO
Collection tab and back — same session, same login, no second connection. Lock the session with `F10`
(or `L`). Run `wms-go-doctor` (or just
`wms_go_doctor`) on this box any time to check both ports are actually listening.

**A note on security:** Telnet sends everything, including your password, in plain text — exactly
like a real router/switch console port. That's fine on a trusted LAN or over a VPN, the way this was
always intended to be used, but don't forward ports `23`/`2323` to the open internet. If you need
access from somewhere untrusted, prefer the web option over HTTPS (once a reverse proxy/TLS is put in
front of port `7681`) or tunnel over SSH/VPN instead.

---

## LEGO collection

A from-scratch Go port of the old bash/jq/Python `KC-LEGO-CLI` tool, folded into this binary instead
of living as a separate handful of shell scripts, `.conf` files, and JSON blobs. No shell-outs, no
`jq`, no Python — a native Go package (`internal/lego`) with its own small SQLite file, plus a direct
`net/http` client for the Rebrickable API (replacing the old `curl` + `jq` pipeline).

**Sets and parts are deliberately kept separate:**
- **Sets** (what you own, instruction books, parted-out status) live entirely in their own SQLite
  file (`LEGO_DB_PATH`, default `/root/docker-server/wms/lego.db`) and never touch Part-DB or
  ModernWMS — this is just personal collection tracking.
- **Parts** you own individually (spares, bulk lots, parted-out stock) are kept per part **and colour**
  and sync **one-way** into the existing Part-DB inventory through Part-DB's REST API (adding a part
  does this straight away; `wms-go lego sync-parts` retries anything that could not be sent), filed
  under `Lego > <category>` and matched on Part-DB's `ipn` column (`<part>-<colour id>`) — so re-running
  the sync updates existing rows instead of duplicating them.

```bash
# One-time: import your existing collection + reference catalogs
wms-go lego import \
  --collection /root/Lego/0.json \
  --collection /root/Lego/kc_sets_export_20250718_222338.json \
  --ref-sets /root/Lego/Lego-Lookup/legolookup.json \
  --ref-parts /root/Lego/Lego-Lookup/legolookup-part.json

# Download the offline catalog once, then add parts by number: name/category come from the catalog
wms-go lego catalog refresh
wms-go lego add-part 3001 --color red --qty 25
wms-go lego sync-parts        # only needed to retry parts that could not be sent to Part-DB
```

In the TUI, log in as normal and pick the **LEGO Collection** tab from the hub, or press `G` from any
screen to jump straight there (and `G` again to jump straight back) — same login, same session, same
2FA as everything else; there's no separate LEGO account system and no reconnect involved either way.
It covers: search my sets, search
sets and search parts (both use live Rebrickable search when a key is set, falling back automatically to
the imported local catalog when it isn't or a request fails — a found set can be added straight to your
collection with `A`), inspect a set, add or update a set and add or update an owned part (both fill in the
details from Rebrickable, ask how many you own, and need a `yes` to save), list owned parts. Write actions go through the same read-only-role gate as every other tab.

Set `REBRICKABLE_API_KEY` to enable live lookups (both set search and part search use it); without it,
search and lookup still work fully against whatever you imported via `--ref-sets`/`--ref-parts`. Get a
free key at [rebrickable.com](https://rebrickable.com) → Account Settings → API → "Create new key".

**Local CLI / local `wms-go tui`:** export it in your shell (e.g. add to `~/.bashrc`, then `reload`):
```bash
export REBRICKABLE_API_KEY="your-key-here"
```

**Telnet/web gateway (`wms-gateway.service`):** a shell export doesn't reach a systemd service — set it
in the unit instead, via an environment file (keeps the key out of `systemctl cat`/process listings'
plaintext unit source):
```bash
install -m 600 /dev/null /root/docker-server/wms/lego.env
echo 'REBRICKABLE_API_KEY=your-key-here' >> /root/docker-server/wms/lego.env
# insert EnvironmentFile= right after the [Service] line (must land inside that
# section, not appended to the end of the file, or systemd silently ignores it):
sed -i '/^\[Service\]/a EnvironmentFile=/root/docker-server/wms/lego.env' /etc/systemd/system/wms-gateway.service
systemctl daemon-reload && systemctl restart wms-gateway.service
```
Confirm it took: search a set/part from the LEGO tab over telnet and check the results title says
"(live)" instead of "(local)".

---

## Technical notes

### What changed vs. the Python suite

- One binary with subcommands instead of five symlinked scripts.
- Part-DB access is native Go (`modernc.org/sqlite`, pure Go, no cgo) instead of shelling into the
  container; Part-DB password hashing is native `golang.org/x/crypto/bcrypt` instead of a
  `docker exec partdb php -r 'password_verify(...)'` hack.
- ModernWMS access is unavoidably still `docker exec -i modernwms python3 -` piping a generated
  script — confirmed live that the container has no bind mount and no `sqlite3` CLI, only `python3`,
  so this is the only real bridge, not a shortcut.
- Sync dashboard's REST API now enforces Basic Auth on POST routes (the original left them
  unchecked — a real gap, fixed here since this is new code, not a faithful bug-for-bug port).
- ASN ticket numbers (`ASN` + timestamp) get a random suffix to avoid the original's same-second
  collision risk, and `receive`'s ASN `creator` field is the actual logged-in/`--as` user instead of
  a hardcoded ID.
- The sync dashboard's HTML page is dropped; `wms-go sync status`/`trigger` cover it from the CLI.
  The REST API and `/metrics` (same endpoint paths, same Prometheus metric names) are kept — the
  existing Grafana dashboard and Prometheus scrape config need no changes.
- The web terminal still wraps the real `ttyd` binary (like the original's `start_webtui.sh`) rather
  than reimplementing a WebSocket/xterm bridge, but the reverse proxy in front of it is Go's
  `net/http/httputil.ReverseProxy` instead of a hand-rolled byte-pipe.
- **New: TOTP 2FA** (`internal/twofa`), opt-in, stored independently of both live app schemas — see
  above.

### Security fixes

**2026-09-18 — 2FA could be skipped at the code prompt.** After the password was accepted the session
was already stored, and the `G`, `+`, `L` and `F6` shortcuts only checked that a session existed, so
pressing one of them at the authenticator-code (or forced-password-change) prompt went straight into
the app. The session now counts as signed in only after password, 2FA and any forced password change
have all completed (`App.authed`), the shortcuts require it, and pressing Esc at either prompt abandons
the login and clears the half-authenticated session (audit action `LOGIN_ABORTED`). Regression test:
`TestShortcutsBlockedUntilSignOnCompletes`. The build deployed before this update contained the hole,
which only mattered to someone who already had a valid password.

**2026-09-18 — status messages vanished.** `back()` cleared the message line, so every "created /
updated / added" confirmation and every access-denied message set just before returning to the previous
screen was wiped before it was drawn. Regression test: `TestBackPreservesMessage`.

**Earlier pass** — four small, self-contained fixes, none touching the live Python suite or its config:

- **No more hardcoded default admin password.** The sync API used to bootstrap its admin account from
  `SYNC_ADMIN_PASS`, which defaulted to the literal string `admin123` if the env var wasn't set — and
  that default was live in production (`/root/docker-server/partdb-sync/.env`). `SyncAdminPass` now
  has no default at all; if it's unset and no credentials file exists yet, the server refuses to
  bootstrap and logs an error instead of silently standing up a known-weak account. **This is a
  behavior change** — set `SYNC_ADMIN_PASS` explicitly before first run.
- **Backup files are no longer world-readable.** `wms-go backup` now `chmod 0600`s the resulting
  `.db` dump (previously inherited the umask, typically `0644`) and creates the backup directory at
  `0700` instead of `0755`.
- **Audit log opens at `0600`**, not `0644`, matching the 2FA store's existing convention. (Only
  affects newly-created log files — chmod the existing `/root/tui_audit.log` by hand if you want the
  same tightening applied retroactively.)
- **Flagged, not changed:** the hardcoded admin-username bypass (`kcollins`/`ID==1`/`ID==2` in
  `internal/auth/session.go`, `internal/partdb/users.go`, `internal/wmsdb/users.go`) is a known design
  smell — a config-driven admin list would be the fix — but changing who is treated as admin is its
  own security-sensitive decision, deliberately left for a separate, explicit sign-off pass.

### Known, inherited gaps (carried over deliberately, not oversights)

- **Warehouse Ops** aliases straight to the Part-DB stock-adjust screen, same as the original — no
  ModernWMS-side move/freeze/stocktake screens exist in the source app either.
- Script Hub only discovers scripts under this repo's own `scripts/` dir (the original scanned
  several host directories and executed arbitrary files it found — intentionally narrowed here).

### Deploy

`deploy/docker/` builds the sync/API daemon as `wms-sync` (parity with the Python suite's
`partdb-sync` container, published on `8083` instead of `8082` so it can run alongside the live stack
without a collision). `deploy/systemd/wms-gateway.service` and `wms-sync.service` are unit files for
the two long-running pieces (both `ExecStart /usr/local/bin/wms-go ...` — the binary this project
builds/installs as, not `wms`).

**The gateway is installed and running.** `wms-gateway.service` took over the ports the Python suite's
`modernwms-webtui.service` used to hold (`23`, `2323` telnet; `7681` web; `7682` ttyd) — it cannot run
alongside it. To ship a new build to it:

```bash
wms-go-deploy      # builds, backs up the current binary to /root/backups/wms-go.<timestamp>,
                   # swaps the new one in atomically, restarts wms-gateway.service
```

Restarting drops any sessions connected at that moment. The manual equivalent is
`go build -o /tmp/wms-new ./cmd/wms`, `cp /usr/local/bin/wms-go /root/backups/...`, `mv -f /tmp/wms-new
/usr/local/bin/wms-go` (a running binary can't be overwritten in place, so replace it with `mv`), then
`systemctl restart wms-gateway.service`. The pre-update binary is kept at
`/root/backups/wms-go.pre-classic`.

Rollback to the previous build: `cp /root/backups/wms-go.pre-classic /usr/local/bin/wms-go && systemctl
restart wms-gateway.service`. Rollback of just the look: set `MODERNWMS_TUI_CLASSIC=0` in the
service environment. Rollback to the Python gateway entirely:

```bash
systemctl stop wms-gateway.service
systemctl disable wms-gateway.service
systemctl enable --now modernwms-webtui.service
```

`wms-sync.service` has no such conflict (it doesn't bind any port the Python suite uses) and can be
installed/enabled independently whenever you're ready to cut that piece over too.

### Testing

```bash
go build ./... && go vet ./... && go test ./... && gofmt -l .
go test -race ./...   # concurrency check — clean as of this pass, including internal/lego
```

Unit tests cover the pure logic (auth hashing, sync field-mapping/soft-delete rules, the telnet IAC
parser including NAWS window-size parsing, the layout renderers, the sign-on screens and their key
handling, the Settings screens' admin gate, field-navigation, TOTP enroll/verify/backup-codes, LEGO collection import/search/part-sync)
without needing the live containers. See "Verified against production" above for what's actually been
run against real data, and "Not yet exercised" for what hasn't.

The LEGO import path was additionally run against the real files in `/root/Lego` (`0.json`, the
`kc_sets_export_*.json` export, and both `legolookup*.json` reference catalogs) into a scratch
`LEGO_DB_PATH`, confirmed idempotent on a second run. `wms-go lego sync-parts` was **not** run against
the live Part-DB during development (it would write test data into real inventory) — its logic is
covered by `internal/lego/partsync_test.go` instead; run it for real once you're ready.
