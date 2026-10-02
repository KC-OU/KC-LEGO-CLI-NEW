# Remote-bot API: setup, direct use, and management

A small, loopback-only webhook that lets a linked admin trigger a handful of WMS admin actions —
reassigning a picker/checker's ticket, messaging a user, logging off a stuck session, checking
what's open — from outside the TUI entirely. This is the base API and stands on its own: anything
that can POST JSON to `127.0.0.1` can drive it (a shell script, a cron job, Postman, a systemd
timer). **If you specifically want to trigger it from Discord or Slack chat messages**, that's a
separate guide, [Remote bot via Discord/Slack and n8n](remote-bot-n8n.md), which builds on top of
the setup here rather than repeating it.

This is also separate from [Discord export DMs](discord-notifications.md) (WMS → you, a one-way
notification) and the [low-stock/price-drop webhooks](notifications.md) (WMS → a channel) — this is
the only one of the three that lets something outside WMS *do* something inside it, which is why it
has its own credential, its own lockout, and its own section below on keeping an eye on it.

## Part 1 — Set up

1. **Give yourself a real Access Control entry first, if you don't have one.** Admin → Access
   Control → Users → **N**, with at least the `admin` group (or whatever group already covers what
   you need). The bot-link screen below refuses to run otherwise — a bare entry with nothing but a
   bot credential on it would leave you with *zero* policy permissions the instant it's saved, not
   more, since having any entry at all switches you onto the policy's own permission list instead of
   your legacy role.
2. **Link an identifier and a PIN.** Admin → Access Control → **My remote bot link** → *Set up /
   change my link, PIN, security question*. Fill in:
   - **Discord and/or Slack user ID** — this is just the lookup key the API uses to find your
     credential; it doesn't have to belong to a Discord/Slack account that actually sends it
     messages. If you're only ever going to call the API directly (this guide, not n8n), pick any
     stable string you'll remember and put it in the Discord ID field — nothing checks that it's a
     real platform account.
   - **A bot PIN** — separate from your real WMS password. This is the one secret every call has to
     supply, so treat it like a PIN, not a password you reuse anywhere else.
   - **A security question and answer** — only used later, to reset the PIN if you forget it.
3. **Turn the listener on.** Set `WMS_BOT_API_PORT` (Admin → Settings, or directly) and restart
   `wms-gateway.service`. It always binds `127.0.0.1` only — never configurable wider, on purpose,
   since this is a new credentialed surface and every caller is expected to run on the same box.
4. **Confirm it's up:**
   ```bash
   curl -s http://127.0.0.1:$WMS_BOT_API_PORT/bot/action \
     -d '{"platform":"discord","id":"your-id","pin":"your-pin","action":"list_open_tickets"}'
   ```
   A JSON reply (even an error one) means it's listening; "connection refused" means the port isn't
   set or the gateway hasn't restarted since you set it.

## Part 2 — API reference

### `POST /bot/action`

| `action` | `params` | Does the same thing as |
|---|---|---|
| `reassign_ticket` | `{"ticket_id": 42, "to": "pat"}` (`to: ""` releases it to the open queue) | Admin → Assign Work → force off |
| `message_user` | `{"username": "pat", "body": "..."}` | Admin → Message a User |
| `kick_session` | `{"session_id": "...", "message": "..."}` | Admin → Live Sessions → K |
| `list_open_tickets` | *(none)* | the Assign Work list — read-only |

```json
{"platform": "discord", "id": "your-id", "pin": "your-pin", "action": "reassign_ticket", "params": {"ticket_id": 42, "to": "pat"}}
```

A wrong ID or a wrong PIN both come back as the same `401` — on purpose, so a caller can't tell
which IDs are actually linked. Every action needs the exact permission its TUI equivalent already
requires (`users.view`, the same gate as the whole Admin submenu) — this door can never do more than
that admin could already do by signing in. Responses are always JSON: `{"ok": true, ...}` on
success, `{"error": "..."}` with `400` (bad request), `401` (bad credentials), `403` (permission
denied), `429` (locked — see Part 4) or `500` otherwise.

### `POST /bot/reset-pin`

Needs a live code from your *real* WMS 2FA **and** the security answer — either alone is refused,
same rule as the TUI's own reset screen:

```bash
curl -s http://127.0.0.1:$WMS_BOT_API_PORT/bot/reset-pin \
  -d '{"platform":"discord","id":"your-id","code":"482913","answer":"rex","new_pin":"5678"}'
```

## Part 3 — Calling it directly (no Discord, Slack, or n8n at all)

Nothing about the API requires a chat platform in the loop. Some uses that don't:

- **A one-off from the shell**, exactly like the curl examples above.
- **A cron job / systemd timer** that checks `list_open_tickets` on a schedule and does something
  with the result (write it to a file, email it, whatever you'd otherwise reach for cron to do).
- **A small script of your own** in any language that can POST JSON — this is a plain HTTP+JSON
  API, nothing n8n- or Discord-specific about the wire format itself.

If this is all you need, you're done — Part 4 below covers living with it day to day. Only read
[the n8n guide](remote-bot-n8n.md) if you actually want chat messages to trigger it.

## Part 4 — Managing it day to day

**Watch what it's doing.** Every action lands in Admin → Recent Activity exactly like its TUI
equivalent, with "(bot)" in the detail text so you can tell the two apart at a glance — and in the
audit log the same way (`TICKET_FORCED_OFF`, `MESSAGE_SENT`, `SESSION_KICKED`), both searchable the
normal way.

**A locked-out PIN** (five wrong attempts) clears itself after five minutes — there's currently no
admin "unlock now" action; the wait is the fix. If it keeps happening, someone's guessing, which is
worth asking about before just waiting it out.

**Changing the PIN** — same *Set up / change my link, PIN, security question* screen; leave the
Discord/Slack ID and security Q&A fields blank to keep them, fill in only the PIN fields.

**Forgot the PIN** — *Reset my bot PIN* on the same hub (needs your 2FA + security answer), or the
`/bot/reset-pin` call above if you've got another way to reach it but not the TUI.

**Turning it off for one admin** — there's no dedicated "unlink" button yet. Setting a new PIN only
you know effectively disables the old one immediately; removing your whole Access Control entry
(Users → **X**) removes the bot link along with everything else on it, which is heavier than most
people want just for this.

**Turning it off entirely** — unset `WMS_BOT_API_PORT` and restart `wms-gateway.service`. No linked
account's bot PIN is reachable until it's set again.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `connection refused` | `WMS_BOT_API_PORT` isn't set, or the gateway hasn't restarted since you set it |
| `401` on every request | Check the platform/id match exactly what's linked (Admin → Access Control → Users → your row), and that the PIN is right — both failure modes look identical on purpose |
| `429` | Five wrong PINs in a row — wait five minutes, or check Recent Activity for who's been guessing |
| `403` | The linked admin's account doesn't hold `users.view` — check their group in Access Control → Users |
| Reset always `401` | The 2FA code and the security answer are both required in the *same* request — a right answer with a stale/reused code still fails |
