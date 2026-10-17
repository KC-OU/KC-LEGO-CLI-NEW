# Changelog

All notable changes. The format follows [Keep a Changelog](https://keepachangelog.com/); versions follow [SemVer](https://semver.org/).

## [Unreleased]

### Security
- **A failed-login lockout that didn't actually survive a reconnect.** 5 wrong passwords used to end
  the session ("Maximum failed login attempts exceeded"), but the attempt counter lived only on that
  one TUI process — and telnet (and the web terminal) spawn a fresh process per connection, so simply
  reconnecting reset it to zero every time, making the lockout trivially bypassable. Fixed with a
  small persisted store (`internal/loginlock`, alongside `2fa.json`) keyed by username: 5 failures now
  locks that username out for 15 minutes regardless of how many times the session reconnects, cleared
  by a correct login or an admin password reset. See
  [Access control](docs/guides/access-control.md#a-real-persisted-login-lockout).
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
- **A kanban board for checks and picks, with a backlog gate for new sets.** Adding a new set now
  queues it in a backlog instead of becoming checkable immediately — an admin releases it when
  it's a good time, since someone may already be mid-check elsewhere. Pickers/checkers get a
  **Kanban** entry (replacing "My Current Jobs") showing their own job, recent history and the
  shared backlog, plus private or admin-shared notes on their current job. Admins get a full board
  (all users, both checks and picks, filterable to one person) and the backlog release action. On a
  terminal 70 columns or wider, Backlog/Active/Done render as three bordered, colour-coded columns
  side by side with a live card count, falling back to a plain stacked list on anything narrower.
  Reachable with **Shift+F1** (F13 fallback) from either hub. A check line at or above a
  quantity threshold (default 50, `KANBAN_BACKLOG_QTY_THRESHOLD`) is automatically flagged to be
  done last, without leaving the job. The board is read-only — claiming, finishing and abandoning
  jobs all still happen exactly where they already did. See
  [Kanban board](docs/guides/kanban.md).
- **Renaming a numeric Picker/Checker login ID.** An MC9090 (or any numeric-keypad-only handheld)
  can't type anything but digits, so that account's ID is effectively its permanent username —
  there was previously no supported way to change one once created. `wms access user rename
  <old-id> <new-id>` moves the policy entry, 2FA enrollment and login-lockout state, and every
  ticket/accuracy/kanban/attendance/messaging record in the LEGO database, in one go; a live
  session under the old ID is dropped so it reconnects instead of silently losing its permission
  lookup. The audit log is deliberately never rewritten — history from before a rename stays under
  the old ID. See
  [Access control](docs/guides/access-control.md#renaming-a-numeric-login-id).
- **Accuracy, always visible, with a real name for numeric accounts.** Every screen's header now
  shows the signed-in user's current accuracy (e.g. `7354 - Kieran Collins (100%)`), not just the
  dedicated accuracy screen. `wms access user set-name <id> "<full name>"` gives a numeric-login
  Picker/Checker account (whose "username" is otherwise just its ID) a real display name to show
  alongside it. See [Kanban board](docs/guides/kanban.md#accuracy-always-visible).
- **"Solo mode": a personal copy for a machine with no access to the ModernWMS/Part-DB containers.**
  A second Arch box at home, say — not the server the real containers run on, and not the existing
  `wms-gateway-test.service` (which still needs those containers reachable). `wms access user
  set-password <name> --source local` sets a local sign-in password (bcrypt, stored in `access.json`,
  same scheme as the remote-bot PIN); `WMS_SOLO_MODE=1` makes the gateway start even without a
  Part-DB SQLite file (every Part-DB call then returns a clear error instead of aborting the whole
  process), and routes login through that local password instead of ModernWMS/Part-DB. LEGO features
  work exactly as they do live — they never depended on either backend. `scripts/solo-instance.sh`
  builds and runs one entirely out of `~/.local/share/wms-solo`, no root or systemd needed, bound to
  its own IP and port rather than loopback-only. See [Solo mode](docs/guides/solo-mode.md).
- **A missing or wrong LEGO element id, fixable by any checker, not just admins.** Element ids
  (the 6-8 digit number printed by each part in modern instructions, used for Pick a Brick/Bricks &
  Pieces ordering) normally only ever came from the Rebrickable catalog sync, with no way to add one
  the catalog is missing or correct one that's wrong. The "Where to Buy" screen's **E** key (and
  `wms lego element-id <part> <colour> --set <id>`/`--clear`) now lets a checker or admin record one
  directly — a new `part_element_overrides` table, checked ahead of the catalog's own value and never
  touched by a catalog refresh (unlike `cat_element_ids` itself, which is fully reloaded every sync).
  A manually-added id is just as scannable afterwards as a catalog one. Gated on `sets.check` (the
  same permission as checking a set at all), not an admin-only permission. See
  [Missing parts, prices and orders](docs/guides/missing-parts-and-orders.md).
- **A set's own instruction book number(s) and photos, assignable directly.** `wms lego set-info
  <set> --instruction-book <number> --instruction-book-count <n>` (or the matching fields on *Set
  Workshop → Where the Set Is Kept*) sets or corrects a set's booklet number(s) yourself, rather than
  only through catalog import — the existing slash-separated convention covers more than one
  (`6644777/6644778`). `wms lego set-photo <set> <file>` attaches any number of photos to a set
  directly (the box, the built model, ...), verified the same way a check's proof photo already is;
  `wms lego set-photos`/`set-photo-remove` list and remove them, and the set detail screen shows how
  many are attached. CLI-only, same reasoning as `wms lego attach` — it needs a real file on disk. See
  [Sets, missing parts and what to build](docs/guides/sets-and-building.md#your-sets).
- **Missing/extra are now coloured per line, not just in the top badge.** The table check view's
  Miss/Extra columns render red/blue (`theme.Danger`/`theme.Info`) per row, matching the always-on
  MISSING/EXTRA badge and the guided walk's own per-line colouring — scanning down the list itself
  shows which lines need attention, not just the running total at the top. See
  [Set checks](docs/guides/set-checks.md#the-parts-check).
- **Bag lookup now shows who printed it and what its check found.** Scanning or typing a bag's code
  (*Set Workshop → Find a Bag*, `wms lego bag-find`, `GET /mobile/bag-find`) shows an admin who
  registered/printed that bag and when, plus — once its check is finished — that check's own result
  (missing/extra counts, disputed flag and note): "what mess-ups did this bag make", reusing the
  check's existing record rather than a new incident log. Non-admin sessions see the same lookup as
  before. See [Set checks](docs/guides/set-checks.md#finding-a-lost-bag).
- **Numeric user ID + PIN for new/reset Picker and Checker accounts**, for handheld barcode scanners
  (e.g. an MC9100 over telnet) whose keypad makes a free-form username awkward to type: a 4-5 digit
  user ID and a 5-6 digit PIN instead of a free-form name and a mixed-character temp password.
  Validated/generated at creation (`wms users create` / *Users → Create New User*) and reset
  (`wms users reset` / *Users → Reset Password*, which also clears the new persisted lockout above);
  existing accounts are untouched until one is next created or reset. Admin accounts are unaffected.
  See [Access control](docs/guides/access-control.md#numeric-login-for-handheld-scanners).
- **A set's retail box barcode as a second way to look it up.** `wms lego set-info <set>
  --box-barcode <code>` (or the "Box barcode" field on *Set Workshop → Where the Set Is Kept*)
  registers a box's own UPC/EAN against a set; scanning or typing that barcode anywhere a set
  number already works — CLI, TUI, mobile — opens the same set, same as typing its set number.
  Rejects a barcode already registered to a different set. See
  [Sets, missing parts and what to build](docs/guides/sets-and-building.md#your-sets).
- **Check History, searchable by set, checker or date.** *Set Workshop → Reports → Check History*
  lists every finished check and narrows live as you type — part of a set number, a checker's
  username, a date, or "complete" — no separate picker, no row number to hunt for. The CLI's
  `wms lego report history` gained a matching `--by <checker>` filter (part of a username is
  enough), and the mobile app has the same search, admin-only, via `GET
  /mobile/admin/check-history?set=&by=&limit=`. See [Reports](docs/guides/reports.md#from-the-interface).
- **Request a Set**, alongside Request a Feature and Message an Admin: a picker or checker who
  spots a set worth adding to the collection can flag it from *My Settings* or the mobile app's
  `/mobile/request-set` — it lands in Recent Activity (and any configured notification channel)
  without needing admin access. See
  [Assigned work](docs/guides/assigned-work.md#a-few-extras).
- **A visible, always-on MISSING / EXTRA count while checking.** A badge at the top of both the TUI's
  table and guided-walk views now shows the check's running totals the whole time — red for missing,
  blue for extra (a new `Info` style, consistent across every theme) — updated the instant a line is
  marked, including at zero, so a checker never has to scan the whole list or guess where a check
  stands. The guided walk's current-line detail now shows its own extra count too, alongside missing
  and damaged. The mobile app carries the same two numbers (`total_missing`/`total_extra`) on every
  line response for its own equivalent. See
  [Set checks](docs/guides/set-checks.md#the-parts-check).
- **Status cards: a visible reason for idle time, instead of a guess or a nag.** Delay and No Work
  Available cards (`wms attendance card set/clear/list`, *Admin → Open Assignments → V*, or
  `/mobile/admin/status-card*`) are admin-applied and show next to someone's name in the Open
  Assignments queue; the supervisor idle-ticket alert (a claimed ticket quiet for 60+ minutes) skips
  anyone with one on, or anyone on a break, instead of nagging about something already explained.
  Break itself isn't a new concept — a worker can now also toggle their own existing paid break by
  scanning one printed code (`CARD-BREAK`, `wms attendance card-scan`/`POST
  /mobile/attendance/card-scan`) instead of finding the break button, same `StartBreak`/`EndBreak`
  underneath either way. Every card clears itself the moment there's evidence it no longer applies —
  claiming a ticket, or clocking out. See
  [Assigned work](docs/guides/assigned-work.md#status-cards-why-someone-looks-idle-right-now).
- **Per-bag scan gating for parts checks**: before a part can be marked, the checker now has to
  scan or type the ID label of the physical bag it's actually in — scan the wrong one (right set,
  wrong bag) and it's a warning you can see and override, not a hard block; scan a code from a
  different set and it says which one. Bag **kinds** (`small`, `main`, and a new `extra` for
  anything a check turns up that wasn't expected — never printed up front, only once something's
  actually been marked Extra) are a global, admin-extensible registry (`wms lego bag-kind
  add/list`); each set gets its own instance per kind it needs (`75192-A`, `75192-B`, `75192-E`),
  printable in one run with the new `wms lego bag-labels <set>`, and listed on the Bluetooth
  print page's bag dropdown the same way. Finishing a check now holds until every bag kind that
  check actually needed has been scanned at least once, not just the one small-bag confirmation
  the old flow asked for at the end. A set's bag limit (how many instances of a kind it can have)
  and whether gating applies to it at all are both admin-only overrides (`wms lego set-info
  --max-bags`/`--bag-gating`), gated the same way the **V** key's Admin/Picker-Checker toggle
  already is — no new login or 2FA step. Only checks started after this shipped are gated; the
  CLI's non-interactive `wms lego check` is unaffected. See
  [Set checks](docs/guides/set-checks.md#bags-small-main-extra--and-scanning-the-right-one).
- **Reusable bag pool, a lost-bag lookup, per-bag breakdowns, and `wms lego bag-add`** round out the
  bag-scan gating above. `wms lego bag-pool add/assign/list/release` registers a real, permanent
  container under its own stable code and hands it to whichever set's check currently needs it
  instead of printing a fresh one-off code each time — it scans and gates exactly like any other
  bag once assigned, and frees itself back to available the moment that check finishes. `wms lego
  bag-find <code>` (TUI: Set Workshop → **F**; mobile: `GET /mobile/bag-find`) resolves any code —
  scanned or not, a pool bag or a per-check instance, even one only ever printed — to what it
  belongs to, independent of any active checking session, for a bag that turns up without its
  checker. The completion dashboard now splits an incomplete set's shortfall by which bag each short
  part belongs in ("Main 5 · Small Parts 3") instead of one set-wide total, and the missing/extra
  parts reports (`wms lego report missing`/`report extra`) tag each row with its bag the same way.
  `wms lego bag-add <set> <kind>` registers and prints a second (or further) instance of a kind once
  an admin has raised a set's `--max-bags` limit — previously that limit had nothing that could
  actually act on it. See
  [Set checks](docs/guides/set-checks.md#a-reusable-bag-pool).
- **Bluetooth printing for the small-bag label (Labelnize/Phomemo PM260 and
  compatible M-series printers)**: a new page, `/mobile/print`, that signs in
  the same way the mobile app does and then talks to the printer directly
  over Web Bluetooth from whatever phone or laptop actually has it open — the
  server itself never touches Bluetooth, since it's rarely anywhere near the
  printer. Needs Chrome or Edge (Safari/iOS has no Web Bluetooth) and HTTPS.
  `labels.Raster1bpp` turns a label into the printer's own packed 1-bit
  raster format, centered within its 72-byte head width, reusing the same
  rendering `wms lego small-bag-label`'s PNG export already uses so the two
  can't drift apart. See [Set checks](docs/guides/set-checks.md#printing-straight-from-a-labelnizephomemo-pm260-over-bluetooth).
- **Paper-first checklist workflow**: `wms lego paper-done <set>` marks a set's paper checklist
  finished without touching any counts — it opens (or resumes) that set's check, so `wms lego
  check` later resumes the exact same session and the set drops off `wms lego checks pending`
  automatically. `wms lego attach <set> <scan.jpg|png|pdf>` verifies the file's actual content
  (not just its extension) before storing a copy next to `lego.db` as proof the paper count was
  right, with `--flag "reason"` to mark a check disputed for review if the CLI and paper counts
  ever disagree — on its own this changes no numbers and no accuracy score. The login banner,
  `wms sys status`, and a new **Set Workshop → 9** screen all surface the pending count. The
  **Parts Stock-take** sheet (`wms lego report stocktake`) now prefills its header, condition,
  cost and part/minifig counts from what's already known, and shows whether a proof was filed.
  See [Reports](docs/guides/reports.md#paper-first-check-on-paper-enter-it-in-the-cli-later).
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
- **Reprinting a bag label could print the wrong code, or silently miss some bags entirely.**
  `wms lego bag-labels`, the Bluetooth print page, and `/mobile/print/bags` all recomputed a bag's
  code from its kind (small/main/extra) via the deterministic formula — only ever correct for that
  kind's first, auto-generated instance. A reusable pool bag's own chosen code, a second same-kind
  instance ("-A2"), or a custom bag kind beyond the built-in three either printed a wrong label or
  never showed up to print at all. All three now read every bag actually registered on the set (new
  `DB.PrintableBags`) and print its own true code — reprinting a damaged or unscannable label is now
  just running the same command or picking it from the same list again, for any kind of bag. See
  [Set checks](docs/guides/set-checks.md#reprinting-a-damaged-or-unscannable-label).
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
