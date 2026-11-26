#!/usr/bin/env bash
# Everything needed to rebuild this server on another machine (e.g. a fresh NixOS VPS), in ONE encrypted file:
# every git repo as a bundle (all branches) plus any uncommitted/untracked work, docker-server (compose files and
# bind-mounted data), named docker volumes under a size cap, a Postgres dump for Kan.bn, systemd units, helper
# scripts, SSH host keys, wms config/secrets, Cloudflare auth, and a manifest of installed packages/containers.
# Not included (rebuildable or already off-box): build output (target/, node_modules), the 12 GB local backup
# archive (the nightly job already puts encrypted copies on GitHub), Claude transcripts.
#
#   migrate-export.sh            build /root/migration/kc-export-<date>.tar.zst.enc  (passphrase: backup.key)
#   migrate-export.sh verify F   decrypt F into a scratch dir, check every checksum, list the contents
#
# The output holds secrets (API tokens, SSH host keys): it is mode 600 and encrypted, but you must copy it off this
# machine yourself (scp/rsync) before you reset anything.
set -euo pipefail
KEY=${WMS_MIGRATE_KEY:-/root/.config/wms-go/backup.key}
OUT=${WMS_MIGRATE_OUT:-/root/migration}
VOL_CAP_MB=${WMS_MIGRATE_VOL_CAP_MB:-1500}
umask 077

enc()  { openssl enc -aes-256-cbc -pbkdf2 -iter 200000 -salt -pass "file:$KEY"; }
dec()  { openssl enc -d -aes-256-cbc -pbkdf2 -iter 200000 -pass "file:$KEY"; }

