# Notifications

Alerts go to **Discord, Slack, Telegram, Microsoft Teams, Google Chat, email, Pushover, Gotify, custom webhooks** (which is
how WhatsApp works, through Twilio or CallMeBot) and **ntfy**.

## Channels

Channels are described in a [projectdiscovery/notify](https://github.com/projectdiscovery/notify) `provider-config.yaml` — the
same file notify's own CLI reads — at `WMS_NOTIFY_PROVIDERS` (default `/root/docker-server/wms/notify-provider-config.yaml`,
keep it `chmod 600`). Each entry's `id` is a channel:

```yaml
discord:
  - id: lego
    discord_webhook_url: https://discord.com/api/webhooks/…
    discord_username: KC-PARTS
slack:
  - id: ops
    slack_webhook_url: https://hooks.slack.com/services/…
telegram:
  - id: phone
    telegram_api_key: 123456:ABC…
    telegram_chat_id: "987654321"
smtp:
  - id: email
    smtp_server: smtp.example.com:587
    smtp_username: you@example.com
    smtp_password: an-app-password
    from_address: you@example.com
    smtp_cc: [you@example.com]
custom:
  - id: whatsapp            # CallMeBot: https://www.callmebot.com/blog/free-api-whatsapp-messages/
    custom_webhook_url: https://api.callmebot.com/whatsapp.php?phone=447700900123&apikey=123456&text={{data}}
    custom_method: GET
```

`{{data}}` is the message; `{{dataJsonString}}` is the message as a JSON string, for webhooks that take JSON. `NOTIFY_URL`
(an ntfy topic such as `https://ntfy.sh/your-secret-topic`, or a JSON webhook) adds the channel `ntfy`, and is what alerts
fall back to when there is no provider file.

wms sends these itself, using notify's file format and field names, rather than linking notify's Go packages: those pull in
archive libraries with vulnerabilities that have no fixed release, which a gateway on the internet should not carry.

## Events and routing

| Event | When |
|-------|------|
| `security` | a burst of failed sign-ins, a lockout, the audit log changed |
| `set_incomplete` / `set_complete` | a parts check finds parts missing / the last missing part arrives |
| `order_shipped` / `order_received` | an order moves along |
| `low_stock`, `price_drop`, `backup`, `export_done` | as before |

Every event goes to every channel until you route it:

```bash
wms-go notify channels                       # channels, and where each event goes
wms-go notify route security email,discord
wms-go notify route set_complete whatsapp
wms-go notify test                           # a test to every channel, with each result
```

In the terminal: *Admin → Access Control → 5 Notifications* — **Enter** changes an event's channels, **T** tests them all.
