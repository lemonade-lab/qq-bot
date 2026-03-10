# 构建阶段
FROM golang:1.24 AS builder
WORKDIR /app
ENV GOPROXY=https://goproxy.cn
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
COPY forward/ ./forward/
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=0.0.1
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
  go build -ldflags "-X main.Version=${VERSION} -s -w" -o qqbot-gateway .

# 运行阶段
FROM alpine:latest
RUN apk --no-cache add ca-certificates curl \
  && addgroup -S appgroup && adduser -S appuser -G appgroup

COPY --from=builder /app/qqbot-gateway /usr/local/bin/qqbot-gateway
RUN chmod 0755 /usr/local/bin/qqbot-gateway

EXPOSE 9000

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:9000/health || exit 1

USER appuser

ENTRYPOINT ["/usr/local/bin/qqbot-gateway"]