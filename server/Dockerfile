FROM golang:1.26.6-alpine3.23@sha256:e57c41c1d5864341031181b0db34b9a537bb5773eb6428e4e5bdaea0f9135406 AS build

WORKDIR /src

ENV CGO_ENABLED=0

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY cmd ./cmd
COPY data ./data
COPY internal ./internal
COPY migrations ./migrations

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -mod=readonly -trimpath -buildvcs=false -ldflags="-s -w" \
        -o /out/speakup-server ./cmd/server && \
    go build -mod=readonly -trimpath -buildvcs=false -ldflags="-s -w" \
        -o /out/speakup-migrate ./cmd/migrate

FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659 AS runtime

RUN addgroup -S -g 10001 speakup && \
    adduser -S -D -H -u 10001 -G speakup speakup

COPY --from=build --chown=10001:10001 /out/speakup-server /usr/local/bin/speakup-server
COPY --from=build --chown=10001:10001 /out/speakup-migrate /usr/local/bin/speakup-migrate

USER 10001:10001

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -q -T 2 --spider "http://127.0.0.1:${SERVER_PORT:-8080}/readyz" || exit 1

CMD ["/usr/local/bin/speakup-server"]
