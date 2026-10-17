#!/usr/bin/env bash
# Deploys this checkout to the LIVE telnet/web gateway, with a backup of everything it touches.
#
#   bash scripts/deploy.sh
#
# It builds the binary, then refuses to go on unless `wms preflight deploy` says GO for this exact
# commit (it runs the preflight itself when there is no fresh verdict). Then it backs up, applies the private
# settings, swaps the binary, restarts the gateway (rolling back if it does not come back), and refreshes
# the offline catalog, a LEGO backup, and doctor.
#
# The legacy KC-LEGO-CLI data import is NOT part of this script — it used to run here automatically
# whenever `sets` read 0, which can't tell "never imported" apart from "just deliberately reset", and
# silently reimported old data after a wipe. It's scripts/import-legacy-data.sh now: a genuinely
# one-time, run-by-hand step, guarded by its own sentinel file rather than the current row count.
#
# Private values are NOT in this repository: they come from the file named by WMS_DEPLOY_VALUES
# (default ~/.config/wms-go/deploy.env, mode 600, KEY=value lines).
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LIVE="${WMS_LIVE_DIR:-/root/docker-server/wms}"
BIN="${WMS_LIVE_BINARY:-/usr/local/bin/wms-go}"
BACKUPS="${WMS_ROLLBACK_DIR:-/root/backups}"
VALUES="${WMS_DEPLOY_VALUES:-$HOME/.config/wms-go/deploy.env}"
LEGACY="${WMS_LEGACY_DIR:-/root/Lego}"
AUDIT_LOG="${AUDIT_LOG_FILE:-/root/tui_audit.log}"
UNIT="${WMS_GATEWAY_UNIT:-wms-gateway.service}"
ts="$(date +%Y%m%d-%H%M%S)"

say() { printf '\n\033[1;32m== %s\033[0m\n' "$*"; }
die() { printf '\033[1;31m%s\033[0m\n' "$*" >&2; exit 1; }

cd "$REPO"
[ "$(id -u)" -eq 0 ] || die "run as root: this replaces $BIN and restarts $UNIT"

say "1/7 build the new binary"
mkdir -p "$BACKUPS"
tmp="$(mktemp "$(dirname "$BIN")/.wms-go.XXXXXX")"
trap 'rm -f "$tmp"' EXIT
version="${WMS_VERSION:-$(git describe --tags --always 2>/dev/null || echo dev)}"
go build -trimpath -ldflags "-X main.version=${version} -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u +%F)" -o "$tmp" ./cmd/wms
chmod 0755 "$tmp"
"$tmp" version

say "2/7 preflight (nothing below runs without a GO for commit $(git rev-parse --short HEAD))"
if ! "$tmp" preflight --require deploy --repo "$REPO"; then
  "$tmp" preflight deploy --repo "$REPO" || die "NO-GO: fix what the preflight lists, then run this again."
fi

say "3/7 backups into $BACKUPS"
sqlite3 "$LIVE/lego.db" ".backup $BACKUPS/lego.db.pre-deploy.$ts"
cp -p "$LIVE/settings.json" "$BACKUPS/settings.json.pre-deploy.$ts"
[ -f "$LIVE/2fa.json" ] && cp -p "$LIVE/2fa.json" "$BACKUPS/2fa.json.pre-deploy.$ts"
cp -p "$BIN" "$BACKUPS/wms-go.$ts"
echo "previous binary: $BACKUPS/wms-go.$ts"

say "4/7 private settings (from $VALUES; values are not printed)"
WMS_VALUES_FILE="$VALUES" WMS_SETTINGS="$LIVE/settings.json" python3 - <<'PY'
import json, os
want = {}
for line in open(os.environ["WMS_VALUES_FILE"]):
    line = line.strip()
    if line and not line.startswith("#") and "=" in line:
        k, v = line.split("=", 1)
        want[k.strip()] = v.strip().strip("\"'")
path = os.environ["WMS_SETTINGS"]
try:
    data = json.load(open(path))
except FileNotFoundError:
    data = {}
data.update(want)
tmp = path + ".new"
with open(tmp, "w") as f:
    json.dump(data, f, indent=2)
os.chmod(tmp, 0o600)
os.replace(tmp, path)
print("settings keys now:", ", ".join(sorted(data)))
PY
chmod 600 "$AUDIT_LOG" 2>/dev/null || true

say "5/7 swap the binary and restart $UNIT (connected sessions are dropped)"
live="$(ss -tn state established '( sport = :23 or sport = :2323 or sport = :7681 )' 2>/dev/null | tail -n +2 | wc -l)"
echo "$live live session(s) will be dropped"
mv -f "$tmp" "$BIN"
trap - EXIT
systemctl restart "$UNIT"
sleep 3
if ! systemctl is-active --quiet "$UNIT"; then
  echo "the gateway did not come back: rolling back"
  cp "$BACKUPS/wms-go.$ts" "$BIN"
  systemctl restart "$UNIT"
  die "rolled back to $BACKUPS/wms-go.$ts"
fi
echo "$UNIT is active"

say "6/7 offline catalog (about 25 s; searching keeps working meanwhile) and a LEGO backup"
"$BIN" lego catalog refresh
"$BIN" lego backup || true

say "7/7 doctor"
"$BIN" doctor || true

if [ ! -f "$LIVE/.legacy-import-done" ] && [ -f "$LEGACY/0.json" ]; then
  echo
  echo "Legacy KC-LEGO-CLI data at $LEGACY has never been imported — a one-time step,"
  echo "run by hand (not part of this script, not run automatically on every deploy):"
  echo "  bash $REPO/scripts/import-legacy-data.sh"
fi

if [ -t 0 ] && [ -f "$REPO/scripts/install-bashrc.sh" ]; then
  echo
  read -r -p "Install the condensed ~/.bashrc (the old one is saved first) and forward the old command names? [y/N] " ans
  if [ "${ans:-n}" = "y" ] || [ "${ans:-n}" = "Y" ]; then
    bash "$REPO/scripts/install-bashrc.sh" --wrappers
  else
    echo "Skipped. Later: bash scripts/install-bashrc.sh [--wrappers]   (--dry-run shows what would change)"
  fi
fi

echo
echo "Done."
echo "Rollback:  cp $BACKUPS/wms-go.$ts $BIN && systemctl restart $UNIT"
echo "Database backup: $BACKUPS/lego.db.pre-deploy.$ts (lego.db is in WAL mode: copy lego.db*, or use 'wms lego backup')."
