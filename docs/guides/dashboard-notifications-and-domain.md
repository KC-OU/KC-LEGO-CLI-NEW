# Dashboard notifications, email, and your own domain

A from-scratch walkthrough for three things that came up going into production: Discord alerts for the new web
dashboard, where notification/onboarding email actually comes from, and putting the whole project (CLI, telnet,
mobile app, dashboard) on its own domain instead of `your-domain.example`.

## 1. Discord notifications for the dashboard

**There's nothing dashboard-specific to build.** `internal/dashboardapi`'s admin actions (message a user, force log
off a CLI/TUI session, file an accuracy report) already fire the exact same events (`message`, `forced_off`,
`accuracy_report_filed`) through the same [notify system](notifications.md) every other alert in this project uses
— see that doc for the full channel list (Slack, Telegram, Teams, email, ntfy, custom webhooks). Discord specifically:

1. In Discord: **Server Settings → Integrations → Webhooks → New Webhook**. Pick the channel you want dashboard
   alerts in, name it (e.g. "KC-Parts"), **Copy Webhook URL**.
2. Edit (or create) the file at `WMS_NOTIFY_PROVIDERS` (default `/root/docker-server/wms/notify-provider-config.yaml`
   — `chmod 600` it, the webhook URL is a bearer credential):
   ```yaml
   discord:
     - id: dashboard
       discord_webhook_url: https://discord.com/api/webhooks/XXXXXXXX/YYYYYYYYYYYYYYYYYYYYYYYY
       discord_username: KC-PARTS
   ```
3. Confirm it's picked up and test it:
   ```bash
   wms-go notify channels      # should list "dashboard" under discord
   wms-go notify test          # sends a test message to every channel
   ```
4. Optional — route only the dashboard's own events there instead of everything:
   ```bash
   wms-go notify route message discord
   wms-go notify route forced_off discord
   wms-go notify route accuracy_report_filed discord
   ```

That's the whole setup. Anything that calls `notify.Dispatch` anywhere in the codebase — the Discord bot, the TUI,
the dashboard — reaches this same webhook from this point on.

## 2. Email — do you need to host your own mail server?

**No.** Running your own mail server (Postfix/Exim) is the wrong tool here regardless of domain: it needs SPF/DKIM/
DMARC set up correctly, IP reputation warmed up over weeks, and ongoing fighting to stay off blacklists — all real
risk for zero benefit at this scale. Split the problem in two:

### Sending (what this project actually needs today)

The existing `smtp:` provider in `provider-config.yaml` (see [Notifications](notifications.md)) is plain SMTP —
point it at any provider's relay and it works with no code changes. For a new domain, pick a transactional-email
provider and verify the domain with it (adds a couple of DNS records — trivial in Cloudflare, see below):

