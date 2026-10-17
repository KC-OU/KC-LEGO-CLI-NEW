# Solo mode

A personal, standalone copy of the gateway for a machine that has no access to the live ModernWMS/
Part-DB docker containers — a second Arch box at home, say, rather than the server those containers
actually run on. It is not a lighter version of [the test instance](test-instance.md); it is for a
genuinely different situation:

| | Test instance | Solo mode |
|---|---|---|
| Where it runs | The same box as live, or one with access to the same containers | Any machine, no containers needed at all |
| Sign-in | Your real ModernWMS/Part-DB account | A local password, set just for this copy |
| Reachable from | `127.0.0.1` only (SSH tunnel to reach it from elsewhere) | Its own IP and port, like live |
| LEGO collection | Fresh public catalog, empty collection | Same: fresh public catalog, empty collection |
| Part-DB parts / ModernWMS-only screens | Work normally (same containers) | Not available — see below |

If you *do* have access to the ModernWMS/Part-DB containers (you're setting up a second copy on the
same server, or one that can reach them), use [the test instance](test-instance.md) instead — it's
simpler and reuses your real login.

## What's different

ModernWMS is only ever reachable via `docker exec` into its container — there is no network path to
it. Part-DB's own reads are a local SQLite file; only its writes have a real HTTP API. Neither of
those is available on a machine with no access to the containers, so solo mode:

- Signs in against a password stored in `access.json` instead of either backend (`internal/access`'s
  `PasswordHash` field, bcrypt, same scheme as the remote-bot PIN). Set one with:
  ```
  wms access user set-password <name> --source local
  wms access user set-groups "local:<name>" admin
  ```
  (omit `--password` to get one generated and printed once). This account is governed by the same
  permission groups, 2FA policy, channels and persisted login lockout as any other — it just isn't
  ModernWMS or Part-DB underneath.
- Starts even when there's no Part-DB SQLite file to open, rather than aborting — every Part-DB
  screen/API call then returns a clear "Part-DB not available (solo mode)" error instead of working,
  so a Part-DB parts lookup fails cleanly rather than taking the whole app down.
- ModernWMS-only screens (warehouse ops, stock, orders, user management) behave the same as they
  would with an unreachable container today: a normal "docker exec failed" error, not a crash. The
  LEGO side of this app (sets, parts, checks, bagging — `internal/lego`, its own SQLite file) was
  always independent of both backends and works exactly as it does live.

## Setting it up

```bash
bash scripts/solo-instance.sh kc      # first run: builds, creates account "kc", seeds the public
                                       # catalog, starts the gateway
bash scripts/solo-instance.sh status
bash scripts/solo-instance.sh stop
```

No root, no systemd, no containers — it builds into and runs entirely out of
`~/.local/share/wms-solo` (override with `WMS_SOLO_DATA_DIR`), never touching `/usr/local/bin/wms-go`,
`/root/docker-server/wms`, or any systemd unit. Telnet defaults to port 2325 and the web terminal to
7696 (both overridable — `WMS_SOLO_TELNET_PORT`, `WMS_SOLO_WEB_PORT`), bound to `0.0.0.0` by default
(`WMS_SOLO_HOST`) rather than the test instance's hardcoded loopback — the point of this one is being
reachable from elsewhere on your own network.

The sign-on screen's message-of-the-day says `*** PERSONAL SOLO COPY ***`, so it's never mistaken for
the real one.
