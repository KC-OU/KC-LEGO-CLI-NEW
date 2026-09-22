# Shipping: preflight, deploy and publish

`wms preflight` audits everything that matters before you deploy to a machine or publish the code, and ends in a clear
**GO** or **NO-GO**. It changes nothing: it reads the live files and builds in a scratch folder.

```bash
wms preflight deploy      # the code, the live machine, the new binary on scratch data
wms preflight publish     # the code, what would be published, a clean build, the docs, GitHub
wms preflight all --quick # skip the slow checks while you work
```

On a terminal it is animated (a checklist, a progress bar, the verdict); with `--plain`, `--json` or in a pipe it prints one
line per check.

It is quick: independent checks run **in parallel** (up to four at once), the analysis tools (staticcheck, govulncheck, gosec)
are installed **once** into `~/.cache/wms-preflight/bin` and reused for a week instead of being fetched on every run, the
exported tree is shared between the checks that need it, and the tests use Go's **test cache** (the trap folder has a fixed
path, so unchanged packages are not re-run). A publish preflight leaves the race detector to CI, which runs it on every
push; a deploy preflight still runs it. A publish preflight takes about a minute the first time and under half a minute after.

## What it checks

| Group | Checks |
|---|---|
| Code | clean git tree; gofmt; vet; staticcheck; tidy `go.mod`; the whole test suite with the race detector, run with every writable path pointed at an empty trap folder that must still be empty afterwards (a test that touches live data fails the audit); govulncheck; gosec (new HIGH findings only) |
| Publish | module path, licence and policy files; GitHub sign-in and whether the repository exists; **secrets and personal data in the exported tree**; data files and large files; a clean checkout builds and vets; workflow files; goreleaser; the docs build with `--strict` |
| Deploy | root and required tools; free disk space; a read-only integrity check of the live LEGO database; the settings file is private and the deploy values are present; the new binary builds and runs against scratch data; a rollback copy is possible; ModernWMS and LEGO backups are recent |
| Scripts | `deploy.sh` and `publish.sh` parse, and require a preflight |

The secret scan looks for private keys and tokens, for every value in the machine's own `settings.json`, `2fa.json` and
deploy values, and for the patterns listed in `~/.config/wms-go/pii-patterns.txt` (your domain, e-mail address, IP: one regular
expression per line). Findings name the file and line, never the text.

## The verdict gates the scripts

`wms preflight` saves its verdict (target, commit, time). `scripts/deploy.sh` and `scripts/publish.sh` build the binary and
run `wms preflight --require deploy|publish`, which succeeds only for a **full** GO, for the **same commit**, from the last
15 minutes, earned against the same database and settings files (a GO from a scratch run does not count). When there is none they run the preflight themselves and stop on a NO-GO. So the routine is:

```bash
wms preflight all          # read it
bash scripts/deploy.sh     # backs up, applies the private values, swaps the binary, rolls back on failure
wms publish --wait         # one new commit on top of the public main branch; waits for CI and the docs
```

## Publishing

`wms publish` pushes what is **committed** here to the public repository without your private history: it keeps a clone of
the public repository in `~/.cache/wms-publish/` (fetched, never re-cloned), exports `HEAD` over it, and commits only the
difference — one commit, with your GitHub noreply address — on top of `main`. It refuses without a fresh GO from
`wms preflight publish` for that commit, and runs the preflight itself when there is none.

```bash
wms publish -m "Set checks, orders and labels" --wait
wms publish --tag v1.2.0      # also tags a release (the release workflow builds archives and packages)
wms publish --dry-run         # everything but the commit and push
```

The documentation is built by the repository's own **Docs** workflow and served by **GitHub Pages** only
(https://kc-ou.github.io/KC-LEGO-CLI-NEW/). `vercel.json` turns Vercel's Git deployments off. `scripts/publish.sh` is for
creating the repository the first time (a fresh one-commit history).

Private values are never in the repository: `~/.config/wms-go/deploy.env` (mode 600) holds what deploy writes into the live
settings, and `tools.env` in the same folder holds machine-specific paths such as `WMS_MKDOCS`.