- **[Resend](https://resend.com)** — simplest setup, generous free tier (3,000 emails/month), modern SMTP+API.
- **[Brevo](https://brevo.com)** (formerly Sendinblue) — free tier (300/day), SMTP relay, slightly more enterprise-y UI.
- Your **existing Google Workspace/personal email account** also works as a plain SMTP relay (port 587, an app
  password) if you don't want a new account anywhere — fine for low volume, but it'll send *as* your existing
  address, not `noreply@yournewdomain`, unless that's a Google Workspace account for the new domain specifically.

Either way, this plugs straight into the existing config:
```yaml
smtp:
  - id: email
    smtp_server: smtp.resend.com:587   # or whichever provider
    smtp_username: resend
    smtp_password: <api key / app password>
    from_address: noreply@yournewdomain.tld
    smtp_cc: [you@yournewdomain.tld]
```

One real gap worth knowing about before Phase 4 (the New Starter Guide PDF emailed with the temp password):
`internal/notify`'s `sendMail` only sends plain text/HTML, no attachments. Attaching a PDF needs a small addition
to that function (MIME multipart) when Phase 4 gets there — not needed for today's blocking items.

### Receiving (a human inbox at the new domain, if you want one)

Two no-hosting options, in order of effort:

1. **Cloudflare Email Routing** (free) — forwards `anything@yournewdomain.tld` to your existing inbox (Gmail,
   `your-domain.example`, whatever). Not a real mailbox, just forwarding — but it's zero setup beyond a couple of DNS
   records Cloudflare adds for you, and it's enough if you just want `admin@yournewdomain.tld` to land somewhere you
   already read.
2. **A real mailbox at the new domain** — [Zoho Mail](https://zoho.com/mail) has a genuinely free tier (5 users,
   custom domain) if you want to actually log into `you@yournewdomain.tld` as its own inbox. Google Workspace
   (~£5/user/month) is the heavier, more polished option if this becomes a real business tool people rely on daily.

Hostinger also sells its own email hosting (Titan Mail) alongside the domain — fine if you'd rather keep
registrar+email+DNS all in one dashboard, but Cloudflare Email Routing or Zoho cost nothing and don't lock you to
Hostinger for anything but the domain registration itself.

## 3. A custom domain, managed through Cloudflare + Hostinger

### Buy it at Hostinger, run DNS at Cloudflare

1. Register the domain at Hostinger as usual.
2. In Cloudflare: **Add a site**, paste the domain, let it scan existing DNS records.
3. Cloudflare gives you two nameservers (e.g. `xyz.ns.cloudflare.com`). In Hostinger's panel: **Domains → \<your
   domain\> → DNS/Nameservers → Change nameservers** → paste Cloudflare's two.
4. Wait for propagation (Cloudflare's dashboard shows "Active" once it's done, usually under an hour). From here on,
   every DNS record, the free TLS, and Email Routing all live in Cloudflare — Hostinger is now just the registrar of
   record, nothing else touches it.

### Recommended subdomain layout — home server, dynamic IP

This box's own IP changes (same situation as wherever `docker-server`/`your-domain.example` already runs), so plain
A records pointing at it would go stale. Split by protocol instead:

| Subdomain | Points at | How |
|---|---|---|
| `dashboard.kcparts.xyz` | Caddy, which internally splits `/api/auth/*` → `kc-parts-auth-node` (3001) and everything else → `kc-parts-dashboard-rs` (3000) | **Cloudflare Tunnel** public hostname |
| `term.kcparts.xyz` | the existing ttyd web terminal (port 7681) | **Cloudflare Tunnel** public hostname |
| `cli.kcparts.xyz` | the telnet gateway (ports 2323/23, raw TCP) | plain **DNS-only A record**, kept current by a small dynamic-DNS script (below) |

There's no separate `auth.kcparts.xyz` hostname — `kc-parts-auth-node` is reached at
`dashboard.kcparts.xyz/api/auth/*`, same origin as the dashboard itself, which is exactly what makes the
cross-origin-cookie problem `kc-parts-dashboard-rs`'s own README flags go away. Any OAuth callback URL you
register with a provider (next section) is therefore under `dashboard.kcparts.xyz`, never `auth.kcparts.xyz`.

**Cloudflare Tunnel** (`cloudflared`) is the fit for a dynamic IP: it's an outbound-only connection from this box
to Cloudflare, so there's no port to forward and nothing to update when the home IP changes — Cloudflare's edge
also terminates TLS, so Caddy doesn't need to request its own certificate for this hop.

1. Cloudflare dashboard → **Zero Trust → Networks → Tunnels → Create a tunnel** → Cloudflared → name it (e.g.
   `kcparts-home`). It gives you an install-and-run command detected for this box's OS — follow it; it installs
   `cloudflared` as a background service on its own.
2. Still on that tunnel's page, **Public Hostname** tab → add two entries:
   - `dashboard.kcparts.xyz` → HTTP → `localhost:8080` (Caddy, below — not 3000/3001 directly)
   - `term.kcparts.xyz` → HTTP → `localhost:7681`
3. Caddy now only has to split traffic locally — no public hostname, no ACME, just plain HTTP on one local port:
   ```caddyfile
   http://localhost:8080 {
   	handle /api/auth/* {
   		reverse_proxy 127.0.0.1:3001
   	}
   	handle {
   		reverse_proxy 127.0.0.1:3000
   	}
   }
   ```
4. Set `kc-parts-auth-node`'s `.env`: `BETTER_AUTH_URL=https://dashboard.kcparts.xyz` and
   `DASHBOARD_ORIGIN=https://dashboard.kcparts.xyz`. Point `kc-parts-dashboard-rs`'s `BETTER_AUTH_URL` at the
   same address.

**`cli.kcparts.xyz` (raw telnet) can't go through the tunnel** — Cloudflare's free tier only proxies HTTP(S)/SSH/
RDP to anonymous internet clients, not arbitrary raw TCP, and a plain telnet client can't speak the Zero Trust
client protocol a TCP tunnel would otherwise require. So this one stays a plain DNS-only (grey cloud) A record,
kept pointed at the current home IP by a small cron script instead of by hand:

```bash
#!/bin/bash
# ponytail: blind poll-and-patch every 5 min, fine for one record at this traffic level —
# switch to a webhook-based updater only if 5 minutes of staleness after an IP change ever matters.
set -eu
ZONE_ID="…"     # kcparts.xyz's zone, from the Cloudflare dashboard's Overview page (right sidebar)
RECORD_ID="…"   # the cli.kcparts.xyz A record's id — list it once: GET /zones/$ZONE_ID/dns_records?name=cli.kcparts.xyz
API_TOKEN="…"   # Cloudflare → My Profile → API Tokens → Create Token → Zone:DNS:Edit, scoped to this zone only

IP=$(curl -s https://api.ipify.org)
curl -s -X PATCH "https://api.cloudflare.com/client/v4/zones/$ZONE_ID/dns_records/$RECORD_ID" \
  -H "Authorization: Bearer $API_TOKEN" -H "Content-Type: application/json" \
  --data "{\"type\":\"A\",\"name\":\"cli.kcparts.xyz\",\"content\":\"$IP\",\"proxied\":false}"
```

`crontab -e`: `*/5 * * * * /path/to/ddns.sh` — the server's own firewall/fail2ban keeps doing the actual
protecting on that port, same as a static IP; this script only keeps the DNS name pointed at the right place.

### Domain name — `kcparts.xyz`

Avoid putting "lego" itself in the domain — LEGO Group is known for aggressively policing trademark use in
commercial domain names, and this is a resale/checking business, not a fan project. "KC-Parts" is already this
project's own brand everywhere else (`modernwms-partdb-go`'s own docs, the GitHub org), and **`kcparts.xyz` is the
chosen domain** — short, no trademark exposure, cheap at Hostinger. `.xyz` carries no forced-HTTPS quirk (that
was only ever a `.app`-specific consideration, and a non-issue either way since every browser-facing piece is
already behind TLS via Cloudflare/Caddy regardless of TLD).

The subdomain table and Caddyfile above apply directly with this domain: `dashboard.kcparts.xyz`,
`auth.kcparts.xyz`, `term.kcparts.xyz` (all proxied), `cli.kcparts.xyz` (DNS-only, for the raw telnet port) — use
these in place of every `yourdomain.tld` placeholder above, including `kc-parts-auth-node`'s `BETTER_AUTH_URL`/
`DASHBOARD_ORIGIN` and `kc-parts-dashboard-rs`'s `BETTER_AUTH_URL`.

Alternatives, if `kcparts.xyz` turns out to already be taken at registration time: `kcparts.app`, `kcparts.io`,
`kcpartswms.com`.
