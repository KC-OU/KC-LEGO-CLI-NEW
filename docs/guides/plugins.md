# Plugins

Write your own commands and reactions in any language. A plugin is an executable named `wms-<name>` in the plugin folder
(`WMS_PLUGIN_DIR`, default `/root/docker-server/wms/plugins`).

!!! danger "A plugin runs code on your machine"
    Read a plugin before you enable it. The controls below limit the damage, they don't make an untrusted program safe.

## The lifecycle

```bash
wms-go plugin new hello                 # creates an example (not enabled)
$EDITOR /root/docker-server/wms/plugins/wms-hello
wms-go plugin enable hello --hooks part_added,low_stock     # shows the checksum, asks you to confirm
wms-go hello some arguments             # runs it: same as `wms-go plugin run hello ...`
wms-go plugin list
wms-go plugin disable hello
```

Built-in commands always win over a plugin of the same name.

## What protects you

| Control | How |
|---------|-----|
| **Only from the plugin folder** | Never from `$PATH`, so a writable directory can't hijack a command. The folder and each file must be owned by root (or you), not writable by others, regular files (no links). |
| **Nothing runs until enabled** | Enabling **pins the SHA-256**. If the file changes, it **refuses to run** until you review it and enable it again. |
| **Scrubbed environment** | No API keys, tokens or settings; a neutral working directory. It gets `WMS_PLUGIN_NAME` (and `WMS_PLUGIN_EVENT` for hooks). |
| **Unprivileged** | When wms runs as root, plugins run as `nobody` (`WMS_PLUGIN_USER`). `--as-user root` is an explicit, loudly-warned opt-in. |
| **Time limit** | Hooks are killed (the whole process group) after 10 seconds; output is capped at 64 KB. |
| **Audited** | Enabling, refusing, running and every hook call go to the audit log. |

`wms-go doctor` flags a plugin whose file has changed.

!!! note "The plugin folder and its parents must be searchable by the plugin user"
    If a plugin can't start as `nobody`, the error says so: `chmod o+x` the directories and make the file `o+rx`.

## Hooks

An enabled plugin is called as `wms-<name> hook <event>` with the event as one JSON line on **stdin**:

```json
{"event":"part_added","at":"2026-09-20T12:00:00Z","data":{"part":"3001","name":"Brick 2 x 4","colour":"Red","qty":25,"part_db_id":17}}
```

| Event | When | `data` |
|-------|------|--------|
| `part_added` / `part_updated` | a part is saved in the interface or by `add-part` | part, name, colour, qty, was, part_db_id |
| `low_stock` | the alert monitor finds parts below their minimum (daily) | `lines`: one text line per part |
| `price_drop` | a watch is at or under its limit | `lines` |
| `backup_done` | `wms backup` or `wms lego backup` finishes | kind, file, bytes, encrypted |
| `catalog_refreshed` | the offline catalog changed | files_updated, rows |

A slow or failing plugin never blocks the others or the action that triggered it.

## A minimal plugin

```bash
#!/bin/bash
set -euo pipefail
if [[ "${1:-}" == "hook" ]]; then
    payload="$(cat)"
    echo "got $2: $payload" >> /tmp/wms-plugin.log      # the plugin user must be able to write here
    exit 0
fi
echo "hello, arguments: $*"
```

Because plugins run unprivileged with a scrubbed environment they can't read wms's data. They work from the event JSON, and (with
`--as-user`) from what that user is allowed to run.
