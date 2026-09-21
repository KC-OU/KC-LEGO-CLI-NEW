#!/usr/bin/env bash
# Publishes this checkout to GitHub with a FRESH history (one commit, your GitHub noreply address), turns on
# the docs site, waits for CI, and tags a release so the release workflow builds binaries and packages.
#
#   bash scripts/publish.sh [tag]        # the tag defaults to v1.0.0
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

say "2/7 identity for the single commit"
name="${WMS_GIT_NAME:-$(gh api user --jq .login)}"
mail="${WMS_GIT_EMAIL:-$(gh api user --jq '"\(.id)+\(.login)@users.noreply.github.com"')}"
echo "$name <$mail>"

say "3/7 export exactly what is committed"
rm -rf "$OUT" && mkdir -p "$OUT"
git archive HEAD | tar -x -C "$OUT"

say "4/7 fresh history, one commit"
cd "$OUT"
git init -q -b main
git -c user.name="$name" -c user.email="$mail" add -A
git -c user.name="$name" -c user.email="$mail" commit -q -m "Initial commit: wms-go, a LEGO collection manager with an offline Rebrickable catalog, BrickLink prices and a telnet/web terminal UI (successor to KC-LEGO-CLI)"
git log --format='%h %an <%ae>' | head -1

say "5/7 create the public repository and push"
gh repo create "$REPO" --public --description "$DESC" --source . --remote origin --push

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
