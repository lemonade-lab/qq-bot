#!/bin/bash
set -e

# Remove existing builder if it exists
docker buildx rm gobuilder 2>/dev/null || true

# Create and use new builder
docker buildx create --name gobuilder --config ./docker-buildkitd.toml --use --driver-opt network=host
docker buildx inspect --bootstrap

# Get version from git
VERSION=$(git describe --tags --abbrev=0 2>/dev/null || git rev-parse --short HEAD 2>/dev/null || echo "0.0.1")
echo "Using VERSION=${VERSION}"

# Build and push
ADDRESS="registry-hub.alemonjs.com"
APP_NAME="bubble"

docker buildx build \
   --platform linux/amd64,linux/arm64 \
   -t ${ADDRESS}/${APP_NAME}:latest \
   -t ${ADDRESS}/${APP_NAME}:${VERSION} \
   --build-arg VERSION=${VERSION} \
   --push .

echo "Build completed successfully!"
# docker exec -it <name> bash