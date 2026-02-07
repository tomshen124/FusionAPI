FROM golang:1.21-alpine AS builder

WORKDIR /app

# 安装依赖
RUN apk add --no-cache gcc musl-dev

# 复制 go mod 文件
COPY go.mod go.sum* ./
RUN go mod download

# 复制源代码
COPY . .

# 构建
RUN CGO_ENABLED=1 go build -o fusionapi ./cmd/fusionapi

# 运行镜像
FROM alpine:latest

WORKDIR /app

# 安装 SQLite 运行时依赖
RUN apk add --no-cache ca-certificates

# 复制二进制
COPY --from=builder /app/fusionapi .
COPY --from=builder /app/config.yaml .

# 创建数据目录
RUN mkdir -p /app/data

# 暴露端口
EXPOSE 18080

# 运行
CMD ["./fusionapi", "-config", "config.yaml"]
