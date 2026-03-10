# 构建阶段
FROM golang:1.21-alpine AS builder

WORKDIR /app
ENV GOPROXY=https://goproxy.cn
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o ffmpeg-server main.go

# 运行阶段 - 使用 Alpine 并安装 FFmpeg
FROM alpine:3.18

WORKDIR /app

# 安装 FFmpeg 和其他必要工具
RUN apk update && apk add --no-cache \
    ffmpeg \
    wget \
    ca-certificates \
    tzdata \
    && rm -rf /var/cache/apk/*

# 验证 FFmpeg 安装
RUN ffmpeg -version | head -2 && \
    echo "FFmpeg installed successfully"

# 从构建阶段复制 Go 二进制文件
COPY --from=builder /app/ffmpeg-server .

# 创建必要的目录
RUN mkdir -p /tmp/uploads /tmp/outputs

# 暴露端口
EXPOSE 8080

# 健康检查
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --quiet --tries=1 --spider http://localhost:8080/health || exit 1

# 运行应用
CMD ["./ffmpeg-server"]