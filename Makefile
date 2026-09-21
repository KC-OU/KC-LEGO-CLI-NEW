.PHONY: build test docs-generate docs-serve docs-build
BIN ?= wms

build:
	CGO_ENABLED=0 go build -trimpath -o $(BIN) ./cmd/wms

test:
	go vet ./... && go test ./... -count=1

# The command reference and the screenshots are generated, never edited by hand.
docs-generate:
	go run ./cmd/wms docs-gen docs/reference/cli
	WMS_DOCS_OUT=$(CURDIR)/docs/assets/screens go test ./internal/uiapp -count=1 -run TestDocScreensRender

docs-serve: docs-generate
	mkdocs serve

docs-build: docs-generate
	mkdocs build --strict
