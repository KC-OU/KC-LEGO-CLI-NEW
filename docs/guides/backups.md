# Backups

## ModernWMS

```bash
wms-go backup                       # a consistent copy via SQLite's online backup
wms-go backup --encrypt             # sealed with a passphrase (age); the plaintext is deleted after a verified seal
wms-go backup decrypt file.db.age   # never overwrites
```

The passphrase comes from `--passphrase-file`, `WMS_BACKUP_PASSPHRASE` or a hidden prompt, **never from an argument** (history, `ps`).
The sealed file is standard [age](https://age-encryption.org): `age -d` opens it too. **Lose the passphrase and the backup is gone.**

## Your LEGO collection

```bash
wms-go lego backup [--encrypt]
```

Your sets, owned parts, history and price data, without the re-downloadable catalog, so it's small. Restore by copying the file back as `lego.db`, or use
[history and restore](history.md) for a point in time.

### Also keeping a copy on GitHub

```bash
wms-go lego backup --github
```

`--github` uploads the sealed backup as a release on a **separate, private** GitHub repo
(`WMS_BACKUP_GITHUB_REPO`, default `KC-OU/wms-backups`) — not the public `KC-LEGO-CLI-NEW` source repo this
project itself lives in, so a backup is never a public download even by mistake. One release per backup,
tagged by timestamp, uploaded with the same `gh` CLI [`publish.sh`](shipping.md) already needs (`gh auth
login` once). `--github` always implies `--encrypt`: a plaintext backup is never uploaded, so you'll be
prompted for a passphrase (or set `WMS_BACKUP_PASSPHRASE`/`--passphrase-file`) even if you didn't pass
`--encrypt` yourself. **Lose that passphrase and the GitHub copy is exactly as unrecoverable as a local
`--encrypt` backup is.**

Like [`publish.sh`](shipping.md) and `promote-dev.sh`, this only ever reaches GitHub when you run `--github`
yourself — nothing backs up there automatically. The repo is created (private) the first time, if it doesn't
exist yet. Restore the same way as any sealed backup: download the release asset, `wms backup decrypt`.

`lego.db` runs in SQLite's WAL mode, so a search or a page keeps working while the catalog refresh writes. That means recent changes can
sit in `lego.db-wal` beside the file: use `wms-go lego backup` (it is consistent), or copy `lego.db`, `lego.db-wal` and `lego.db-shm` together.
Do not copy `lego.db` alone.

## Checking

`wms-go doctor` fails if there is no backup or the newest is over a week old, and [alerts](alerts.md) can tell you. Schedule backups with cron or a systemd timer.