if [ "${1:-}" = verify ]; then
  f=${2:?usage: migrate-export.sh verify <file.enc>}
  d=$(mktemp -d /tmp/migrate-verify.XXXXXX); trap 'rm -rf "$d"' EXIT
  dec < "$f" | zstd -d | tar -x -C "$d"
  ( cd "$d"/* && sha256sum -c MANIFEST.sha256 --quiet ) && echo "verify: every file matches its checksum"
  find "$d" -maxdepth 3 | sed "s#$d/##" | head -60
  exit 0
fi

[ -r "$KEY" ] || { echo "no passphrase file at $KEY" >&2; exit 1; }
mkdir -p "$OUT"; chmod 700 "$OUT"
stamp=$(date +%Y%m%d-%H%M%S)
S="$OUT/stage-$stamp/kc-export"; mkdir -p "$S"/{git,volumes,systemd,bin,config,db}
trap 'rm -rf "$OUT/stage-$stamp"' EXIT
: > "$S/NOTES.txt"
note() { echo "$*" | tee -a "$S/NOTES.txt" >&2; }

# 1. git: bundle every branch, and keep work that bundles can't hold
while IFS= read -r g; do
  r=$(dirname "$g"); n=$(basename "$r")
  git -C "$r" bundle create "$S/git/$n.bundle" --all >/dev/null 2>&1 || note "WARN: could not bundle $n"
  if [ -n "$(git -C "$r" status --porcelain 2>/dev/null)" ]; then
    note "UNCOMMITTED work in $n (saved as git/$n.patch and git/$n.untracked.tgz)"
    git -C "$r" diff HEAD > "$S/git/$n.patch" 2>/dev/null || true
    git -C "$r" ls-files --others --exclude-standard -z | tar --null -czf "$S/git/$n.untracked.tgz" -C "$r" -T - 2>/dev/null || true
  fi
  # git-ignored files that matter (the Rust dashboard's .env, local configs) are NOT in bundles or untracked.tgz;
  # keep the small ones (< 1 MB), never build output or dependencies.
  git -C "$r" ls-files -oi --exclude-standard -z 2>/dev/null | while IFS= read -r -d '' f; do
    case "$f" in target/*|node_modules/*|*/target/*|*/node_modules/*) continue ;; esac
    if [ -f "$r/$f" ] && [ "$(stat -c %s "$r/$f")" -lt 1048576 ]; then printf '%s\0' "$f"; fi
  done | tar --null -czf "$S/git/$n.ignored-small-files.tgz" -C "$r" -T - 2>/dev/null || note "WARN: ignored files of $n"
  git -C "$r" remote -v > "$S/git/$n.remotes.txt" 2>/dev/null || true
done < <(find /root -maxdepth 2 -name .git -type d 2>/dev/null)

# 2. docker-server (compose files + bind-mounted data). SQLite files are copied consistently via the backup API.
tar --exclude='node_modules' --exclude='*.log' -czf "$S/docker-server.tgz" -C /root docker-server
python3 - "$S/db" <<'PY'
import sqlite3, sys, os
out = sys.argv[1]
for src in ("/root/docker-server/partdb/db/app.db",):
    if os.path.exists(src):
        d = sqlite3.connect(os.path.join(out, os.path.basename(os.path.dirname(os.path.dirname(src))) + "-" + os.path.basename(src)))
        sqlite3.connect(src).backup(d); d.close()
PY
# Kan.bn's Postgres: a logical dump, never a copy of live data files
if docker ps --format '{{.Names}}' | grep -qx kanbn-postgres; then
  docker exec kanbn-postgres pg_dumpall -U kanbn > "$S/db/kanbn-postgres.sql" 2>/dev/null || note "WARN: kanbn pg_dumpall failed"
fi

# 2b. containers started by hand (docker run) or from other folders are not in docker-server/: keep the exact recipe
# (docker inspect holds image, env, ports, mounts, network and restart policy -- it contains secrets, so it only
# lives inside this encrypted archive), plus the Teedy compose folder.
mkdir -p "$S/docker"
for c in $(docker ps -aq); do docker inspect "$c" > "$S/docker/$(docker inspect "$c" --format '{{.Name}}' | tr -d /).inspect.json"; done
[ -d /root/teedy ] && tar -czf "$S/docker/teedy-folder.tgz" -C /root teedy

# 3. named docker volumes under the size cap
for v in $(docker volume ls -q); do
  p=/var/lib/docker/volumes/$v/_data; [ -d "$p" ] || continue
  mb=$(du -sm "$p" 2>/dev/null | cut -f1)
  if [ "${mb:-0}" -gt "$VOL_CAP_MB" ]; then note "SKIPPED volume $v (${mb} MB > cap ${VOL_CAP_MB} MB)"; continue; fi
  tar -czf "$S/volumes/$v.tgz" -C "$p" . 2>/dev/null || note "WARN: volume $v"
done

# 4. host config: units, helper scripts, ssh host keys, wms + cloudflare config, memory notes
cp -a /etc/systemd/system/{wms-*,kc-parts-*,modernwms-*,partdb-*,crowdsec-*,sshd-log-feed*} "$S/systemd/" 2>/dev/null || true
cp -a /etc/tmpfiles.d/wms-terminal.conf "$S/systemd/" 2>/dev/null || true
cp -a /usr/local/bin/{wms-*.sh,manage-users,modernwms*,partdb-tui,receive-stock,reset-*,script-runner,set-sync-password,start-webtui,user-manager,wms-tui,crowdsec-firewall-bouncer} "$S/bin/" 2>/dev/null || true
tar -czf "$S/config/ssh-host-keys.tgz" -C /etc/ssh ssh_host_*_key ssh_host_*_key.pub 2>/dev/null || tar -czf "$S/config/ssh-host-keys.tgz" -C /etc ssh
tar -czf "$S/config/root-config.tgz" -C /root .config/wms-go .config/cloudflare .config/gh 2>/dev/null || true
tar -czf "$S/config/etc-crowdsec.tgz" -C /etc crowdsec 2>/dev/null || true
[ -d /root/.claude/projects/-root/memory ] && tar -czf "$S/config/claude-memory.tgz" -C /root/.claude/projects/-root memory

# 5. what is installed / running (so the Nix config can be written against facts)
pacman -Qqe > "$S/packages-explicit.txt" 2>/dev/null || true
docker ps -a --format '{{.Names}}\t{{.Image}}\t{{.Ports}}' > "$S/containers.txt"
systemctl list-unit-files --state=enabled --no-legend > "$S/enabled-units.txt"
ss -tlnp > "$S/listening-ports.txt" 2>/dev/null || true
uname -a > "$S/host.txt"; date -u >> "$S/host.txt"

# 6. checksums, then pack + encrypt
( cd "$S" && find . -type f ! -name MANIFEST.sha256 -print0 | sort -z | xargs -0 sha256sum > MANIFEST.sha256 )
final="$OUT/kc-export-$stamp.tar.zst.enc"
tar -C "$OUT/stage-$stamp" -c kc-export | zstd -T0 -3 | enc > "$final"
chmod 600 "$final"
note "wrote $final ($(du -h "$final" | cut -f1))"
echo "$final"
