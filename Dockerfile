# 后端构建阶段
FROM golang:1.24 AS builder
WORKDIR /app
ENV GOPROXY=https://goproxy.cn
COPY src ./src
COPY docs ./docs
COPY go.mod go.sum main.go ./
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=0.0.1
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
  go build -ldflags "-X main.Version=${VERSION} -X main.BuildTime=$(date +%s) -s -w" -o bubble .

# 运行阶段 - 使用轻量级镜像并包含健康检查工具
FROM alpine:latest
RUN apk --no-cache add ca-certificates curl jq
WORKDIR /usr/local/bin

# 从构建阶段复制二进制文件到标准可执行目录
COPY --from=builder /app/bubble /usr/local/bin/bubble

# 确保可执行并设置合理属主/权限
RUN chmod 0755 /usr/local/bin/bubble \
  && addgroup -S appgroup && adduser -S appuser -G appgroup \
  && chown appuser:appgroup /usr/local/bin/bubble

# 暴露端口
EXPOSE 8080

# 健康检查
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8080/health/live || exit 1

# 切换为非 root 用户
USER appuser

# 运行应用
ENTRYPOINT ["/usr/local/bin/bubble"]