# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS builder

RUN GOBIN=/out go install github.com/tsenart/vegeta/v12@v12.13.0

WORKDIR /src
COPY services/location-service/go.mod services/location-service/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked go mod download

COPY services/location-service ./
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    CGO_ENABLED=0 go build \
    -buildvcs=false \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/gps-vegeta-targets \
    ./cmd/gps-vegeta-targets

FROM alpine:3.23

RUN addgroup -S benchmark && adduser -S benchmark -G benchmark
COPY --from=builder /out/vegeta /usr/local/bin/vegeta
COPY --from=builder /out/gps-vegeta-targets /usr/local/bin/gps-vegeta-targets
COPY --chmod=755 tools/gps-benchmark/run-gps-benchmark.sh /usr/local/bin/run-gps-benchmark

USER benchmark
WORKDIR /benchmark

ENTRYPOINT ["/bin/sh"]
