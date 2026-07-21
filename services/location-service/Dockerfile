# syntax=docker/dockerfile:1
# Build Location Service bằng Go 1.26 và Alpine Linux
FROM golang:1.26-alpine AS builder

# Chuyển thư mục sang /src
WORKDIR /src

# Cache dependency riêng với source code
COPY go.mod go.sum ./

RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    go mod download

# Copy toàn bộ source code vào container
COPY . .

# Build binary Linux hoàn toàn static
RUN --mount=type=cache,target=/go/pkg/mod,sharing=locked \
    --mount=type=cache,target=/root/.cache/go-build,sharing=locked \
    CGO_ENABLED=0 GOOS=linux go build \
    -buildvcs=false \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/location-service \
    .

# Runtime binary Linux hoàn toàn static với Alpine Linux và CA certificates
FROM alpine:3.23 AS runtime

# Certificate cần thiết nếu service gọi HTTPS
RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S app -G app

# Chuyển sang thư mục /app để chạy ứng dụng
WORKDIR /app

# Chuyển binary từ builder sang runtime, đặt quyền sở hữu cho user app
COPY --from=builder --chown=app:app \
    /out/location-service \
    /app/location-service

# Chuyển sang user không có quyền root để chạy ứng dụng
USER app

#  Expose cổng 8080 cho Location Service
EXPOSE 8080

# Thiết lập healthcheck để kiểm tra trạng thái của Location Service
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --quiet --output-document=/dev/null \
    http://127.0.0.1:8080/health || exit 1

# Thiết lập lệnh khởi động mặc định để chạy Location Service
ENTRYPOINT ["/app/location-service"]
