#!/usr/bin/env bash
# Runs a personal, standalone copy of the wms CLI/telnet/web gateway ("solo mode") on THIS machine --
# for a second Arch box with no access to the live ModernWMS/Part-DB docker containers. See
# docs/guides/solo-mode.md. Sign-in uses a local password (internal/access's own PasswordHash field),
# not either backend; the LEGO collection is a fresh copy of the public Rebrickable catalog, empty.
#
# Nothing here touches /usr/local/bin/wms-go, /root/docker-server/wms, any systemd unit, or the
# existing loopback-only wms-gateway-test.service -- this is entirely its own data directory under
# $HOME, no root needed, no containers needed.
#
#   bash scripts/solo-instance.sh kc              # first run: build, create account "kc", start
#   bash scripts/solo-instance.sh stop
#   bash scripts/solo-instance.sh status
#
# Override with env vars: WMS_SOLO_DATA_DIR, WMS_SOLO_HOST (default 0.0.0.0), WMS_SOLO_TELNET_PORT
# (default 2325), WMS_SOLO_WEB_PORT (default 7692).
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DATA="${WMS_SOLO_DATA_DIR:-$HOME/.local/share/wms-solo}"
BIN="$DATA/bin/wms-go-solo"
PIDFILE="$DATA/gateway.pid"
LOG="$DATA/gateway.log"

say() { printf '\n\033[1;35m== %s\033[0m\n' "$*"; }
die() { printf '\033[1;31m%s\033[0m\n' "$*" >&2; exit 1; }

export WMS_SOLO_MODE=1
export LEGO_DB_PATH="${LEGO_DB_PATH:-$DATA/lego.db}"
export WMS_SETTINGS_FILE="${WMS_SETTINGS_FILE:-$DATA/settings.json}"
export WMS_ACCESS_FILE="${WMS_ACCESS_FILE:-$DATA/access.json}"
export TWOFA_FILE="${TWOFA_FILE:-$DATA/2fa.json}"
export LOGIN_LOCK_FILE="${LOGIN_LOCK_FILE:-$DATA/login_lock.json}"
export AUDIT_LOG_FILE="${AUDIT_LOG_FILE:-$DATA/audit.log}"
export LISTEN_PORTS="${WMS_SOLO_TELNET_PORT:-2325}"
export GATEWAY_PORT="${WMS_SOLO_WEB_PORT:-7696}"
# ttyd itself only ever binds loopback (internal/gateway/web.go passes -i lo),
# proxied externally through GATEWAY_PORT above -- this just needs to not
# collide with another ttyd already running on this box. ttyd also claims
# TTYD_PORT+1 for /solo, so leave a gap.
export TTYD_PORT="${WMS_SOLO_TTYD_PORT:-7698}"
export GATEWAY_LISTEN_HOST="${WMS_SOLO_HOST:-0.0.0.0}"
export WMS_MOTD="${WMS_MOTD:-*** PERSONAL SOLO COPY -- not the live instance, not connected to it ***}"

mkdir -p "$DATA/bin"

case "${1:-start}" in
  stop)
    [ -f "$PIDFILE" ] || die "not running (no $PIDFILE)"
    # The gateway spawns ttyd (and tmux) as children for the web terminal;
    # killing just its own pid would leave those orphaned, still holding the
    # port -- -PID kills the whole process group started by setsid below.
    kill -TERM -- "-$(cat "$PIDFILE")" 2>/dev/null || true
    rm -f "$PIDFILE"
    echo "stopped"
    exit 0
    ;;
  status)
    if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
      echo "running (pid $(cat "$PIDFILE")) -- telnet $GATEWAY_LISTEN_HOST $LISTEN_PORTS"
    else
      echo "not running"
    fi
    exit 0
    ;;
  start) user="${2:-}" ;;
  *) user="$1" ;;
esac

if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
  die "already running (pid $(cat "$PIDFILE")) -- 'bash scripts/solo-instance.sh stop' first"
fi

say "1/4 build (from this checkout, branch $(git -C "$REPO" branch --show-current 2>/dev/null || echo '?'))"
cd "$REPO"
go build -trimpath -o "$BIN.new" ./cmd/wms
chmod 0755 "$BIN.new"
mv -f "$BIN.new" "$BIN"
"$BIN" version

first_run=0
if [ ! -f "$WMS_ACCESS_FILE" ]; then
  first_run=1
  [ -n "$user" ] || die "first run: pass a username, e.g. 'bash scripts/solo-instance.sh kc'"
fi

if [ "$first_run" = 1 ]; then
  say "2/4 creating local account '$user' (password shown once below)"
  "$BIN" access user set-password "$user" --source local
  "$BIN" access user set-groups "local:$user" admin

  say "3/4 seeding a fresh public LEGO catalog (empty collection)"
  "$BIN" lego catalog refresh || echo "catalog refresh failed/skipped -- fine, it'll retry later"
else
  say "2/4 account and catalog already set up, skipping"
  say "3/4 (skipped)"
fi

say "4/4 starting -- telnet $GATEWAY_LISTEN_HOST $LISTEN_PORTS   web http://$GATEWAY_LISTEN_HOST:$GATEWAY_PORT/"
# setsid makes this its own process group leader, so `stop` can kill it and
# the ttyd/tmux children it spawns together (see the -PID kill above).
setsid nohup "$BIN" gateway serve >>"$LOG" 2>&1 &
echo $! >"$PIDFILE"
sleep 1
if ! kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
  die "did not stay up -- check $LOG"
fi
echo "running (pid $(cat "$PIDFILE")), logs: $LOG"
