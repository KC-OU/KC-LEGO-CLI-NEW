# Test instance

A second, local-only copy of the gateway (`wms-gateway-test.service`) for trying out in-progress work — especially
the login/auth changes — without touching the live instance you actually use day to day.

## What's separate, what's shared

| | Live | Test |
|---|---|---|
| Binary | `/usr/local/bin/wms-go` | `/usr/local/bin/wms-go-test` |
| Built from | `main` | `dev` |
| Telnet | 2323 (and 23), reachable externally | 2324, `127.0.0.1` only |
| Web terminal | 7681 → the public domain via Cloudflare | 7691, `127.0.0.1` only |
| LEGO collection | your real one | a fresh copy of the public Rebrickable catalog, empty collection |
| ModernWMS/Part-DB login | — | **same** — the same containers, same accounts, same passwords |
| 2FA | — | **same** `2fa.json` — no separate enrolment needed |
| Settings (API keys, Discord bot, etc.) | — | a one-time copy, independently editable from then on |
| Access control, audit log, exports, archive | — | entirely separate files |

Nothing you do on the test instance — adding parts, checking sets, testing a new report or login flow — ever
touches your real collection, permissions, or audit trail. Signing in uses your real credentials because that part
is deliberately shared.

## Using it

```bash
telnet 127.0.0.1 2324                                   # from this box
ssh -L 7691:127.0.0.1:7691 <this box>                    # then open http://127.0.0.1:7691/ locally
```

The sign-on screen shows a message-of-the-day banner saying it's the test instance, so it's never mistaken for the
real one.

## Redeploying it

```bash
git checkout dev            # in-progress work lives here, not main
bash scripts/deploy-test.sh # build + restart the test service only
```

`main`, the live instance, and the public GitHub repo only move when a change from `dev` has been tried here and
is ready. At that point:

```bash
bash scripts/promote-dev.sh   # fast-forwards main to dev's tip and deploys it live — see below
wms publish                   # separate, deliberate step: pushes to the public GitHub repo
```

## Promoting `dev` to live (`scripts/promote-dev.sh`)

A guarded, self-serve version of "merge `dev` into `main` and deploy it" — so shipping something you've already
tried here doesn't need a fresh conversation. Run as root, from the repo, on `dev`:

```bash
bash scripts/promote-dev.sh
```

It refuses to touch `main` or the live service unless, in order: the working tree is clean and on `dev`; `wms
preflight deploy` says **GO** for `dev`'s tip (the full `-race` test suite, `go vet`, staticcheck, gosec,
govulncheck — everything `scripts/deploy.sh` itself would refuse to skip); and you type `PROMOTE` after seeing
exactly which commits (`git log main..dev`) are about to ship. `main` only ever moves by a clean fast-forward — if
it has diverged, the script stops rather than create a surprise merge. It then calls `scripts/deploy.sh` (backup,
binary swap, restart `wms-gateway.service`, automatic rollback if the service doesn't come back), and leaves you
back on `dev`. It never touches GitHub — `wms publish` stays a separate, deliberate step, run whenever you choose.
