#!/usr/bin/env bash
# One-time import of the legacy KC-LEGO-CLI JSON export into the live LEGO
# store — split out of deploy.sh because deploy.sh used to run this
# automatically on every deploy whenever `sets` happened to read 0, which is
# indistinguishable from "a deliberate collection reset just ran" and
# silently reintroduced the old collection about an hour after a wipe (see
# the commit that split this out). This script now owns the "once, ever"
# guarantee itself, via a sentinel file, instead of inferring it from the
# current row count.
#
#   bash scripts/import-legacy-data.sh          # refuses if already imported
#   bash scripts/import-legacy-data.sh --force   # import again anyway
set -euo pipefail

LIVE="${WMS_LIVE_DIR:-/root/docker-server/wms}"
BIN="${WMS_LIVE_BINARY:-/usr/local/bin/wms-go}"
LEGACY="${WMS_LEGACY_DIR:-/root/Lego}"
SENTINEL="$LIVE/.legacy-import-done"

say() { printf '\n\033[1;32m== %s\033[0m\n' "$*"; }
die() { printf '\033[1;31m%s\033[0m\n' "$*" >&2; exit 1; }

force=false
[ "${1:-}" = "--force" ] && force=true

if [ -f "$SENTINEL" ] && [ "$force" = false ]; then
  die "already imported $(cat "$SENTINEL") — run with --force if you really mean to import the legacy files again (e.g. onto a fresh/reset collection)."
fi

[ -f "$LEGACY/0.json" ] || die "no legacy data at $LEGACY/0.json — nothing to import."

args=(lego import --collection "$LEGACY/0.json")
for f in "$LEGACY"/kc_sets_export_*.json; do [ -f "$f" ] && args+=(--collection "$f"); done
[ -f "$LEGACY/Lego-Lookup/legolookup.json" ] && args+=(--ref-sets "$LEGACY/Lego-Lookup/legolookup.json")
[ -f "$LEGACY/Lego-Lookup/legolookup-part.json" ] && args+=(--ref-parts "$LEGACY/Lego-Lookup/legolookup-part.json")

say "importing legacy collection: ${args[*]}"
"$BIN" "${args[@]}"

date -u +%FT%TZ > "$SENTINEL"
echo "done. $SENTINEL written — future deploys and future runs of this script will no longer import automatically."
