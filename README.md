# Traffic System

Phân tích dữ liệu GPS tài xế xác định thông tin độ tắc nghẽn, gồm 3 service

- **Location Service:** sử dụng ngôn ngữ go và framework gin.
- **GraphHopper:** sử dụng trực tiếp mã nguồn mở và có thể set up sau.
- **Tile38:** cơ sở dữ liệu không gian phục vụ dữ liệu vị trí thời gian thực.

## Kiến trúc hiện tại

Ba service chạy trong cùng một Docker network:

| Service | Địa chỉ nội bộ |
|---|---|
| Location Service | `location-service:8080` |
| GraphHopper | `graphhopper:8989` |
| Tile38 | `tile38:9851` |

Location Service có thể kết nối tới GraphHopper và Tile38 thông qua service name của Docker Compose, không sử dụng IP container cố định.

## Định hướng cấu trúc code

Phần Location Service viết lại sử dụng **Feature-first Pipeline với Matching Strategy**. Pipeline dùng một output map matching chung để có thể bắt đầu với GraphHopper và thay hoặc so sánh với phương pháp Tile38 xử lý từng GPS trong tương lai.

Xem cấu trúc package, luồng phụ thuộc và các pattern dự kiến tại [docs/traffic_structure.md](../docs/traffic_structure.md).

## Yêu cầu

Máy phát triển cần có:

- Docker Desktop
- Docker Compose
- Git

Không bắt buộc cài Go, Java hoặc Maven trực tiếp trên máy. Các công cụ build đã được đóng gói trong Docker image.

## Clone dự án

Clone repository và tải GraphHopper submodule:

```bash
git clone --recurse-submodules <repository-url>
```

Nếu đã clone repository nhưng chưa có source GraphHopper:

```bash
git submodule update --init --recursive
```

## Chuẩn bị dữ liệu OpenStreetMap

Tải một vùng nhỏ từ OpenStreetMap dưới dạng `.osm` và đặt tại:

```text
data/osm/hanoi-test.osm
```

## Khởi động hệ thống

Build và khởi động ba service:

```bash
docker compose up --build -d
```

Kiểm tra trạng thái:

```bash
docker compose ps
```

Kết quả mong muốn là cả ba service đều chuyển sang trạng thái `healthy`.

## Các endpoint kiểm tra

| Service | Endpoint |
|---|---|
| Location Service | `http://localhost:8080/health` |
| GraphHopper | `http://localhost:8989/health` |
| Tile38 | `http://localhost:9851/ping` |

Có thể kiểm tra bằng PowerShell:

```powershell
Invoke-RestMethod http://localhost:8080/health
Invoke-RestMethod http://localhost:8989/health
Invoke-RestMethod http://localhost:9851/ping
```

## Kiểm tra kết nối nội bộ

Location Service gọi GraphHopper:

```bash
docker compose exec location-service wget -qO- http://graphhopper:8989/health
```

Location Service gọi Tile38:

```bash
docker compose exec location-service wget -qO- http://tile38:9851/ping
```

## Dừng hệ thống

Dừng và xóa các container:

```bash
docker compose down
```

Dừng hệ thống và xóa cả dữ liệu Tile38 cùng GraphHopper cache:

```bash
docker compose down -v
```

Lưu ý: `docker compose down -v` sẽ xóa các named volume và khiến GraphHopper phải import lại dữ liệu OSM trong lần chạy tiếp theo.

## Trạng thái hiện tại

- [x] GraphHopper được build từ source bằng Maven và Java 25.
- [x] Tile38 sử dụng image phiên bản cố định `1.38.0`.
- [x] Location Service được build bằng Go 1.26 và Gin.
- [x] Ba service chạy trong cùng Docker network.
- [x] Health check hoạt động cho cả ba service.
