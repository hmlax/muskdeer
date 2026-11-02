FROM golang:1.27.3-bookworm@sha256:972b4110ba0f47539a975c31e52c64765e1ff19f2d74469ae4a595c6676e80a5 AS toolchain
ENV GOTOOLCHAIN=local CGO_ENABLED=0
WORKDIR /workspace
RUN test "$(go env GOVERSION)" = "go1.27.3"

FROM toolchain AS build
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY api ./api
COPY cmd ./cmd
COPY deer ./deer
COPY internal ./internal
COPY web/admin/assets.go ./web/admin/assets.go
COPY web/admin/dist ./web/admin/dist
COPY third_party ./third_party
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -mod=readonly -trimpath -o /out/admin ./cmd/admin && \
    go build -mod=readonly -trimpath -o /out/operator ./cmd/operator && \
    go build -mod=readonly -trimpath -o /out/migrate ./cmd/migrate && \
    go build -mod=readonly -trimpath -o /out/adminuser ./cmd/adminuser
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go list -deps -f '{{with .Module}}{{.Dir}}{{end}}' ./cmd/admin ./cmd/operator ./cmd/migrate ./cmd/adminuser > /tmp/module-dirs && \
    mkdir -p /out/licenses/go /out/licenses/lumberjack && \
    cp /usr/local/go/LICENSE /out/licenses/go/LICENSE && \
    cp third_party/lumberjack/LICENSE /out/licenses/lumberjack/LICENSE && \
    sort -u /tmp/module-dirs > /tmp/module-dirs-sorted && \
    while IFS= read -r module_dir; do \
      case "$module_dir" in /go/pkg/mod/*) \
        target="/out/licenses/${module_dir#/go/pkg/mod/}"; \
        mkdir -p "$target"; \
        find "$module_dir" -maxdepth 1 -type f \( -iname 'LICENSE*' -o -iname 'NOTICE*' -o -iname 'COPYING*' \) -exec cp '{}' "$target/" \; ;; \
      esac; \
    done < /tmp/module-dirs-sorted

FROM scratch AS runtime-base
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/licenses /licenses
USER 65532:65532
WORKDIR /app
STOPSIGNAL SIGTERM
FROM runtime-base AS runtime-admin
COPY --from=build /out/admin /admin
COPY --from=build /out/migrate /migrate
COPY --from=build /out/adminuser /adminuser
ENTRYPOINT ["/admin"]

FROM runtime-base AS runtime-operator
COPY --from=build /out/operator /operator
ENTRYPOINT ["/operator"]
