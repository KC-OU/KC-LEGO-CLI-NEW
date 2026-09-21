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
bash scripts/publish.sh    # fresh one-commit history, public repo, docs site, CI, tagged release
```

Private values are never in the repository: `~/.config/wms-go/deploy.env` (mode 600) holds what deploy writes into the live
settings, and `tools.env` in the same folder holds machine-specific paths such as `WMS_MKDOCS`.
