# Telnet and the web terminal

`wms-go gateway serve` (the `wms-gateway` systemd unit) serves the interface to barcode scanners, old terminals and browsers:

- **Telnet** on the ports in `LISTEN_PORTS` (default 2323 and 23). It negotiates the window size, so the interface fills your window.
- **Web terminal** (ttyd behind a small proxy) on `GATEWAY_PORT` (default 7681), with tap-to-select for tablets.

--8<-- "docs/assets/screens/signon.html"

## Telnet is not encrypted

Passwords and 2FA codes cross the network readable by anyone on the path. The sign-on screen says so (quietly on a private network, as a
warning from a public address). Use telnet only on a network you trust, and the web terminal over HTTPS elsewhere. Remote sessions
**require 2FA**, unless an admin has exempted an account (limited groups, networks, channels and expiry — see
[Access control](access-control.md)).

## The sign-on screen

Both the telnet and the web terminal open on the **control room**: the KC-PARTS logo, live **system** status (Part-DB,
ModernWMS, the sync service and the catalog's age — checked in the background, so a slow container never delays the prompt),
the **collection** (sets, pieces, what is missing and on order), **alerts**, the admin's **message of the day** and a clock.
The collection and alerts are visible **before** anyone signs in; *Admin → Access Control → Security settings* (or
`WMS_SIGNON_PUBLIC_STATS=0`) hides them. `wms motd "Stock check Saturday 10:00"` sets the message.

After signing in you see when and where you last signed in, and any **failed attempts** on your name since. The Overview
(hub **1**) is the same control room with spend, the Set of the Day, a growth chart and recent activity; **K** shows the old
ModernWMS / Part-DB KPI table.

## What protects the gateway

- At most **4 sessions per address**; a session that uses up its 5 sign-in attempts is a strike, and 3 strikes in an hour block that address
  for 15 minutes (doubling up to an hour). This machine's own address is never throttled.
- A session left at sign-on for **5 minutes is closed**; a signed-in session **locks after 15 idle minutes** by default, and
  can be given a maximum length; both are set globally or per user in *Admin → Access Control → Security settings*.
- The web terminal can't see client addresses, so it isn't throttled per address.

## Two-factor authentication

```bash
wms-go users 2fa enable <user>       # shows a QR/secret and backup codes
wms-go users 2fa status <user>
wms-go users 2fa unlock <user>       # after a lockout
```

Codes are single-use (a code that just signed you in is refused if replayed; that's not counted as a guess). Five wrong codes lock the account for 5 minutes,
doubling each time up to an hour. Signing back in from the same address within `TWOFA_GRACE_MINUTES` (default 30) skips the prompt on telnet; the web
terminal doesn't get an address-based grace window (see below for why), but it doesn't need one either — a session it already verified stays signed in.

## The web terminal is one persistent, shared session

Every telnet connection is its own process, with its own address, so a fresh connection can be trusted with the grace window above. A browser tab
talking to ttyd has no such address to check — nothing about the connection proves "this is the same person who already passed 2FA" — so the web
terminal deliberately never grants the grace window (an empty/unknown origin is refused, on purpose).

What it does instead: `wms-gateway` runs the web terminal's `wms tui` process inside a `tmux` session (`wms-web`) that outlives any one
connection. Opening a new tab, the PWA resuming after being backgrounded, or a dropped mobile connection all **attach to that same running
process** — wherever you left it, already signed in — instead of spawning a fresh one that has to ask again. Signing out (**Q** at the hub) still
returns that process to the sign-on screen, so the next person to open the web terminal gets a real prompt.

The trade-off: it's **one shared screen**, not one per device. Every tab or device that opens the web terminal is looking at (and driving) the
same session — the right model for one person using several devices, not for several people sharing a login. `wms doctor` reports whether `tmux`
is installed; without it, the web terminal falls back to today's per-connection behaviour (a fresh sign-in and 2FA prompt every time).
