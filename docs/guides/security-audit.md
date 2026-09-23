# Security audit

A record of the security review run on this project, what was checked, what was found, and what changed as a result —
so the claims in [Security](security.md) and [SECURITY.md](https://github.com/KC-OU/KC-LEGO-CLI-NEW/blob/main/SECURITY.md)
are backed by something you can read for yourself, not just asserted. Re-run any of this yourself: everything here uses
`gosec`, `govulncheck`, and `go list`, already wired into `wms preflight`.

## Method

1. **Static analysis, full repo**: `gosec` (every rule, not just the HIGH-severity ones `wms preflight` gates on) and
   `govulncheck` (known CVEs reachable from this code).
2. **Manual trust-boundary review**: every place a filesystem path or a subprocess argument comes from something other
   than a fixed, compile-time constant — the two categories (`G304`, `G204`) worth a human looking past the tool's own
   summary, because both are prone to false positives (Go's `exec.Command` never invokes a shell unless you explicitly
   ask it to, so most "tainted subprocess" findings aren't shell-injectable at all — but argument-injection and
   path-traversal are real enough to check case by case).
3. **Dependency audit**: every direct module, beyond what `govulncheck` already flags — anything unmaintained,
   unusually small a maintainer base, or otherwise worth knowing about even without a known CVE.
4. **Secrets-at-rest**: every credential-holding file and its containing directory, checked for permissions that would
   let another local account read or even just list them.

## Findings

### Static analysis
- **gosec, full repo**: 76 findings — 12 HIGH, 64 MEDIUM. All 12 HIGH findings were already reviewed in an earlier pass
  and are listed in `acceptedGosec` (`internal/preflight/checks.go`) with a one-line reason each (an integer that
  provably fits its type, a settings key named like a credential but holding none, a path already validated elsewhere).
  `wms preflight` fails on any HIGH finding *not* in that list, so this isn't a one-time check — it's continuously
  enforced.
- **govulncheck**: 0 reachable vulnerabilities in this code or its imports.

### Manual review of the MEDIUM findings (`G304`/`G204`/`G202`/`G203`)
Every occurrence was traced to its source, not just pattern-matched:

| Where | What it looked like | What it actually is |
|---|---|---|
| `internal/docker/docker.go` (4×) | subprocess args from a variable | the container name is an admin-configured setting, not shown to a shell (Go's `exec.Command` never parses metacharacters) |
| `internal/uiapp/scripts.go` (Script Hub) | subprocess via `sh -c` | args are passed through `"$@"` in a fixed wrapper script — the one safe way to use `sh -c` with dynamic arguments; a typed answer can't start with `-` or contain control characters (`tools.Answer`) |
| `internal/plugin/plugin.go` | a plugin name becomes a file path | validated against `^[a-z0-9][a-z0-9_-]{0,31}$` — no `.` or `/` in the character class, so `../` traversal is not expressible before the path is even built |
| `internal/exports/exports.go` (the `/dl/` download link) | a token becomes a file path | the token is regex-validated (`^[A-Z2-7]{32}$`) before use, the stored filename is re-checked with `filepath.Base`, and the file is opened with `os.OpenInRoot` scoped to the export folder |
| `internal/lego/catalogload.go`, `migrate.go` | file paths and SQL table names from a variable | catalog file names and every SQL table name come from a fixed Go literal (`catalogSpecs`) — table/column names can't be parameterised in `database/sql`, so building them from a closed, compile-time list is the correct pattern, not a shortcut |
| `internal/lego/export.go` (HTML export) | an unescaped template URL | already scoped to `data:image/…` or `https://` only — anything else renders as no image at all |
| **File paths taken from the TUI** | — | there are **none** — every screen reachable over telnet/web that touches the filesystem (exports, labels, imports) writes to a fixed, generated location; reading an arbitrary server-side file by path is only ever a CLI flag, which is the operator's own shell already at full access, not a remote-user boundary |

No fix was needed for any of these — they're correct as written. Two internal notes were added (`G401`/`G117` on the
deliberate MD5 compatibility hash and the TOTP secret's own store) so a future gosec run doesn't need to re-derive this.

### Dependency audit
16 direct dependencies, all from the standard library extension (`golang.org/x/…`), the Charm terminal-UI ecosystem,
`spf13/cobra`, or well-known single-purpose libraries (`filippo.io/age`, `modernc.org/sqlite`, `pquerna/otp`). One flag:
**`github.com/boombuler/barcode`** has no tagged release since 2019 (a pseudo-version is pinned). It's small,
self-contained drawing code with no file or network I/O, so the practical risk is low — but it's the one dependency
worth watching, or replacing, if label generation ever needs a symbology it doesn't have.

### Secrets-at-rest
Every credential file (`settings.json`, `2fa.json`, the sync dashboard's `credentials.json`) was already `0600` — but
their **containing directories** were `0755` (world-listable, though not world-readable-inside), letting another local
account on the box enumerate filenames even without reading them. Tightened to `0700`:
`internal/twofa/twofa.go`, `internal/api/credentials.go`, `internal/config/config.go`. `wms doctor` already checks file
permissions (`checkSecretFiles`); it's a directory-listing hardening, not a fix for a live leak — nothing sensitive was
ever exposed, since the files themselves were correctly locked down throughout.

## What this doesn't cover

This audit is about the code and the two network-facing services it runs (telnet, the web terminal). It does not cover
the Docker containers this box also runs (ModernWMS, Part-DB, the monitoring stack) — those are separately maintained
images with their own update cadence and are outside this repository.

## Reporting something new

See [SECURITY.md](https://github.com/KC-OU/KC-LEGO-CLI-NEW/blob/main/SECURITY.md) — private disclosure through GitHub's
security advisories, not a public issue.
