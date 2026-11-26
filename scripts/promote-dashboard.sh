#!/usr/bin/env bash
# The dashboard (Rust) half of a promote: lists the dashboard commits that are not live yet, tests them, and after
# CONFIRM builds the release binary and restarts the dashboard. Run by promote-dev.sh after the Go deploy (and when
# there is nothing to promote on the Go side); it can also be run on its own.
#
#   bash scripts/promote-dashboard.sh
#
# "Live" is the commit recorded in $MARK after the last successful run. The previous binary is kept in
# /root/backups so a dashboard that does not come back up is rolled back on the spot.
# The dashboard needs no `wms preflight`: its own gate is the full cargo test run below.
set -euo pipefail

DASH="${KC_DASHBOARD_DIR:-/root/kc-parts-dashboard-rs}"
MARK="${KC_DASHBOARD_MARK:-/var/lib/kc-parts-dashboard/deployed-commit}"
SERVICE=kc-parts-dashboard
BIN="$DASH/target/release/kc_parts_dashboard"

AUTO=0
for arg in "$@"; do
	case "$arg" in -y | --yes | --confirm) AUTO=1 ;; esac
done
say() { printf '\n\033[1;35m== %s\033[0m\n' "$*"; }
die() { printf '\033[1;31m%s\033[0m\n' "$*" >&2; exit 1; }

[ -d "$DASH/.git" ] || { echo "no dashboard checkout at $DASH — skipping the dashboard."; exit 0; }
export GIT_PAGER=cat PATH="$PATH:/root/.cargo/bin"
if [ "$AUTO" = 1 ]; then # -y needs the promote PIN: already checked by promote-dev.sh (passed on, never printed), else ask
	. "$(dirname "${BASH_SOURCE[0]}")/promote-pin.sh"
	{ [ -n "${PROMOTE_PIN:-}" ] && pin_ok "$PROMOTE_PIN"; } || pin_ask || die "no PIN, no -y: run it without -y to confirm by hand."
fi
cd "$DASH"

say "dashboard (Rust): commits that are not live yet"
head="$(git rev-parse HEAD)"
live="$(cat "$MARK" 2>/dev/null || true)"
if [ "$live" = "$head" ]; then
	echo "the dashboard is already running $(git rev-parse --short HEAD) — nothing to do."
	exit 0
fi
if [ -n "$live" ] && git cat-file -e "$live^{commit}" 2>/dev/null; then
	git log --oneline "$live..HEAD"
else
	echo "(no record of what is live yet — HEAD $(git log -1 --format='%h %s'))"
fi
[ "$(git rev-parse --abbrev-ref HEAD)" = "master" ] || die "the dashboard is on '$(git rev-parse --abbrev-ref HEAD)', not master — switch first."
[ -z "$(git status --porcelain)" ] || die "uncommitted changes in $DASH — commit or stash them first."

say "dashboard tests"
cargo test --release 2>&1 | tail -4
[ "${PIPESTATUS[0]}" -eq 0 ] || die "NO-GO: the dashboard tests fail; nothing was changed."

if [ "$AUTO" = 1 ]; then
	ans=CONFIRM
	echo "Building the dashboard at $(git rev-parse --short HEAD) and restarting it (-y: PIN accepted)"
else
	read -r -p "Type CONFIRM to build the dashboard at $(git rev-parse --short HEAD) and restart it: " ans
fi
[ "$ans" = "CONFIRM" ] || die "cancelled (typed '$ans', not CONFIRM)"

say "build and restart"
mkdir -p /root/backups "$(dirname "$MARK")"
backup="/root/backups/kc_parts_dashboard.$(date +%Y%m%d-%H%M%S)"
cp "$BIN" "$backup" 2>/dev/null || backup=""
cargo build --release 2>&1 | tail -2
systemctl restart "$SERVICE"
sleep 3
code="$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:3100/health/check || true)"
if [ "$code" != "401" ] && [ "$code" != "200" ]; then
	if [ -n "$backup" ]; then
		cp "$backup" "$BIN" && systemctl restart "$SERVICE"
		die "the new dashboard did not come back (HTTP $code); the previous one was restored from $backup."
	fi
	die "the new dashboard did not come back (HTTP $code) and there was no earlier binary to restore."
fi
echo "$head" >"$MARK"
echo "Dashboard live at $(git rev-parse --short HEAD) (HTTP $code). Rollback: cp $backup $BIN && systemctl restart $SERVICE"
