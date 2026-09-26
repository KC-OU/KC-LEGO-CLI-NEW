#!/usr/bin/env bash
# Publishes this checkout to GitHub, turns on the docs site, waits for CI, and tags a release so the
# release workflow builds binaries and packages.
#
#   bash scripts/publish.sh [tag]        # the tag defaults to v1.0.0
#
# The very first publish (the repository doesn't exist yet) creates it with a single fresh commit under
# your GitHub noreply address — a clean starting point with no local history exposed. Every publish after
# that APPENDS: it fetches the repository's current commits, finds the local commit whose message matches
# its tip, and pushes just the real commits after that point (same messages and dates, author re-mapped to
# your noreply address) — the existing public history is never rewritten or force-pushed.
#
# It refuses to go on unless `wms preflight publish` says GO for this exact commit (it runs the preflight
# itself when there is no fresh verdict): the preflight scans exactly what would be published for secrets and
# personal data, builds a clean checkout, and builds the docs.
set -euo pipefail

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REPO="${WMS_PUBLISH_REPO:-KC-OU/KC-LEGO-CLI-NEW}"
TAG="${1:-v1.0.0}"
OUT="${WMS_PUBLISH_DIR:-${TMPDIR:-/tmp}/wms-publish}"
DESC="${WMS_PUBLISH_DESC:-Successor to KC-LEGO-CLI: LEGO collection manager with an offline Rebrickable catalog, BrickLink prices and a telnet/web terminal UI}"

say() { printf '\n\033[1;32m== %s\033[0m\n' "$*"; }
die() { printf '\033[1;31m%s\033[0m\n' "$*" >&2; exit 1; }

cd "$REPO_DIR"
command -v gh >/dev/null || die "install GitHub's CLI (gh) and run: gh auth login"

say "1/7 build, then preflight (nothing below runs without a GO for commit $(git rev-parse --short HEAD))"
bin="$(mktemp)"
trap 'rm -f "$bin"' EXIT
go build -trimpath -o "$bin" ./cmd/wms
if ! "$bin" preflight --require publish --repo "$REPO_DIR"; then
  "$bin" preflight publish --repo "$REPO_DIR" || die "NO-GO: fix what the preflight lists, then run this again."
fi

say "2/7 identity for the published commit(s)"
name="${WMS_GIT_NAME:-$(gh api user --jq .login)}"
mail="${WMS_GIT_EMAIL:-$(gh api user --jq '"\(.id)+\(.login)@users.noreply.github.com"')}"
echo "$name <$mail>"

rm -rf "$OUT" && mkdir -p "$OUT"
nothing_new=false

if gh repo view "$REPO" >/dev/null 2>&1; then
	say "3/7 what's new since the last publish"
	git clone -q "$REPO_DIR" "$OUT"
	cd "$OUT"
	git remote remove origin
	git remote add origin "https://github.com/$REPO.git"
	git fetch -q origin main
	remote_msg="$(git log -1 --format=%B origin/main)"
	match=""
	while IFS= read -r h; do
		if [ "$(git log -1 --format=%B "$h")" = "$remote_msg" ]; then
			match="$h"
			break
		fi
	done < <(git log --format=%H HEAD)
	[ -n "$match" ] || die "couldn't find a local commit matching the published tip's message — resolve by hand (see the script's header comment)."
	echo "last published: $(git rev-parse --short "$match") \"$(git log -1 --format=%s "$match")\""

	mapfile -t new_commits < <(git rev-list --reverse "$match..HEAD")
	if [ "${#new_commits[@]}" -eq 0 ]; then
		echo "nothing new to publish."
		nothing_new=true
	else
		say "4/7 append ${#new_commits[@]} commit(s), re-authored under your noreply address"
		git checkout -q -b publish-append origin/main
		for c in "${new_commits[@]}"; do
			msg="$(git log -1 --format=%B "$c")"
			adate="$(git log -1 --format=%aI "$c")"
			if ! git cherry-pick -n "$c" >/dev/null; then
				git cherry-pick --abort
				die "cherry-picking $(git rev-parse --short "$c") for publish didn't apply cleanly — resolve it by hand."
			fi
			GIT_AUTHOR_NAME="$name" GIT_AUTHOR_EMAIL="$mail" GIT_AUTHOR_DATE="$adate" \
				GIT_COMMITTER_NAME="$name" GIT_COMMITTER_EMAIL="$mail" \
				git commit -q -m "$msg"
		done
		git log --format='%h %an <%ae>  %s' "origin/main..HEAD"

		say "5/7 push the new commit(s)"
		git push origin publish-append:main
	fi
else
	say "3/7 export exactly what is committed"
	git archive HEAD | tar -x -C "$OUT"
	cd "$OUT"

	say "4/7 fresh history, one commit"
	git init -q -b main
	git -c user.name="$name" -c user.email="$mail" add -A
	git -c user.name="$name" -c user.email="$mail" commit -q -m "Initial commit: wms-go, a LEGO collection manager with an offline Rebrickable catalog, BrickLink prices and a telnet/web terminal UI (successor to KC-LEGO-CLI)"
	git log --format='%h %an <%ae>' | head -1

	say "5/7 create the public repository and push"
	gh repo create "$REPO" --public --description "$DESC" --source . --remote origin --push
fi

if [ "$nothing_new" = true ]; then
	echo
	echo "Repository: https://github.com/$REPO"
	echo "Nothing to tag or wait on — the last publish already covered this commit."
	exit 0
fi

say "6/7 docs site (GitHub Pages from Actions) and CI"
gh api -X POST "repos/$REPO/pages" -f build_type=workflow >/dev/null 2>&1 \
  || gh api -X PUT "repos/$REPO/pages" -f build_type=workflow >/dev/null 2>&1 \
  || echo "could not enable Pages automatically: Settings > Pages > Source: GitHub Actions"
sleep 10
run="$(gh run list -R "$REPO" --branch main --workflow CI --limit 1 --json databaseId -q '.[0].databaseId')"
gh run watch -R "$REPO" "$run" --exit-status || die "CI failed: gh run view -R $REPO $run --log-failed   (no release was tagged)"

say "7/7 tag $TAG (starts the release build: archives, checksums, deb/rpm/apk, container image, attestations)"
git tag -a "$TAG" -m "wms-go ${TAG#v}"
git push origin "$TAG"
sleep 10
rel="$(gh run list -R "$REPO" --workflow Release --limit 1 --json databaseId -q '.[0].databaseId')"
gh run watch -R "$REPO" "$rel" --exit-status || echo "the release run needs attention: gh run list -R $REPO"

owner="${REPO%%/*}"
echo
echo "Repository: https://github.com/$REPO"
echo "Docs (once the Docs workflow finishes): https://$(echo "$owner" | tr '[:upper:]' '[:lower:]').github.io/${REPO#*/}/"
echo
echo "To make $REPO_DIR follow the public history (keeps a bundle of the old one):"
echo "  cd $REPO_DIR && git bundle create ~/wms-old-history-\$(date +%Y%m%d).bundle --all && git remote add origin https://github.com/$REPO.git && git fetch origin && git reset --soft origin/main && git branch -u origin/main"
