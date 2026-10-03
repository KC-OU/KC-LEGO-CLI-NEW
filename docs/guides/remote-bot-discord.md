# Remote bot via a Discord slash command (no n8n)

This wires a Discord `/wms` slash command straight to the [remote-bot API](remote-bot-api.md)'s
four actions — no n8n, no workflow to build. Discord calls WMS directly over HTTPS whenever you type
the command; WMS verifies the request really came from Discord (a cryptographic signature, not a
password) and runs the action.

**Do [the setup in the API guide](remote-bot-api.md#part-1--set-up) first** — a real Access Control
entry and a bot PIN. You don't need `WMS_BOT_API_PORT` turned on for this guide specifically (this
uses its own route), though there's no harm leaving it on too if you also use the n8n guide.

**One difference from that guide that matters here:** it says the Discord ID field can be any
string you'll remember, since n8n is the one constructing that field and can send anything. That's
not true for this guide — Discord itself fills in your *real* account ID on every slash command, so
whatever you link here must be that exact number, not a made-up placeholder. To get it: in Discord,
Settings → Advanced → turn on **Developer Mode**, then right-click your own name/avatar anywhere →
**Copy User ID**. Use that (a long number, not your username) as the Discord ID when setting up your
link. Linking anything else — including something that happens to match your WMS username — fails
every command with "unauthorized," no matter how right the PIN is.

## How this is different from the n8n guide

n8n needed a Discord bot that stays logged in, listening for messages, and WMS's bot API only
listens on `127.0.0.1` — so n8n had to be bridged onto the same loopback to reach it (the whole
"one gotcha" section in that guide). This is simpler: Discord's slash commands work by Discord
itself calling a URL you give it, once, whenever someone runs the command. That means:

- No persistent bot process to keep running or restart if it crashes.
- No new Cloudflare Tunnel hostname — the URL is just a new *path* on a domain you already have
  pointed here (`tui.example.com`), so there's one less thing to add in Cloudflare.
- Instead of a loopback check, every request is verified with a signature Discord attaches — only
  Discord's real servers can produce a valid one.

## Part 1 — Create the Discord application

1. Go to [discord.com/developers/applications](https://discord.com/developers/applications) → **New
   Application**. Name it anything (e.g. "WMS Bot").
2. On **General Information**, copy the **Public Key** — a long hex string. This is what proves a
   request really came from Discord; it's not a secret (it's a *public* key), but keep it exact.
3. Go to the **Bot** tab → **Add Bot** if one wasn't created automatically. Copy its **Token** too —
   you only need this once, to register the command in Part 2, not for ongoing use.
4. Go to **OAuth2 → URL Generator**. Tick scopes **`bot`** and **`applications.commands`**, then
   open the generated URL and invite it to a server you're in.

## Part 2 — Register the `/wms` command

This is a one-time setup call to Discord's API — paste your own Application ID (General Information
page) and Bot Token (from step 3 above) into this and run it once from any shell:

```bash
curl -X PUT "https://discord.com/api/v10/applications/YOUR_APPLICATION_ID/commands" \
  -H "Authorization: Bot YOUR_BOT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '[{
    "name": "wms",
    "description": "WMS admin actions",
    "options": [
      {"type":1,"name":"reassign","description":"Reassign or release a ticket","options":[
        {"type":4,"name":"ticket_id","description":"Ticket ID","required":true},
        {"type":3,"name":"pin","description":"Your bot PIN","required":true},
        {"type":3,"name":"to","description":"Username to assign to (blank releases it)","required":false}
      ]},
      {"type":1,"name":"message","description":"Message a user","options":[
        {"type":3,"name":"username","description":"Who to message","required":true},
        {"type":3,"name":"body","description":"Message text","required":true},
        {"type":3,"name":"pin","description":"Your bot PIN","required":true}
      ]},
      {"type":1,"name":"kick","description":"Kick a live session","options":[
        {"type":3,"name":"session_id","description":"Session ID","required":true},
        {"type":3,"name":"message","description":"Reason shown to the user","required":true},
        {"type":3,"name":"pin","description":"Your bot PIN","required":true}
      ]},
      {"type":1,"name":"tickets","description":"List open tickets","options":[
        {"type":3,"name":"pin","description":"Your bot PIN","required":true}
      ]},
      {"type":1,"name":"alerts","description":"List open accuracy alerts","options":[
        {"type":3,"name":"pin","description":"Your bot PIN","required":true}
      ]}
    ]
  }]'
```

If you already registered `/wms` before `alerts` existed, re-run this same command — it's a full
replace, not an append, so it picks up the new subcommand along with the existing ones.

A JSON array in reply (one object, `"name":"wms"`) means it registered. It can take up to an hour to
show up for everyone the first time (Discord's own caching, nothing to do with WMS), though usually
it's much faster.

## Part 3 — Turn the WMS side on

Same place as every other optional setting in this project — the settings file, not a TUI screen
(see [the API guide's Part 1, step 3](remote-bot-api.md#part-1--set-up) for exactly where that is
and why). Add:

```json
"WMS_DISCORD_PUBLIC_KEY": "the hex string from step 2 above"
```

and restart `wms-gateway.service`. This is the only thing that turns the route on — it's off
entirely (not reachable, 404) until this is set, same "empty = off" convention as everything else
optional here. As with the bot API port, this is a one-line server change — just ask if you'd rather
not edit it yourself.

## Part 4 — Point Discord at it

Back on the **General Information** page in the Developer Portal, set **Interactions Endpoint URL**
to:

```
https://tui.example.com/discord/interactions
```

(or whichever public hostname you already have pointed at the main WMS web gateway — it doesn't need
to be a new one). Discord immediately sends a test request to that URL and only saves it if WMS
responds correctly — so do Part 3 first, or this step will fail with "could not be verified."

## Using it

In any server the bot's been invited to:

```
/wms tickets pin:1234
/wms reassign ticket_id:42 pin:1234 to:pat
/wms message username:pat body:"please double check your last set" pin:1234
/wms kick session_id:abc123 message:"logged off remotely" pin:1234
/wms alerts pin:1234
```

Every reply is visible only to you (Discord calls this "ephemeral") — the same instinct as the API's
collapsed 401s: a bot action's result isn't meant for the whole channel to see.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| "The interaction failed" | Usually a wrong PIN or missing permission — same causes as the API guide's table, just surfaced as a Discord error message instead of JSON |
| `❌ unauthorized` even with the right PIN | The linked Discord ID isn't your *real* account ID — see the callout above. A placeholder string (like a WMS username) will never match what Discord actually sends |
| Discord won't save the Interactions Endpoint URL | `WMS_DISCORD_PUBLIC_KEY` isn't set yet, the gateway hasn't restarted since, or the URL doesn't point at a hostname actually proxying to this server |
| `/wms` doesn't show up in Discord at all | Give it up to an hour the first time; otherwise double-check Part 2 actually returned the command back in its JSON reply |
| Locked out (`429`-equivalent reply) | Same five-wrong-PINs, five-minute lock as the API — see the API guide's Part 4 |
