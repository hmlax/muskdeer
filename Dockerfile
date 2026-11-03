FROM golang:1.27.3-bookworm@sha256:972b4110ba0f47539a975c31e52c64765e1ff19f2d74469ae4a595c6676e80a5 AS toolchain
ENV GOTOOLCHAIN=local CGO_ENABLED=0
WORKDIR /workspace
RUN test "$(go env GOVERSION)" = "go1.27.3"

FROM toolchain AS build
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY market ./market
COPY strategy ./strategy
COPY execution ./execution
COPY portfolio ./portfolio
COPY deer ./deer
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    for name in market strategy execution portfolio; do \
      go build -mod=readonly -trimpath -o /out/$name ./$name/cmd || exit $?; \
    done && \
    for name in migrate coreinit readycheck; do \
      go build -mod=readonly -trimpath -o /out/$name ./cmd/$name || exit $?; \
    done
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go list -deps -f '{{with .Module}}{{.Dir}}{{end}}' ./market/cmd ./strategy/cmd ./execution/cmd ./portfolio/cmd ./cmd/migrate ./cmd/coreinit ./cmd/readycheck > /tmp/module-dirs && \
    mkdir -p /out/licenses/go /out/licenses/lumberjack && \
    cp /usr/local/go/LICENSE /out/licenses/go/LICENSE && \
    cp deer/third_party/lumberjack/LICENSE /out/licenses/lumberjack/LICENSE && \
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
COPY --from=build /out/migrate /migrate
COPY --from=build /out/coreinit /coreinit
COPY --from=build /out/readycheck /readycheck
USER 65532:65532
WORKDIR /app
STOPSIGNAL SIGTERM

FROM runtime-base AS runtime-market
COPY --from=build /out/market /market
ENTRYPOINT ["/market"]

FROM runtime-base AS runtime-strategy
COPY --from=build /out/strategy /strategy
ENTRYPOINT ["/strategy"]

FROM runtime-base AS runtime-execution
COPY --from=build /out/execution /execution
ENTRYPOINT ["/execution"]

FROM runtime-base AS runtime-portfolio
COPY --from=build /out/portfolio /portfolio
ENTRYPOINT ["/portfolio"]
