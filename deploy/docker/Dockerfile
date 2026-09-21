# Builds the wms sync/API daemon (`wms sync serve`). The TUI/gateway binary
# runs on the host directly (it needs `docker`/`ttyd` on PATH and a real tty),
# so this image is scoped to the sync+metrics service only, matching the
# original's partdb-sync container.
ARG GO_VERSION=1.26
FROM golang:${GO_VERSION}-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/wms ./cmd/wms

# The sync engine's ModernWMS write path shells out to `docker exec` (the
# modernwms container has no sqlite3 CLI and no bind mount, only python3 —
# see the plan's "ModernWMS DB access strategy"), so the runtime image needs
# a real `docker` CLI on PATH, not a distroless/static base.
FROM docker:27-cli
COPY --from=build /out/wms /usr/local/bin/wms
EXPOSE 8082
ENTRYPOINT ["/usr/local/bin/wms", "sync", "serve"]
