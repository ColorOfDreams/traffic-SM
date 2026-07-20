# syntax=docker/dockerfile:1

ARG TILE38_VERSION=1.38.0

FROM tile38/tile38:${TILE38_VERSION}

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD ["tile38-cli", "ping"]