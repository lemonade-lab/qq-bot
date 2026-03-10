# FFmpeg

```sh
docker build -t ffmpeg-server:latest .
```

```sh
docker compose up -d
docker compose down
```

## 示例

```bash
# 健康检查
curl http://localhost:19080/health

# 转换格式
curl -X POST http://localhost:19080/convert \
  -F "file=@input.mp4" \
  -F "format=avi"

# 下载文件
curl -O http://localhost:19080/download/{filename}
```
