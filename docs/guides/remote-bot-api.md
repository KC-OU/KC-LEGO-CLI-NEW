# Remote-bot API: setup, direct use, and management

## What this actually is, in plain English

Normally the only way to do admin things in WMS — reassign a ticket, message a user, kick a stuck
session — is to sign into the TUI yourself and click through the menus. The remote-bot API is a
second, small door into those same four actions that doesn't need the TUI open at all: something
else (a script, a scheduled job, a chat bot) sends it one JSON message over HTTP, and it does the
action and replies. That's the whole idea — it's not a new feature, it's a second way to trigger a
handful of existing admin features.

It only ever listens on `127.0.0.1` (this server, talking to itself) — nothing on the internet, or
even your home network, can reach it directly. It is off by default (no port set = the listener
doesn't start at all), and every request to it has to prove who's calling with an ID + PIN, separate
from your real WMS login.

**The four things it can do**, each the same as an existing TUI action:

| What it does | Same as, in the TUI |
|---|---|
| Reassign a ticket to someone else (or release it) | Admin → Assign Work → force off |
| Send a message to a user | Admin → Message a User |
| Log a stuck session off | Admin → Live Sessions → K |
| List what tickets are currently open | Admin → Assign Work (read-only) |

Anything that can send an HTTP request with a JSON body can drive it — a one-off command you type
yourself (this guide), a scheduled job, or a chat bot reacting to Discord messages. Two separate
guides build on top of this one: [Remote bot via Discord/Slack and n8n](remote-bot-n8n.md) (a
workflow tool decides what to call) and [Remote bot via a Discord slash command](remote-bot-discord.md)
(no n8n — Discord calls WMS directly). Set this guide up first regardless of which, or neither, you
use.

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
     stable string you'll remember — e.g. `me` or `admin1` — and put it in the Discord ID field;
     nothing checks that it's a real platform account.
   - **A bot PIN** — separate from your real WMS password. This is the one secret every call has to
     supply, so treat it like a PIN, not a password you reuse anywhere else.
   - **A security question and answer** — only used later, to reset the PIN if you forget it.
3. **Turn the listener on.** This part has no TUI screen (unlike API keys/theme/2FA, which do live
   under Admin → Settings) — it's a server-side setting, turned on the same way the person running
   the server turns on any other optional port in this app:
   - Open the settings file the server already uses for its other saved keys/tokens — by default
     `/root/docker-server/wms/settings.json` (root-only; this is the same file your Rebrickable key
     and Part-DB token already live in, just never printed in any doc since it holds secrets).
   - Add `"WMS_BOT_API_PORT": "7699"` to it (pick any free local port — 7699 is just an example;
     check nothing else already uses it first).
   - Restart the gateway so it picks the new setting up: `systemctl restart wms-gateway.service`.
   - It always binds `127.0.0.1` only, no matter what port you pick — never configurable wider, on
     purpose, since this is a new credentialed surface and every caller is expected to run on the
     same box.

   If you'd rather not edit that file by hand, this is exactly the kind of one-line server change
   that's easy to hand off — just say which port you want turned on.
4. **Confirm it's up:**
   ```bash
   curl -s http://127.0.0.1:7699/bot/action \
     -d '{"platform":"discord","id":"your-id","pin":"your-pin","action":"list_open_tickets"}'
   ```
   (Swap `7699` for whatever port you actually set in step 3, and `your-id`/`your-pin` for what you
   entered in step 2.) `curl` is just a command-line tool for sending an HTTP request — this one
   sends the text after `-d` as the request's body. A JSON reply (even an error one, like
   `{"error":"bad credentials"}`) means the listener is up and your request reached it;
   `curl: (7) Failed to connect` / "connection refused" means the port isn't set yet or the gateway
   hasn't been restarted since you set it.

## A full worked example

Say your port is `7699`, your linked ID is `me`, and your PIN is `4821`. To see what tickets are
currently open:

```bash
curl -s http://127.0.0.1:7699/bot/action \
  -d '{"platform":"discord","id":"me","pin":"4821","action":"list_open_tickets"}'
```

That one line is doing exactly what clicking into Admin → Assign Work does — it's just typed instead
of clicked. The `-d '...'` part is the request's body: a JSON object (a `{key: value, ...}` blob)
telling the API who you are (`platform`+`id`+`pin`) and what you want (`action`, plus `params` if
that action needs any — see the table in Part 2). You get back one line of JSON in reply, e.g.
`{"ok":true,"tickets":[...]}` on success or `{"error":"bad credentials"}` if the ID/PIN was wrong.

To reassign ticket 42 to `pat` instead, it's the same shape with a different `action` and a `params`
object added:

```bash
curl -s http://127.0.0.1:7699/bot/action \
  -d '{"platform":"discord","id":"me","pin":"4821","action":"reassign_ticket","params":{"ticket_id":42,"to":"pat"}}'
```

Every other action in the table below works the same way: same `platform`/`id`/`pin`, a different
`action`, and whatever `params` that row lists.

## Part 2 — API reference

### `POST /bot/action`

| `action` | `params` | Does the same thing as |
|---|---|---|
| `reassign_ticket` | `{"ticket_id": 42, "to": "pat"}` (`to: ""` releases it to the open queue) | Admin → Assign Work → force off |
| `message_user` | `{"username": "pat", "body": "..."}` | Admin → Message a User |
| `kick_session` | `{"session_id": "...", "message": "..."}` | Admin → Live Sessions → K |
| `list_open_tickets` | *(none)* | the Assign Work list — read-only |
| `list_alerts` | *(none)* | Admin → Alerts — open accuracy escalations needing a manual review, read-only |

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
curl -s http://127.0.0.1:7699/bot/reset-pin \
  -d '{"platform":"discord","id":"your-id","code":"482913","answer":"rex","new_pin":"5678"}'
```

## Part 3 — Other ways to call it (still no Discord, Slack, or n8n)

The worked example above was one `curl` command typed by hand, but nothing about the API requires
typing it live every time — it's a plain HTTP+JSON endpoint, so anything that can send one works:

- **A cron job / systemd timer** that calls `list_open_tickets` on a schedule and does something
  with the result (write it to a file, email it, whatever you'd otherwise reach for cron for).
- **A small script of your own**, in any language — Python's `requests`, Node's `fetch`, etc. — built
  the same way as the curl examples: POST that same JSON body to the same URL.

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

**Turning it off entirely** — remove the `WMS_BOT_API_PORT` line from the settings file (or set it to
`""`) and restart `wms-gateway.service`. No linked account's bot PIN is reachable until it's set
again.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `connection refused` | `WMS_BOT_API_PORT` isn't set, or the gateway hasn't restarted since you set it |
| `401` on every request | Check the platform/id match exactly what's linked (Admin → Access Control → Users → your row), and that the PIN is right — both failure modes look identical on purpose |
| `429` | Five wrong PINs in a row — wait five minutes, or check Recent Activity for who's been guessing |
| `403` | The linked admin's account doesn't hold `users.view` — check their group in Access Control → Users |
| Reset always `401` | The 2FA code and the security answer are both required in the *same* request — a right answer with a stale/reused code still fails |
