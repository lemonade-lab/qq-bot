#!/bin/bash
set -e  # 任何命令失败就停止

REMOTE_USER="root"
REMOTE_IP="127.0.0.1"
REMOTE_PATH="/Users/lemonade/Desktop"
IMAGE_NAME="bubble"
IMAGE_TAG="latest"

echo "🐳 构建Docker镜像..."

docker build -t ${IMAGE_NAME}:${IMAGE_TAG} .

echo "📦 保存并压缩镜像..."
docker save ${IMAGE_NAME}:${IMAGE_TAG} | gzip > ./${IMAGE_NAME}.tar.gz

echo "📊 镜像大小:"
du -sh ./${IMAGE_NAME}.tar.gz

echo "🚀 传输到远程服务器..."
scp ./${IMAGE_NAME}.tar.gz ${REMOTE_USER}@${REMOTE_IP}:${REMOTE_PATH}/

echo "📥 在服务器加载镜像..."
ssh ${REMOTE_USER}@${REMOTE_IP} "
  cd ${REMOTE_PATH} && 
  gunzip -c ${IMAGE_NAME}.tar.gz | docker load && 
  echo '✅ 镜像加载完成' && 
  docker images | grep ${IMAGE_NAME}
"

echo "🎉 部署完成!"