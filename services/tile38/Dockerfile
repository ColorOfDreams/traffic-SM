# syntax=docker/dockerfile:1

# Build Tile38 bằng Dockerfile chính thức từ Tile38
ARG TILE38_VERSION=1.38.0

# Runtime chỉ cần Tile38 binary
FROM tile38/tile38:${TILE38_VERSION}

# Chuyển sang user không có quyền root để chạy ứng dụng
USER tile38
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD ["tile38-cli", "ping"]
