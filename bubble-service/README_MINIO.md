# MinIO 配置说明

## 安装 MinIO Client (mc)

```sh
docker run --rm -it --entrypoint=/bin/sh minio/mc
```

## 配置 MinIO

```sh
# 1. 设置 MinIO 别名（注意端口：开发环境是 19000，容器内是 9000）
mc alias set minio http://localhost:19000 lemonade lemonade
```

## 存储桶配置

```sh
# 创建所有存储桶
mc mb minio/bubble
mc mb minio/avatars
mc mb minio/covers
mc mb minio/emojis
mc mb minio/guild-chat-files
mc mb minio/private-chat-files
mc mb minio/backups
mc mb minio/temp
```

### 设置存储桶策略

```sh
# 这些存储桶允许匿名下载，用于头像、封面、表情等公开资源
mc anonymous set download minio/bubble
mc anonymous set download minio/avatars
mc anonymous set download minio/covers
mc anonymous set download minio/emojis
mc anonymous set download minio/guild-chat-files
mc anonymous set download minio/private-chat-files
# 不设置匿名访问，保持默认的私有策略
mc anonymous set none minio/backups
mc anonymous set none minio/temp
```

### 分类到存储桶的映射

| 分类 (category)      | 存储桶 (bucket)      | 说明                   |
| -------------------- | -------------------- | ---------------------- |
| `icons`              | `bubble`             | 图标（映射到应用资产） |
| `avatars`            | `avatars`            | 用户头像               |
| `covers`             | `covers`             | 封面图片               |
| `emojis`             | `emojis`             | 表情符号               |
| `guild-chat-files`   | `guild-chat-files`   | 频道文件               |
| `private-chat-files` | `private-chat-files` | 私聊文件               |
| `attachments`        | `guild-chat-files`   | 附件（映射到频道文件） |
| `temp`               | `temp`               | 临时文件               |

## 访问 MinIO 控制台

- 开发环境：http://localhost:56101

- 用户名：lemonade

- 密码：lemonade
