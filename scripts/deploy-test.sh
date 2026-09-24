#!/usr/bin/env bash
# Redeploys the current checkout (normally the `dev` branch) to the local-only
# TEST instance (wms-gateway-test.service) — never touches the live instance,
# its binary, its database, or its settings. See docs/guides/test-instance.md.
#
#   bash scripts/deploy-test.sh
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="/usr/local/bin/wms-go-test"
UNIT="wms-gateway-test.service"

say() { printf '\n\033[1;36m== %s\033[0m\n' "$*"; }
die() { printf '\033[1;31m%s\033[0m\n' "$*" >&2; exit 1; }

cd "$REPO"
[ "$(id -u)" -eq 0 ] || die "run as root: this replaces $BIN and restarts $UNIT"

say "1/3 build"
go build -trimpath -o "$BIN.new" ./cmd/wms
chmod 0755 "$BIN.new"
mv "$BIN.new" "$BIN"
"$BIN" version

say "2/3 restart $UNIT"
systemctl restart "$UNIT"
sleep 1
systemctl is-active --quiet "$UNIT" || die "did not come back up: journalctl -u $UNIT"

say "3/3 done"
echo "Telnet:  telnet 127.0.0.1 2324"
echo "Web:     ssh -L 7691:127.0.0.1:7691 <this box> , then open http://127.0.0.1:7691/"
echo "Logs:    journalctl -u $UNIT -f"
