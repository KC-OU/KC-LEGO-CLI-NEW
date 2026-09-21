# Telnet and the web terminal

`wms-go gateway serve` (the `wms-gateway` systemd unit) serves the interface to barcode scanners, old terminals and browsers:

- **Telnet** on the ports in `LISTEN_PORTS` (default 2323 and 23). It negotiates the window size, so the interface fills your window.
- **Web terminal** (ttyd behind a small proxy) on `GATEWAY_PORT` (default 7681), with tap-to-select for tablets.

--8<-- "docs/assets/screens/signon.html"

## Telnet is not encrypted

Passwords and 2FA codes cross the network readable by anyone on the path. The sign-on screen says so (quietly on a private network, as a
warning from a public address). Use telnet only on a network you trust, and the web terminal over HTTPS elsewhere. Remote sessions
**always require 2FA**.

## What protects the gateway

- At most **4 sessions per address**; a session that uses up its 5 sign-in attempts is a strike, and 3 strikes in an hour block that address
  for 15 minutes (doubling up to an hour). This machine's own address is never throttled.
- A session left at sign-on for **5 minutes is closed**; a signed-in session **locks after 15 idle minutes** (`TUI_IDLE_LOCK_MINUTES`).
- The web terminal can't see client addresses, so it isn't throttled per address.

## Two-factor authentication

```bash
wms-go users 2fa enable <user>       # shows a QR/secret and backup codes
wms-go users 2fa status <user>
wms-go users 2fa unlock <user>       # after a lockout
```

Codes are single-use (a code that just signed you in is refused if replayed; that's not counted as a guess). Five wrong codes lock the account for 5 minutes,
doubling each time up to an hour. Signing back in from the same address within `TWOFA_GRACE_MINUTES` (default 30) skips the prompt; the web terminal never gets that.
