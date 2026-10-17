# Kanban board

A read-only view over the existing work-assignment system ([Assigned work](assigned-work.md)) —
backlog, active and done jobs, at a glance, with notes. It never changes a job's state itself:
claiming, finishing or abandoning still happens on the normal check/order screens, exactly as
before. The board is a window onto that data, not a second way to work it.

On a terminal 70 columns or wider, Backlog/Active/Done render as three bordered columns side by
side with a live `Backlog: N | Active: N | Done: N` count, each card a short bulleted line. A
narrower terminal (a small telnet pane, some mobile clients) falls back to the same information
as a plain stacked list instead of squeezing three boxes into too little width.

## New sets start in a backlog, not checkable right away

Adding a brand-new set (*LEGO Collection → Add a Set*) now queues it in the kanban backlog instead
of offering to check it immediately — someone may already be mid-check elsewhere, and starting a
fresh one on top of that isn't always the right moment. An admin releases it from the backlog when
it's a good time (*Admin → Kanban Board → Release a backlog set…*, or `wms` has no CLI equivalent
for this — it's TUI-only). Re-adding a set you already own (just updating how many you have) is
unaffected — that one was released long ago.

The backlog list itself is visible to everyone who can check sets (`sets.check`), not just admins
— *Kanban → View the backlog* from the picker/checker hub, or *Admin → Kanban Board → View the
backlog*.

## Your own board (pickers and checkers)

**Kanban** on the picker/checker hub (replacing the old "My Current Jobs" entry) shows:

- Your current job, if you have one (still one at a time, same as always), with a resume action
  that opens it exactly like before.
- Your recent finished jobs.
- The shared backlog.
- **Add a note** — a short free-text note on your current job, either private (only you and
  admins see it) or shared with admins. Notes are append-only: there's no edit or delete, a
  correction is a follow-up note, same treatment this project gives other work records.

You never see anyone else's card here — only your own.

## The admin board

*Admin → Kanban Board* (also reachable from the main hub's own Kanban entry):

- **View all boards** — everyone's checks and picks together, grouped Backlog / Active / Done,
  each card tagged with who's claimed it. A missing/extra badge (`M3 E1`, same convention as the
  table check view) shows on check cards; an "(abandoned)" tag marks a ticket someone gave up on,
  folded into the Backlog column rather than its own.
- **View a specific user's board** — the same thing, filtered to one person.
- **Release a backlog set…** — the admin half of the backlog gate above.

The board refreshes itself every time you open or re-enter it (there's nothing to press) — it's
always reading live data, just not polling in the background while you're looking at something
else.

## A line with a very large quantity gets moved to the back

A check line at or above a threshold quantity (default 50, admin-tunable:
`KANBAN_BACKLOG_QTY_THRESHOLD`) is flagged automatically when the check is saved — it stays part
of the same job, just sorted to be done last, so a 200-piece line doesn't block everything behind
it. Nothing is pulled out into a separate queue.

## Jump straight there: Shift+F1

See [Keys, palette and accessibility](keys-and-accessibility.md#keys) — Shift+F1 (F13 as a
fallback) opens the board from either hub.

## Accuracy, always visible

Every screen's header now shows the signed-in user's current accuracy next to their name — e.g.
`7354 - Kieran Collins (100%)` — for any account with a scored role (picker or checker). It's the
same automatically-computed figure [My Accuracy](assigned-work.md) already showed, just always on
screen instead of one menu away; there's no separate admin override, and the existing sign-on
redirect straight to the full accuracy/history screen for pickers and checkers is unchanged.

The display name part (`Kieran Collins`) is new: a numeric-login Picker/Checker account's
"username" *is* its ID, so there was nowhere a real name lived before. Set one with:

```
wms access user set-name 7354 "Kieran Collins" --source local
```

(swap `--source local` for whichever source the account actually uses — `modernwms` or `partdb`).
An account with no name set just shows its ID, no dangling punctuation.

## What this doesn't do

- No drag-and-drop or any other way to change a card's state from the board — that's deliberate;
  claiming, finishing and abandoning jobs stay exactly where they already were.
- Pickers and checkers are still one job at a time — the board doesn't change that.
- No mobile app view and no separate `wms kanban` CLI command yet — TUI/telnet only for now.
