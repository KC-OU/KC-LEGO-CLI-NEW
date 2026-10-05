# Assigned work: pickers, checkers, accuracy and messages

A picker or checker signs on to a dedicated, tight hub — Overview, a Request screen, My Current
Jobs, a Query submenu (a set/part search folded into one line, Owned Parts, Owned Sets), My
accuracy, My Exports, LEGO Collection, Part-DB Hub, Log out, Exit — instead of the general
ModernWMS/Part-DB hub, since their day genuinely doesn't touch most of it. Anyone whose
permissions go beyond that (an operator, an admin) keeps the general hub even if they also hold
`sets.check`/`orders.manage`.

LEGO Collection and Part-DB Hub are there for **browsing** — searching, checking what's missing,
looking something up — not editing; both `checker` and `picker` already carry the read
permissions that show them by default, and an admin can turn that off for one person (or on for
someone in a different group) from [Access Control's Users list](access-control.md#in-the-terminal).

## Roles

`picker` and `checker` are ordinary starter groups (*Admin → Access Control → Groups*, or `wms
access groups set`) — a user can hold either, both, or neither. Which hub someone sees depends on
which permission they actually hold (`orders.manage` → picker, `sets.check` → checker), not the
literal group name. The `checker` group already grants **both** `sets.check` and `orders.manage`
— every checker is dual-skilled by default, able to pick as well as check.

For a dual-skilled account, which kind of work the hub menu, the Request screen and My Accuracy
frame itself around follows **whatever ticket is currently claimed**, automatically:

- Claim an order and the menu says "Request an order to pick", My Accuracy shows picker numbers,
  and so on — picker framing throughout, for as long as that order is claimed.
- Finish it (or it's taken off you) and claim a check instead, and everything switches back to
  checker framing the same way — nothing to toggle by hand, no setting to remember to flip.
- With **nothing** currently claimed — the moment you're actually looking at the Request
  screen to find work — there's no ticket to read the role from, so it shows **both** kinds of
  open work at once, tagged `[Check]`/`[Order]`, rather than guessing one and hiding the other.

A single-skilled account (only `picker` or only `checker`, not both) is unaffected by any of this
— it only ever sees its one role, same as before.

## Switching to Admin (the V key)

A checker or picker who needs to do one admin thing — assign work, dock someone's accuracy,
message someone — doesn't have to sign out and back in. Press **V**:

- If your own account already holds admin-level access too, it's a straight jump to Admin and
  back, same as the existing **G** key's Main Hub ↔ LEGO Collection toggle.
- Otherwise it asks for a **different** account's username and password — deliberately **no
  authenticator code**, since this session is already signed in and verified; entering a second
  account's password is the same trust level *Abandon without saving* already asks for. Get it
  right and you're switched to that admin account, with **V** now returning you to exactly where
  you were — no credentials needed to switch back down.

Every switch and every return is audited (`ADMIN_QUICK_SWITCH`), and getting the admin password
wrong denies it and logs that too. Picking a user for **Assign Work** or **Message a User**
narrows as you type — by username, part of it, or the numeric user ID shown next to it — so
neither one means scrolling a long list.

## Assignment

*Admin → Assign Work* points a named person, or the open queue (anyone qualified claims it
first-come), at a set to check or an order to pick — *Admin → Live Sessions* and *Open Queue*
(inside Assign Work) show what's outstanding and who's on what. Claiming a ticket doesn't copy
or duplicate anything: it just opens the normal check/order screen, exactly as if you'd gone
there yourself — the ticket only remembers who's on it.

**Scan-to-claim**: every ticket gets a barcode claim token (reachable from the palette, "Scan a
job ticket") — printing it on a physical ticket alongside the usual Code 128 part/set labels lets
someone claim and open a job by scanning it, no menu navigation needed.

**From the phone**: an admin can do the same assigning without a terminal — `GET
/mobile/admin/tickets` (the open queue, who's claimed what and for how long), `GET
/mobile/admin/users` and `POST /mobile/admin/assign-ticket`. Gated the same way *Assign Work*
is in the TUI; no mobile screen calls these yet, so for now this is reachable from a REST client
or whatever mobile screen is built against it next.

**Flagging a wrong location**: `POST /mobile/flag-location` lets a picker or checker report a
part that genuinely isn't where the system says — it messages admins immediately (the same path
as *Message an Admin*) and doesn't block or change the line; they keep working.

**Setting up a new phone**: `wms sys mobile-setup-qr <url>` prints the sentry-wms mobile app's
server URL as a QR code — scan it on the app's first-run SERVER URL screen ("SCAN QR CODE
INSTEAD") instead of typing it by hand. The URL is sentry-wms's own Flask API address (a separate
deployment this project doesn't own — e.g. `https://wms-mobile.example.com`), saved once
(Settings-style) so later runs of the command don't need it given again; `wms sys mobile-setup-qr`
with no argument reprints the saved one.

## How long it's taking

A check or order claimed through a ticket shows **how long it's taken so far**, and once there's
a bit of history, **how long one usually takes** — in the TUI, in the check screen's status line
(`12m so far (usually ~20m)`); on the phone, on every `/mobile/next`/`/mobile/confirm` response
(`elapsed_seconds`/`estimated_seconds`) and in the admin ticket list (`GET /mobile/admin/tickets`,
`claimed_for_seconds`/`estimated_seconds` — the supervisor live-status view's data). The estimate
is a plain average of how long recently *finished* tickets of that kind took, not a per-part
calculation — there's no reliable link from a ticket back to exactly which check or order row it
produced (a set can be recounted many times), so it's "a check like this usually takes about this
long", not a precise countdown. With no finished-ticket history yet, nothing is shown rather than
a made-up number.

## Leaving a job

Pressing your usual back key (Esc/F3/F12) while a ticket-backed check or order is open asks:

- **Save and come back later** — exactly today's normal behaviour: the draft is already
  persisted as you go, and the ticket stays reserved for you until you return.
- **Finish now** — the same as pressing F to finish a check, or marking an order received.
- **Abandon without saving** — discards this attempt's progress entirely (a checker's draft is
  deleted, not just left half-filled) and releases the ticket back to the open queue. This
  requires a second admin's ID and password, then a reason (a couple of common ones, or your
  own text) — the admin is notified and it's all audited. It's a conduct/process record, kept
  completely separate from accuracy.

## Forcing someone off a job

*Admin → Assign Work → Force off a job* takes a claimed ticket away from whoever has it — for
when a job needs to be redirected, not because anyone did anything wrong. Pick the claimed
ticket, choose where it goes next (the open queue, or straight to a named person), then send a
message — the default is *"We've assigned you another task — don't worry, your accuracy won't be
affected,"* or write your own. It never touches accuracy (same "conduct record, not a score hit"
rule as Abandon), and the underlying check/order draft is untouched — whatever was tallied so far
is exactly where the next person (or the same person, on a different job) finds it. If the person
taken off it is mid-check or mid-order right now, their own screen notices within about 15 seconds
(the same background check that delivers messages) and returns them to their hub with the message
waiting.

## Reopening a finished check or order

A set's Completion dashboard (*LEGO → 8 Set Workshop → 2*) has an **R** key next to a checked
set: when a check turns out to have been done wrong — miscounted, missed a line — this puts a
fresh ticket straight back in the open queue for anyone to claim, without touching the finished
one it's correcting. It asks for a reason first (required), and that reason is recorded on the
new ticket's own note, same as the rest of this page's audit trail — nothing about the original
finished check is rewritten or deleted; the "this was redone" record sits beside it. Claiming the
new ticket on an already-checked set is automatically a **recount**, same as pressing K yourself,
so there's nothing extra to set up. Reached from the phone the same way (an admin action,
`POST /mobile/admin/reopen-ticket`); orders can be reopened the same way from there even though
there's no dedicated TUI key for it yet.

## Accuracy

*My Accuracy* (also in the palette) shows today's percentage and the last 7 days, per role — a
picker and checker score are independent for someone holding both. It starts at 100% each day
(nothing to reset: "today" is simply today's rows) and only moves when:

- **A completed check or order is missing parts**: the deduction scales with how many are
  missing — roughly 2–6% for 1–5, 10–14% for 6–15, 15–20% for 16–20, linearly within each band.
  **21 or more never deducts automatically** — it's flagged for a manual conversation instead
  (an external notification fires, `accuracy_escalation`).
- **A completed, fully-correct check or order**: claws back half of today's remaining deficit —
  several clean ones in a row recovers the rest, rather than one lucky set wiping a bad day.
- **An admin manually docks it** (*Admin → Dock Accuracy*, with a required reason) — the system
  itself never decides that 21+ missing costs anything; a person does.

## Messages

*Admin → Message a User* sends a one-way note — a few quick picks, or your own text — that shows
up **full-screen** on the recipient's own screen, the same "read it, press a key to clear it"
treatment the first-run tour uses, so the message is never cramped or cut off. It appears the
moment you reach the hub if one was waiting when you signed in, and mid-session it's checked
every 15 seconds (sessions are separate processes, so this is a poll, not a push). Several at
once page through one at a time ("1 of 3" …), **Enter** for the next, **Q** to dismiss all of
them at once.

## Admin activity feed

*Admin → Recent Activity* is a scannable log of the last 50 admin-relevant events — messages sent
(who to whom), accuracy docked or escalated, sets short on parts, and jobs forced off or
reassigned. It's separate from the tamper-evident audit log (still the record of everything, for
security review): this is a quick "what happened while I was away" screen, and — when a
[notification channel](discord-notifications.md) is configured — the same events are pushed there
too.

## A few extras

- **My Exports** (My Settings, or the palette): your own recent export files, with a one-key
  "get me a fresh download link" for one whose original link or file has expired — no terminal
  needed.
- **Shift handover note** (*Admin → Shift Handover Note*): one free-text note anyone leaves for
  whoever's on next, shown full-screen (alongside any pending messages) the next time a
  picker/checker signs on.
- **Shelf order**: a check or order's lines are listed in Part-DB shelf-location order rather than
  catalog order (parts with no known location yet sort last), so working through one is a single
  pass instead of back and forth.
- **My Rebrickable API key** (My Settings): use your own personal key instead of the shared one,
  or switch back with "Use the shared/global key" — a personal key that stops working isn't
  detected automatically (it just fails like any other Rebrickable error would); switch back
  the same way.
- **Show/hide pictures while checking or picking** (My Settings): off by default; turns on a
  colour swatch and the part's (or set's) picture in the [guided check/pick walk](set-checks.md#guided-walk-and-pictures),
  for anyone who finds a picture faster to confirm against than reading a colour name off the screen.
