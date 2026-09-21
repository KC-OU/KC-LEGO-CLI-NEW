# Contributing

Thanks for helping. This is a small, careful project; a few habits keep it that way.

## Set up

```bash
git clone https://github.com/KC-OU/KC-LEGO-CLI-NEW && cd KC-LEGO-CLI-NEW
go build ./... && go test ./... -count=1
```

Go 1.26 or newer. No CGo, no C toolchain.

## Before you open a pull request

- `gofmt -l .` prints nothing; `go vet ./...` is clean; `go test ./... -count=1` passes (add `-race` for anything concurrent).
- Every bug fix comes with a test that fails without it. Every new parser (anything reading a file or the network) comes with a fuzz test.
- Tests must never touch real data: use `partdbtest` (a Part-DB with the real schema), fake HTTP servers, and temp directories.
  The test helpers point every path at a temp directory and refuse live paths.
- User-visible changes go in `CHANGELOG.md` and, if they change how something is used, in `docs/`.

## Docs

The documentation site is MkDocs Material, built from `docs/`. Edit the Markdown, then preview with:

```bash
pip install mkdocs-material
mkdocs serve
```

The screenshots and the command reference are generated, so do not edit them by hand: `make docs-generate`
(see `docs/README.md`).
