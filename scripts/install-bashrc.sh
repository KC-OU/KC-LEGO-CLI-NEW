#!/usr/bin/env bash
# Installs the condensed ~/.bashrc (scripts/bashrc.sh) after backing up the old one, and can replace the old
# Python command names in /usr/local/bin with small forwarders to wms-go.
#
#   bash scripts/install-bashrc.sh              # just the bashrc
#   bash scripts/install-bashrc.sh --wrappers   # also forward receive-stock, reset-password, manage-users, wms-backup ...
#   bash scripts/install-bashrc.sh --dry-run    # show what would change (touches nothing)
#
# Nothing is deleted: the old bashrc and the old wrapper links are copied to /root/backups first, and both
# changes are undone by copying them back.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKUPS="${WMS_ROLLBACK_DIR:-/root/backups}"
GO="${WMS_LIVE_BINARY:-/usr/local/bin/wms-go}"
BIN_DIR="${WMS_BIN_DIR:-/usr/local/bin}"
ts="$(date +%Y%m%d-%H%M%S)"
wrappers=0
dry=0
for a in "$@"; do
  [ "$a" = "--wrappers" ] && wrappers=1
  [ "$a" = "--dry-run" ] && dry=1
done

die() { printf '\033[1;31m%s\033[0m\n' "$*" >&2; exit 1; }
say() { printf '\033[1;32m%s\033[0m\n' "$*"; }

[ "$dry" -eq 1 ] || "$GO" menu --help >/dev/null 2>&1 || die "$GO has no launcher yet: run scripts/deploy.sh first, then this."
[ "$dry" -eq 1 ] || mkdir -p "$BACKUPS"

say "1/2 ~/.bashrc"
old="$BACKUPS/bashrc.pre-menu.$ts"
[ "$dry" -eq 1 ] && old=~/.bashrc || cp -p ~/.bashrc "$old"
alias_names() {  # every name defined by an "alias a='..' b='..'" line
  grep -E '^\s*alias ' "$1" | grep -oE "(alias |[[:space:]])[A-Za-z0-9_.-]+='" | sed -E "s/^(alias |[[:space:]])//; s/='\$//" | sort -u
}
missing="$(comm -23 <(alias_names "$old") <(alias_names "$REPO/scripts/bashrc.sh") | tr '\n' ' ')"
if [ "$dry" -eq 1 ]; then
  printf 'dry run. old: %s lines, new: %s lines\n' "$(wc -l < "$old")" "$(wc -l < "$REPO/scripts/bashrc.sh")"
else
  cp "$REPO/scripts/bashrc.sh" ~/.bashrc
  printf 'old: %s lines, new: %s lines (old file saved as %s)\n' "$(wc -l < "$old")" "$(wc -l < ~/.bashrc)" "$old"
fi
[ -n "$missing" ] && printf 'aliases that no longer exist (use q, or see: wms-go menu list): %s\n' "$missing"

if [ "$dry" -eq 1 ]; then
  echo "dry run: nothing was changed"
  exit 0
fi

if [ "$wrappers" -eq 1 ]; then
  say "2/2 forwarders in $BIN_DIR (old links saved in $BACKUPS/wrappers.$ts)"
  mkdir -p "$BACKUPS/wrappers.$ts"
  fwd() {  # fwd name body-file-content
    local name="$1" path="$BIN_DIR/$1"
    if [ -L "$path" ] && readlink -f "$path" | grep -q modernwms-partdb-suite; then
      cp -P "$path" "$BACKUPS/wrappers.$ts/"
      rm -f "$path"
      printf '#!/bin/sh\n# forwards to wms-go (the old Python tool is still in /root/modernwms-partdb-suite)\n%s\n' "${2//@GO@/$GO}" > "$path"
      chmod 0755 "$path"
      echo "  $name -> wms-go"
    elif [ -e "$path" ]; then
      echo "  $name: left alone (not a link into the Python suite)"
    fi
  }
  for n in modernwms-backup wms-backup; do fwd "$n" 'exec @GO@ backup "$@"'; done
  fwd receive-stock 'exec @GO@ receive "$@"'
  for n in reset-modernwms-password reset-wms-password; do fwd "$n" 'case "${1:-}" in
  ""|-h|--help) echo "usage: reset-password <user> [new_password] | --temp <user> | --list"; exit 0;;
  -l|--list) exec @GO@ users list;;
  -t|--temp) shift; exec @GO@ users reset "$@";;
  *) if [ -n "${2:-}" ]; then exec @GO@ users set-password "$@"; else exec @GO@ users reset "$@"; fi;;
esac'; done
  for n in manage-users user-manager; do fwd "$n" 'case "${1:-}" in
  -l|--list|list) shift; exec @GO@ users list "$@";;
  -c|--create|create) shift; exec @GO@ users create "$@";;
  -r|--reset|reset) shift; exec @GO@ users reset "$@";;
  --set-password|set-password) shift; exec @GO@ users set-password "$@";;
  -d|--delete|delete) shift; exec @GO@ users delete "$@";;
  --toggle-active|toggle-active) shift; exec @GO@ users toggle "$@";;
  *) exec @GO@ users --help;;
esac'; done
  for n in modernwms modernwms-tui wms-tui partdb-tui; do fwd "$n" 'exec @GO@ tui "$@"'; done
  fwd script-runner 'exec @GO@ menu "$@"'
else
  echo "(the old Python command names in $BIN_DIR are unchanged; add --wrappers to forward them to wms-go)"
fi

echo
echo "Open a new shell, or type: source ~/.bashrc"
echo "Undo:  cp $old ~/.bashrc"
