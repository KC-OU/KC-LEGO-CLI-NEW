# Discord notifications for exports

Any export (a report, a stock sheet, a shopping list) can also be DMed straight to Discord — a real bot message with
the QR code as an actual image attachment, not just a link, so you can scan it off your phone the moment it arrives.
This is separate from the [existing notify system](notifications.md) (ntfy/Slack/Discord webhooks for low-stock,
price-drop and other alerts): those post into a fixed channel and can't DM a person or attach a file, which is
exactly what this needed.

## Set it up (once)

1. Go to [discord.com/developers/applications](https://discord.com/developers/applications) and create a new
   application.
2. Under **Bot**, add a bot and copy its **token**.
3. Invite the bot to any one server you're also in (Discord only lets a bot open a DM with someone it shares a
   server with — it never needs to post there, just share it).
4. In Discord, turn on **Developer Mode** (User Settings → Advanced), then right-click your own name anywhere and
   **Copy User ID**.
5. Set both values (Admin → Settings, or directly):

```bash
WMS_DISCORD_BOT_TOKEN=<the bot token>
WMS_DISCORD_BOT_USER_ID=<your Discord user ID>
```

`wms doctor` reports whether it's configured. It also needs `WMS_PUBLIC_URL` set (the same requirement as the
existing QR/download-link feature) — without it there's no reachable link to send.

## Use it

**CLI**: add `--discord` to any export command, with `--discord-expires` (default 2h) for how long that particular
link should stay valid — its own expiry, independent of the site-wide download-link default, for exactly the
"might be busy, don't make me redo this later" case.

```bash
wms-go lego report missing -o missing.html --discord --discord-expires 8h
wms-go lego stocksheet 75192 -o falcon.html --discord
wms-go lego export --format xlsx -o inventory.xlsx --discord
```

**TUI**: after any export completes (the same **X** → format screen as everywhere else), if Discord is configured
you'll see **D  send this to Discord** — press it, then pick an expiry (30 minutes, 2 hours, 8 hours, or 24 hours).

The message that arrives says who sent it and what it is (e.g. *"kieran — missing parts for 75192, expires
14:30"*), the QR code as an image, and the link as text — so whoever opens Discord knows what it's for and how long
they have, without needing to ask.
