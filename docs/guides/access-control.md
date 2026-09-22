# Access control

Who may do what in the terminal (telnet, web terminal, local), who must use 2FA, and how long sessions and exports last.
Passwords are still checked by ModernWMS and Part-DB; this decides what a signed-in user may do.

## The model

- A **permission** is `area.action`, e.g. `lego.export` or `stock.adjust`. `wms access perms` lists them all:

  | Area | Actions |
  |------|---------|
  | `lego` | view, search, edit, delete, export |
  | `partdb` | view, edit, delete |
  | `stock` | view, check, adjust |
  | `ops` | view, asn, edit (ASN, inventory, outbound, master data) |
  | `sets` | check, stocktake (parts checks and recounts) |
  | `orders` | view, manage (orders for missing parts) |
  | `labels` | print |
  | `exports` | create, download (the QR download link) |
  | `bricklink` | view, price (live BrickLink calls) |
  | `scripts` | run |
  | `containers` | view, manage |
  | `users` | view, manage |
  | `access` | manage (this system) |
  | `settings` | view, edit |
  | `audit` | view |

- A **group** allows or denies permissions. A **user** belongs to groups and can override single permissions.
  **Deny always wins**; anything not set is not granted.
- A user **with no entry** keeps their ModernWMS role's access, exactly as before. Nothing changes until you add someone.
- What you can't use is **hidden**. A key pressed anyway says `ACCESS DENIED — needs the lego.edit permission (not granted)` and is written
  to the audit log (`DENIED_PERMISSION`).

## Starter groups

| Group | For | Usable without 2FA |
|-------|-----|:---:|
| `admin` | everything | no |
| `operator` | day-to-day stock, parts and LEGO work | no |
| `viewer` | read-only everywhere | yes |
| `builder` | LEGO only: browse, missing parts, what to build | yes |
| `exporter` | LEGO view plus exports and downloads | yes |
| `stock-clerk` | stock checks, Part-DB view, LEGO part/set search and export | yes |

Edit any of them (or make your own) in *Admin → Access Control → Groups*, or with `wms access groups set`.

## 2FA, sign-in limits and timeouts

Per user:

- **2FA**: `default` (required over telnet and the web terminal, as before), `required` (also on the local console), or `exempt`.
- An **exempt** account may only be in *restricted* groups, which can never hold `users.manage`, `access.manage`, `settings.edit`,
  `scripts.run` or `containers.manage`; wms refuses the change otherwise. Limit it further to **networks**
  (`--cidr 192.168.1.0/24`): from anywhere else 2FA is asked. The web terminal cannot see the client's address, so a network-limited
  exemption only applies over telnet.
- **Channels**: which ways in are allowed (`telnet`, `web`, `local`).
- **Expiry**: access ends at the start of that day.
- **Timeouts**: the 2FA remember window, idle lock and maximum session length can be set per user; otherwise the global values apply.

Global values (*Admin → Access Control → Security settings*, or `wms access settings`):

| Setting | Default | Meaning |
|---------|---------|---------|
| 2FA remember window | 30 min | the same address signs in again without a code |
| Idle lock | 15 min | 0 = never |
| Maximum session | 0 (none) | signed out after this many hours however active |
| Exports kept | 7 days | files in `WMS_EXPORT_DIR` older than this are deleted |
| Download links | 15 min | how long a QR download link works |

Every sign-in decision is audited: `LOGIN_2FA_EXEMPT`, `LOGIN_DENIED_EXPIRED`, `LOGIN_DENIED_CHANNEL`, `SESSION_MAX_AGE`.

## In the terminal

*Admin → 4 Access Control*:

1. **Groups**: ↑/↓ choose, **Enter** opens the permission grid (arrows move, **Space** cycles allow → deny → not set, **S** saves),
   **N** new, **C** copy, **R** restricted, **D** delete (refused while users are in it).
2. **Users**: **Enter** edits groups, 2FA, networks, channels, expiry, timeouts and a note; **G** edits that user's permission
   overrides; **N** adds a user; **X** removes the entry (back to their ModernWMS role).
3. **Security settings**: the global timings above.
4. **Check a user's effective permissions**: every permission with *why* ("allowed by group exporter", "denied by user override").

You can't save a change that removes your own `access.manage`. Every change is audited as `ACCESS_CHANGED` with what changed.

## Recipes

An export-only account that skips 2FA at home:

```bash
wms-go users create exportbot                       # a normal Part-DB/ModernWMS user
wms-go access user set-groups exportbot exporter --source partdb
wms-go access user 2fa exportbot exempt --cidr 192.168.1.0/24
wms-go access user channels exportbot telnet
wms-go access check exportbot lego.export            # ✓ allowed by group exporter
```

A LEGO-only account for a child, weekends only by expiry:

```bash
wms-go access user set-groups kid builder --source partdb
wms-go access user expires kid 2026-12-31
wms-go access user timeouts kid --idle 10 --max-hours 2
```

Tighten everyone:

```bash
wms-go access settings --idle-minutes 10 --max-session-hours 8 --grace-minutes 15 --export-days 3 --link-minutes 10
```

Back up or move the policy: `wms-go access export > policy.json`, `wms-go access import policy.json`. The file is
`/root/docker-server/wms/access.json` (`WMS_ACCESS_FILE`), readable by root only.
