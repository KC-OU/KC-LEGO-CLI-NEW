#!/usr/bin/env bash
# Promotes commits from `dev` to `main` and deploys them live — the one command you need
# to ship what you've already tried on the test instance, without a fresh conversation.
# See docs/guides/test-instance.md#promoting-dev-to-live-scriptspromote-devsh.
#
#   bash scripts/promote-dev.sh
#
# Not everything on dev is necessarily ready at once, so it lists dev's commits not yet on
# main (newest first, numbered from 1) and you pick one: that commit AND EVERYTHING BELOW IT
# IN THE LIST (older) ships — a safe prefix, never an arbitrary pick, so nothing half-depends
# on a commit left behind. It refuses to touch main or the live service unless, in order:
#   - the working tree is clean and currently on `dev`
#   - `wms preflight deploy` says GO for the commit you picked (full -race test suite, vet,
#     staticcheck, gosec, ...)
#   - you type CONFIRM, after seeing exactly which commits are about to ship
#
# main only ever moves by a clean fast-forward (never a merge commit, never forced) — if
# main has diverged from dev, the script stops rather than risk a surprise merge. This never
# touches GitHub: `wms publish` stays a separate, deliberate step you run whenever you choose.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO"
export GIT_PAGER=cat # non-interactive script: never page, and never fail on a missing pager

say() { printf '\n\033[1;35m== %s\033[0m\n' "$*"; }
die() { printf '\033[1;31m%s\033[0m\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "run as root: this restarts wms-gateway.service"

say "1/6 working tree"
branch="$(git rev-parse --abbrev-ref HEAD)"
[ "$branch" = "dev" ] || die "checked out on '$branch', not dev — switch first: git checkout dev"
[ -z "$(git status --porcelain)" ] || die "uncommitted changes on dev — commit or stash them first"

if git merge-base --is-ancestor dev main; then
	echo "main is already at dev's tip — nothing to promote."
	exit 0
fi

say "2/6 what's on dev but not yet on main (newest first)"
mapfile -t hashes < <(git log --format='%H' main..dev)
n=${#hashes[@]}
for i in "${!hashes[@]}"; do
	git log -1 --format="%s  (%h, %ar)" "${hashes[$i]}" | sed "s/^/$((i + 1))) /"
done

say "3/6 pick what ships"
echo "Type a number: that commit and everything below it in the list (older) ships."
read -r -p "Ship up to and including which number (1-$n)? " pick
case "$pick" in
'' | *[!0-9]*) die "not a number: $pick" ;;
esac
[ "$pick" -ge 1 ] && [ "$pick" -le "$n" ] || die "out of range: $pick (pick 1-$n)"
target="${hashes[$((pick - 1))]}"

echo
echo "Will ship (main -> $(git rev-parse --short "$target")):"
git log --oneline "main..$target"
left=$((pick - 1))
if [ "$left" -gt 0 ]; then
	echo "  ($left newer commit(s) on dev are NOT included this time — run this again for those later)"
fi

say "4/6 preflight for that commit's tree (full test suite, vet, staticcheck, gosec, ...)"
git checkout -q "$target"
bin="$(mktemp)"
trap 'rm -f "$bin"; git checkout -q "$branch" 2>/dev/null || true' EXIT
go build -trimpath -o "$bin" ./cmd/wms
if ! "$bin" preflight deploy --repo "$REPO"; then
	die "NO-GO: fix what the preflight lists, then run this again."
fi
git checkout -q "$branch"

say "5/6 confirm"
read -r -p "Type CONFIRM to merge up to $(git rev-parse --short "$target") into main and deploy it live: " ans
[ "$ans" = "CONFIRM" ] || die "cancelled (typed '$ans', not CONFIRM)"

git checkout -q main
if ! git merge -q --ff-only "$target"; then
	git checkout -q "$branch"
	die "main can't fast-forward to that commit — sort main out by hand, then try again."
fi

say "6/6 deploy it live"
bash "$REPO/scripts/deploy.sh"

git checkout -q "$branch"
trap - EXIT
rm -f "$bin"
echo
echo "Done. main is now at $(git rev-parse --short main)."
if ! git merge-base --is-ancestor dev main; then
	echo "dev still has newer commits not yet promoted — run this again when they're ready."
fi
echo "This never touches GitHub — publish when you're ready: wms publish   (or bash scripts/publish.sh)"
