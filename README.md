## 快速开始

必要环境：go、redis、mysql

- 配置 env

```sh
cp .env.example .env
```

> 自行配置 redis、mysql

- 运行

```sh
go mod tidy
go run main.go
```

### 自建环境

- redis&mysql

```sh
docker compose -f bubble-service/docker-compose-dev.yml down
docker compose -f bubble-service/docker-compose-dev.yml up -d
```

- logs

> 日志系统

```sh
docker compose -f bubble-service/docker-compose-logs.yml down
docker compose -f docker-compose-logs.yml up -d
```

- livekit

```sh
docker compose -f  bubble-service/docker-compose-livekit.yml down
docker compose -f  bubble-service/docker-compose-livekit.yml up -d
```

- oss

```sh
# 启动本地OSS
docker compose -f bubble-service/docker-compose-oss.yml down
docker compose -f bubble-service/docker-compose-oss.yml up -d
# 关闭服务
```

如果不使用线上OSS服务，请调整OSS为本地服务，

`MINIO_ENDPOINT=localhost:19000`

`MINIO_USE_SSL=false`

存储桶，默认都是不公开访问的，请配置[OSS](./README_MINIO.md)

### 生产环境

> 请确保运行 redis&mysql、livekit、oss

```sh
k3d cluster delete bubble-system
# k3d cluster create github --port "30080:30080" --port "31318:31318"
k3d cluster create bubble-system --port "30080:30080" --port "31318:31318"
# build
docker build -t bubble:latest --build-arg VERSION=latest .
# docker load -i bubble.tar.gz
# 加载镜像
k3d image import bubble:latest -c bubble-system
# update
IMAGE=bubble:latest ./apply.sh
```

```sh
docker compose down
docker compose up -d
```

```sh
docker compose rm -sf nginx-web
docker compose up -d nginx-web
```

#### status

```sh
kubectl get pods -n default -l app=bubble-chat --show-labels
```

#### look

```sh
kubectl describe pod <name> -n default
```

#### logs

```sh
kubectl logs <name>
```

#### reclear

```sh
kubectl delete pods -l app=bubble-chat -n default
```
