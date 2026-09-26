#!/usr/bin/env bash
# Promotes commits from `dev` to `main` and deploys them live — the one command you need
# to ship what you've already tried on the test instance, without a fresh conversation.
# See docs/guides/test-instance.md#promoting-dev-to-live-scriptspromote-devsh.
#
#   bash scripts/promote-dev.sh
#
# Not everything on dev is necessarily ready at once, so it lists dev's commits not yet on
# main (newest first, numbered from 1) and you pick what ships: a single number (that commit
# and everything older), 'all', or a comma list like '1,3,6' to hold specific commits back.
# A comma list that skips something cherry-picks instead of fast-forwarding — see the warning
# printed before it runs. It refuses to touch main or the live service unless, in order:
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
echo "The list above is newest first. You can:"
echo "  - type a single number: that commit and everything older ships (a safe prefix)"
echo "  - type 'all': everything listed ships"
echo "  - type a comma list, e.g. 1,3,6: only those ship, holding the rest back — see the"
echo "    warning below before using this; it cherry-picks instead of a plain fast-forward"
read -r -p "Ship which one(s) (1-$n, comma list, or 'all')? " pick
[ "$pick" = "all" ] && pick=1

picks=()
IFS=',' read -r -a picks <<<"$pick"
for i in "${!picks[@]}"; do
	picks[$i]="$(echo "${picks[$i]}" | tr -d '[:space:]')"
	case "${picks[$i]}" in
	'' | *[!0-9]*) die "not a number: ${picks[$i]}" ;;
	esac
	[ "${picks[$i]}" -ge 1 ] && [ "${picks[$i]}" -le "$n" ] || die "out of range: ${picks[$i]} (pick 1-$n)"
done
mapfile -t sorted < <(printf '%s\n' "${picks[@]}" | sort -nu)

# A plain, contiguous prefix from 1 (however it was typed — "3", "1,2,3", "all") is the
# safe, ff-only case: every one of dev's own commits ships as-is, nothing skipped.
is_prefix=true
for i in "${!sorted[@]}"; do
	[ "${sorted[$i]}" -eq "$((i + 1))" ] || { is_prefix=false; break; }
done

bin="$(mktemp)"
trap 'rm -f "$bin"; git checkout -q "$branch" 2>/dev/null || true' EXIT

if [ "$is_prefix" = true ]; then
	pick="${sorted[-1]}"
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
else
	echo
	echo "WARNING: holding commits back out of order means CHERRY-PICKING, not a plain"
	echo "fast-forward. Each shipped commit still has to apply cleanly on its own, but a held-"
	echo "back commit's changes won't be there even if a later one quietly relies on them —"
	echo "the preflight below (full build + tests) is what catches that, not git itself."
	echo "Held-back commits stay on dev, unshipped, until you run this again and pick them."
	echo
	echo "Will ship, oldest first:"
	for ((i = ${#sorted[@]} - 1; i >= 0; i--)); do
		git log -1 --format="%s  (%h, %ar)" "${hashes[$((${sorted[$i]} - 1))]}" | sed 's/^/  /'
	done
	echo "Held back (still on dev, not shipped this time):"
	held=false
	for ((num = 1; num <= n; num++)); do
		if ! printf '%s\n' "${sorted[@]}" | grep -qx "$num"; then
			git log -1 --format="%s  (%h, %ar)" "${hashes[$((num - 1))]}" | sed 's/^/  /'
			held=true
		fi
	done
	[ "$held" = true ] || echo "  (none — this is just an out-of-order pick of the same commits)"

	tmpref="promote-dev-tmp-$$"
	git branch -q "$tmpref" main
	cleanup_tmp() { git checkout -q "$branch" 2>/dev/null || true; git branch -q -D "$tmpref" 2>/dev/null || true; }
	trap 'rm -f "$bin"; cleanup_tmp' EXIT
	git checkout -q "$tmpref"
	for ((i = ${#sorted[@]} - 1; i >= 0; i--)); do
		src="${hashes[$((${sorted[$i]} - 1))]}"
		if ! git cherry-pick "$src" >/dev/null; then
			git cherry-pick --abort
			cleanup_tmp
			die "cherry-picking $(git rev-parse --short "$src") didn't apply cleanly — resolve it by hand, or drop it from the list."
		fi
	done
	target="$(git rev-parse "$tmpref")"

	say "4/6 preflight for the cherry-picked tree (full test suite, vet, staticcheck, gosec, ...)"
	go build -trimpath -o "$bin" ./cmd/wms
	if ! "$bin" preflight deploy --repo "$REPO"; then
		cleanup_tmp
		die "NO-GO: fix what the preflight lists, then run this again."
	fi
	git checkout -q "$branch"

	say "5/6 confirm"
	read -r -p "Type CONFIRM to ship these $((${#sorted[@]})) commit(s) (cherry-picked) into main and deploy live: " ans
	[ "$ans" = "CONFIRM" ] || { cleanup_tmp; die "cancelled (typed '$ans', not CONFIRM)"; }

	git checkout -q main
	if ! git merge -q --ff-only "$tmpref"; then
		git checkout -q "$branch"
		cleanup_tmp
		die "main can't fast-forward to the cherry-picked tree — sort main out by hand, then try again."
	fi
	git branch -q -D "$tmpref"
fi

say "6/6 deploy it live"
bash "$REPO/scripts/deploy.sh"

git checkout -q "$branch"
trap - EXIT
rm -f "$bin"
echo
echo "Done. main is now at $(git rev-parse --short main)."
if ! git merge-base --is-ancestor dev main; then
	echo "dev still has newer (or held-back) commits not yet promoted — run this again when they're ready."
	if [ "$is_prefix" = false ]; then
		echo "Note: the commit(s) just shipped now have a NEW hash on main (cherry-picked); dev's own"
		echo "copy of them will keep showing up in this list until you rebase dev onto main by hand."
	fi
fi
echo "This never touches GitHub — publish when you're ready: wms publish   (or bash scripts/publish.sh)"
