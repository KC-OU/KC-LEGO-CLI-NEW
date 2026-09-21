# Settings

Settings come from the environment, or from the settings file (mode 0600) that the interface's *Admin → Settings & API Keys* screens write; the file wins.
Secrets are better entered in the interface. `config.example.env` in the repository lists the common ones for the systemd units.

## Places

| Setting | Default | Meaning |
|---------|---------|---------|
| `LEGO_DB_PATH` | `/root/docker-server/wms/lego.db` | your LEGO collection and the offline catalog |
| `PARTDB_DB_PATH` | `/root/docker-server/partdb/db/app.db` | Part-DB's SQLite file (read directly; writes go through its API) |
| `PARTDB_API_URL` | `http://127.0.0.1:8081` | Part-DB's address for the REST API |
| `MODERNWMS_CONTAINER` / `MODERNWMS_DB_PATH` | `modernwms` / `/app/wms.db` | how ModernWMS is reached (`docker exec`) |
| `WMS_SETTINGS_FILE` | `/root/docker-server/wms/settings.json` | saved settings and API keys |
| `TWOFA_FILE` | `/root/docker-server/wms/2fa.json` | 2FA secrets |
| `AUDIT_LOG_FILE` | `/root/tui_audit.log` | the hash-chained audit log |
| `MODERNWMS_BACKUP_DIR` | `/root/backups/modernwms` | where backups go |
| `WMS_IMAGE_DIR` | `/root/docker-server/wms/imgcache` | cached pictures |
| `WMS_PLUGIN_DIR` / `WMS_PLUGIN_USER` | `/root/docker-server/wms/plugins` / `nobody` | plugins |
| `PARTDB_URL`, `MODERNWMS_URL` | *(empty)* | your public addresses, used for links; omitted when empty |

## Keys and tokens

| Setting | Meaning |
|---------|---------|
| `PARTDB_API_TOKEN` | Part-DB API token (Edit level) |
| `REBRICKABLE_API_KEY` | optional; live lookups |
| `BRICKLINK_CONSUMER_KEY`, `_CONSUMER_SECRET`, `_TOKEN`, `_TOKEN_SECRET` | BrickLink API |
| `NOTIFY_URL`, `NOTIFY_FORMAT` | [alerts](../guides/alerts.md) |
| `WMS_BACKUP_PASSPHRASE` | for unattended encrypted backups |

## Behaviour

| Setting | Default | Meaning |
|---------|---------|---------|
| `MODERNWMS_TUI_THEME` | `green` | `green`, `amber`, `high-contrast`, `colorblind` |
| `NO_COLOR` | unset | any value turns colour off |
| `MODERNWMS_TUI_IMAGES` | `auto` | `auto`, `blocks`, `ascii`, `off` |
| `MODERNWMS_TUI_CLASSIC` | `1` | `0` uses the older fixed 80-column frame |
| `TUI_IDLE_LOCK_MINUTES` | `15` | `0` = never lock |
| `CATALOG_AUTO_REFRESH_HOURS` | `0` (off) | the gateway refreshes the offline catalog this often (never more than once a day) |
| `TWOFA_GRACE_MINUTES` | `30` | `0` = always ask for a code |
| `LISTEN_PORTS` / `GATEWAY_PORT` | `2323,23` / `7681` | telnet and web gateway |
| `WMS_EQUIVALENTS` | `default` | `default`, `none`, or `alt,mold,print` |
| `BRICKLINK_CURRENCY` / `BRICKLINK_REGION` / `BRICKLINK_CONDITION` | `GBP` / `europe` / `U` | price guide selection |
| `BRICKLINK_DAILY_BUDGET` | `4500` | hard cap on calls a day (max 5000) |
| `WMS_UPDATE_URL` | GitHub | a mirror for `wms update` |

## Exit codes

`0` ok, `1` failure, `2` usage (or a confirmation that needed `--yes`), `3` authentication or setup, `4` network or service, `5` not found.
