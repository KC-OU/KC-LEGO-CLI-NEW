# Assigned work: pickers, checkers, accuracy and messages

A picker or checker signs on to a dedicated, tight hub — Overview, a Request screen, My Current
Jobs, a Query submenu (a set/part search folded into one line, Owned Parts, Owned Sets), My
accuracy, My Exports, Log out, Exit — instead of the general ModernWMS/Part-DB hub, since their
day genuinely doesn't touch most of it. Anyone whose permissions go beyond that (an operator,
an admin) keeps the general hub even if they also hold `sets.check`/`orders.manage`.

## Roles

`picker` and `checker` are ordinary starter groups (*Admin → Access Control → Groups*, or `wms
access groups set`) — a user can hold either, both, or neither. Which hub and which kind of work
("Request an order to pick" vs "Request a set to check") someone sees depends on which
permission they actually hold (`orders.manage` → picker, `sets.check` → checker), not the literal
group name.

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

## A few extras

- **My Exports** (My Settings, or the palette): your own recent export files, with a one-key
  "get me a fresh download link" for one whose original link or file has expired — no terminal
  needed.
- **Shift handover note** (*Admin → Shift Handover Note*): one free-text note anyone leaves for
  whoever's on next, shown full-screen (alongside any pending messages) the next time a
  picker/checker signs on.
- **My Rebrickable API key** (My Settings): use your own personal key instead of the shared one,
  or switch back with "Use the shared/global key" — a personal key that stops working isn't
  detected automatically (it just fails like any other Rebrickable error would); switch back
  the same way.
