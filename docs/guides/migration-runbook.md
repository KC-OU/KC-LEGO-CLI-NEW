# Moving to a new server (Arch → NixOS) and restoring from backup

This is the one-page plan for resetting the VPS or recreating it elsewhere. The rule: **do not reset the old
server until a restore has been proven on the new one.**

## 0. What protects you already

- Nightly at 03:00 the server writes an age-encrypted WMS database backup and an encrypted LEGO collection
  backup, and uploads the collection to the private GitHub repo `KC-OU/wms-backups`. The passphrase is in
  `/root/.config/wms-go/backup.key` — **keep a copy in your password manager; without it the backups are useless.**
- `scripts/migrate-export.sh` makes one encrypted archive of everything else (below).

## 1. Make the export (on the old server)

```
scripts/migrate-export.sh                 # ~3 minutes, writes /root/migration/kc-export-<date>.tar.zst.enc (~2 GB)
scripts/migrate-export.sh verify /root/migration/kc-export-<date>.tar.zst.enc   # must print "every file matches"
```

It holds: every git repo as a bundle (all branches) **plus** `*.patch` / `*.untracked.tgz` for work that was never
committed (check `NOTES.txt` inside: it names every repo that had uncommitted work — commit it first if you can),
`docker-server/` (compose files and bind-mounted data, including Part-DB's SQLite copied safely), `docker/*.inspect.json` (the exact recipe of every container, including ones started by hand such as `modernwms` and `cloudflared-kcparts`) and the Teedy folder, named Docker
volumes under 1.5 GB (larger ones are listed as SKIPPED), a Kan.bn Postgres dump, systemd units, helper scripts,
SSH host keys, `~/.config/wms-go` (all API tokens), Cloudflare credentials, the package list and running containers.

## 2. Copy it off the box

`scp /root/migration/kc-export-*.enc you@elsewhere:` — and check the size matches. The file is encrypted with the
same passphrase as the backups. Nothing in this runbook copies it for you.

## 3. Build the new server

Install NixOS, then adapt `docs/nixos/configuration.nix` (it is a **draft**: untested, with TODOs marked). Get SSH
working first and keep the provider's console open until you have logged in over SSH.

## 4. Restore (on the new server)

```
openssl enc -d -aes-256-cbc -pbkdf2 -iter 200000 -pass file:backup.key < kc-export-*.enc | zstd -d | tar -x -C /root/restore
cd /root/restore/kc-export && sha256sum -c MANIFEST.sha256 --quiet
```

## 5. Put things back, in this order

1. SSH host keys: `tar -xzf config/ssh-host-keys.tgz -C /etc/ssh` then restart sshd (clients keep trusting the server).
2. `config/root-config.tgz` → `/root` (wms-go tokens, Cloudflare auth, gh login).
3. Repos: `git clone git/<name>.bundle /root/<name>`; apply `git/<name>.patch` and unpack `git/<name>.untracked.tgz` in each.
4. `docker-server.tgz` → `/root`; named volumes: `volumes/<v>.tgz` → `/var/lib/docker/volumes/<v>/_data` (create with `docker volume create`).
   Kan.bn: start only its Postgres, then `psql -U kanbn < db/kanbn-postgres.sql`.
5. `systemd/*` units and `bin/*` helpers → their places, then `systemctl daemon-reload`.
6. Cloudflare tunnel: the `cloudflared` container (host network) comes up with the restored compose + token. **Switch DNS
   only when the new box answers correctly on the tunnel**; the old box keeps serving until then.
7. Build and install the binaries: `cd /root/modernwms-partdb-go && scripts/deploy.sh`; `cd /root/kc-parts-dashboard-rs && cargo build --release`.
8. Install the firewall (`wms-firewall.sh` or the NixOS rules) **before** exposing anything, then CrowdSec.

## 6. Prove it

`wms doctor` all green; sign in to the dashboard; open a set and a Part-DB page; pull a nightly backup from GitHub and
`wms backup decrypt` it. Only then reset or cancel the old server.

## If the server just died (no export)

The nightly GitHub backups plus the git remotes recover the data and code; you will have to re-enter API tokens
and Cloudflare credentials, which is why the export (which contains them) matters.
