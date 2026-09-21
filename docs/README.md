# Editing the documentation

The site is [MkDocs Material](https://squidfunk.github.io/mkdocs-material/), built from the Markdown in this folder and
published to GitHub Pages by `.github/workflows/docs.yml` whenever you push to `main`. To change a page, edit its `.md` file
and push (or use the pencil icon on any page of the site).

```bash
pip install -r docs/requirements.txt
make docs-serve        # generates the screens and command reference, then serves http://127.0.0.1:8000
make docs-build        # the same strict build the CI runs
```

**Generated, not hand-written** (both are rebuilt by `make docs-generate` and are git-ignored):

| What | From | Where |
|------|------|-------|
| Terminal screenshots | the real screens, rendered from a small demo collection (`internal/uiapp/docs_screens_test.go`) | `docs/assets/screens/*.html` |
| Command reference | the program's own `--help` (`wms docs-gen`) | `docs/reference/cli/*.md` |

To show a screen on a page: `--8<-- "docs/assets/screens/hub.html"` on a line by itself. Add a new screen by adding
a `shot("name")` in `docs_screens_test.go`.

`QA.md` (the bug ledger) and this file are excluded from the site.
