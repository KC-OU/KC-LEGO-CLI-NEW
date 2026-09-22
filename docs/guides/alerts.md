# Alerts

For Discord, Slack, Telegram, Teams, email, WhatsApp and routing events to channels, see [Notifications](notifications.md);
this page covers what the gateway watches for.

Off unless you set `NOTIFY_URL`. Then the gateway watches for things worth a message and sends **at most one alert per kind per cooldown**.

```bash
export NOTIFY_URL=https://ntfy.sh/your-secret-topic     # or any webhook that takes JSON
wms-go notify test
```

For the gateway service, put it in `/etc/wms-go/wms-go.env` (see `config.example.env`). ntfy is used when the host contains "ntfy",
otherwise a JSON webhook (`{title, message, priority, text, content}`, readable by Slack and Discord); force it with `NOTIFY_FORMAT=ntfy|json`.

| Alert | When |
|-------|------|
| Failed sign-ins | 10 or more in 10 minutes |
| Audit log changed | the hash chain is broken (highest priority) |
| Backup stale | none, or the newest is over 7 days old |
| Low stock | parts below their minimum (daily) |
| Price drop | a [watch](bricklink.md#price-alerts) is at its limit (daily) |
| Audit head | the chain's newest hash, once a day: **your off-machine copy** |

Messages never contain passwords, tokens or 2FA codes. Connection errors don't repeat the URL (it may hold a secret topic).
