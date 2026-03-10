#!/bin/bash
# K8s 生产环境部署脚本 - 优雅关闭与连接耗尽更新策略

set -e
K3D_CLUSTER="bubble-system"  # 默认使用 bubble-system 集群
NAMESPACE="${NAMESPACE:-default}"
APP_NAME="bubble-chat"
AUTO_BUILD="${AUTO_BUILD:-false}"   # 生产环境默认不自动构建，使用指定镜像
REGISTRY="${REGISTRY:-}"           # 生产环境镜像仓库

echo "🚀 正在部署 Bubble Chat 到生产环境 Kubernetes..."
echo "   集群: $K3D_CLUSTER"
echo "   命名空间: $NAMESPACE"
echo "   应用: $APP_NAME"
echo "   自动构建: $AUTO_BUILD"
echo ""

# 检查集群是否存在
if ! k3d cluster list | grep -q "$K3D_CLUSTER"; then
    echo "❌ 集群 '$K3D_CLUSTER' 不存在。可用的集群："
    k3d cluster list
    exit 1
fi

# 设置 kubectl 上下文到指定集群
echo "🔧 切换到集群: $K3D_CLUSTER"
kubectl config use-context "k3d-$K3D_CLUSTER"

# 验证集群连接
if ! kubectl cluster-info &> /dev/null; then
    echo "❌ 无法连接到集群 '$K3D_CLUSTER'"
    exit 1
fi

echo "✅ 已连接到集群: $K3D_CLUSTER"

# 检查 kubectl
if ! command -v kubectl &> /dev/null; then
    echo "❌ 未找到 kubectl。请先安装 kubectl。"
    exit 1
fi

# 创建命名空间（如果不存在）
if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
    echo "📦 正在创建命名空间: $NAMESPACE"
    kubectl create namespace "$NAMESPACE"
fi

# 优先使用 k8s/configmap.yaml（或 k8s/configmap-generated.yaml），否则从 .env 生成
if [ -f "k8s/configmap.yaml" ]; then
    echo "📝 检测到 k8s/configmap.yaml，使用该文件作为 ConfigMap"
    CONFIGMAP_FILE="k8s/configmap.yaml"
elif [ -f "k8s/configmap-generated.yaml" ]; then
    echo "📝 检测到 k8s/configmap-generated.yaml，使用该文件作为 ConfigMap"
    CONFIGMAP_FILE="k8s/configmap-generated.yaml"
elif [ -f ".env" ]; then
    echo "📝 未找到 k8s/configmap.yaml，检测到 .env，正在生成 ConfigMap..."
    kubectl create configmap bubble-config \
        --from-env-file=.env \
        -o yaml \
        --dry-run=client > k8s/configmap-generated.yaml
    echo "✅ 已从 .env 文件生成 ConfigMap: k8s/configmap-generated.yaml"
    CONFIGMAP_FILE="k8s/configmap-generated.yaml"
else
    echo "❌ 生产环境必须提供 k8s/configmap.yaml 或 .env 文件"
    exit 1
fi

# 应用 ConfigMap
echo "📝 正在应用 ConfigMap..."
kubectl apply -f "$CONFIGMAP_FILE" -n "$NAMESPACE"

# 优先使用 k8s/secret.yaml 创建 Secret，否则回退到检查集群或 .env.secrets
if [ -f "k8s/secret.yaml" ]; then
    echo "🔐 检测到 k8s/secret.yaml，正在应用 Secret 文件..."
    kubectl apply -f k8s/secret.yaml -n "$NAMESPACE"
else
    # 检查 Secret 是否存在
    if ! kubectl get secret bubble-secrets -n "$NAMESPACE" &> /dev/null; then
        echo "⚠️  未找到 Secret 'bubble-secrets'。"
        
        # 检查是否有 .env.secrets 文件可以自动创建
        if [ -f ".env.secrets" ]; then
            echo "🔐 检测到 .env.secrets 文件，正在自动创建 Secret..."
            kubectl create secret generic bubble-secrets \
                --from-env-file=.env.secrets \
                -n "$NAMESPACE"
            echo "✅ 已从 .env.secrets 创建 Secret"
        else
            echo "❌ 生产环境必须提供 k8s/secret.yaml、.env.secrets 文件或已存在的 Secret"
            echo "   请使用以下命令创建："
            echo "   kubectl create secret generic bubble-secrets \\"
            echo "     --from-literal=database_dsn='...' \\"
            echo "     --from-literal=redis_password='...' \\"
            echo "     --from-literal=jwt_secret='...' \\"
            echo "     --from-literal=smtp_password='...' \\"
            echo "     --from-literal=minio_access_key_id='...' \\"
            echo "     --from-literal=minio_secret_access_key='...' \\"
            echo "     -n $NAMESPACE"
            exit 1
        fi
    else
        echo "✅ Secret 已存在"
    fi
fi

# ...existing code...
# 应用 Service
echo "🌐 正在应用 Service..."
kubectl apply -f k8s/service.yaml -n "$NAMESPACE"

# 应用 PodDisruptionBudget（如果存在）
if [ -f "k8s/pdb.yaml" ]; then
    echo "🛡️  正在应用 PodDisruptionBudget..."
    kubectl apply -f k8s/pdb.yaml -n "$NAMESPACE"
else
    echo "ℹ️  未找到 pdb.yaml，跳过 PodDisruptionBudget 配置"
fi

# 应用 Deployment 模板
echo "📦 正在应用 Deployment 清单..."
kubectl apply -f k8s/deployment.yaml -n "$NAMESPACE"

# 生产环境必须提供镜像
if [ -z "$IMAGE" ]; then
    echo "❌ 生产环境必须通过环境变量 IMAGE 指定镜像"
    echo "   例如: IMAGE=your-registry/bubble:v1.0.0 ./apply.sh"
    exit 1
fi

echo "🔁 正在设置镜像为: $IMAGE"
kubectl set image deployment/$APP_NAME -n "$NAMESPACE" bubble=$IMAGE

# 如果启用自动构建（仅适用于有构建环境的生产场景）
if [ "$AUTO_BUILD" = "true" ]; then
    echo "🔨 自动构建模式：正在构建生产镜像..."
    TAG="prod-$(date -u +%Y%m%d-%H%M%S)"
    IMAGE="bubble:$TAG"
    
    if ! docker build -t $IMAGE -f Dockerfile . ; then
        echo "❌ Docker 构建失败"
        exit 1
    fi

    # 推送到镜像仓库
    if [ -n "$REGISTRY" ]; then
        echo "📦 正在推送镜像到生产仓库 $REGISTRY"
        docker tag $IMAGE $REGISTRY/$IMAGE
        if ! docker push $REGISTRY/$IMAGE ; then
            echo "❌ 镜像推送失败"
            exit 1
        fi
        IMAGE="$REGISTRY/$IMAGE"
    else
        echo "❌ 生产环境自动构建必须设置 REGISTRY 环境变量"
        exit 1
    fi

    echo "🔁 正在更新部署镜像为: $IMAGE"
    kubectl set image deployment/$APP_NAME -n "$NAMESPACE" bubble=$IMAGE
fi

# 强制触发滚动更新：通过注解 timestamp
TS=$(date -u +%s)
echo "🕒 正在修补部署注解 timestamp=$TS 以强制滚动更新"
if kubectl patch deployment/$APP_NAME -n "$NAMESPACE" --type='json' -p="[{\"op\":\"add\",\"path\":\"/spec/template/metadata/annotations/timestamp\",\"value\":\"$TS\"}]" 2>/dev/null ; then
    echo "✅ 已添加 timestamp 注解"
else
    kubectl patch deployment/$APP_NAME -n "$NAMESPACE" --type='json' -p="[{\"op\":\"replace\",\"path\":\"/spec/template/metadata/annotations/timestamp\",\"value\":\"$TS\"}]"
    echo "✅ 已更新 timestamp 注解"
fi

# 等待部署就绪并在失败时尝试回滚
echo ""
echo "⏳ 正在等待部署就绪（超时时间 300 秒）..."
if ! kubectl rollout status deployment/$APP_NAME -n "$NAMESPACE" --timeout=300s; then
    echo "❌ 滚动更新失败。正在收集诊断信息..."
    echo ""
    echo "📋 Pods 状态:"
    kubectl get pods -l app=$APP_NAME -n "$NAMESPACE" -o wide
    echo ""
    echo "🔍 最近事件:"
    kubectl get events -n "$NAMESPACE" --sort-by=.lastTimestamp --field-selector involvedObject.name=$APP_NAME 2>/dev/null || true
    echo ""
    echo "📝 Pods 详情（最后 3 个）:"
    for p in $(kubectl get pods -l app=$APP_NAME -n "$NAMESPACE" -o name | tail -n 3); do
        echo "--- $p ---"
        kubectl describe $p -n "$NAMESPACE" | tail -n 20
        echo "尾部日志:"
        kubectl logs $p -n "$NAMESPACE" --tail=50
        echo ""
    done
    echo "🔄 正在尝试回滚到前一个版本..."
    kubectl rollout undo deployment/$APP_NAME -n "$NAMESPACE"
    echo "❌ 部署失败，已执行回滚。请检查上述日志和事件信息。"
    exit 1
fi

# 检查镜像拉取策略
PULLPOLICY=$(kubectl get deployment $APP_NAME -n "$NAMESPACE" -o jsonpath='{.spec.template.spec.containers[0].imagePullPolicy}')
echo "镜像拉取策略: $PULLPOLICY"

# 显示状态
echo ""
echo "✅ 生产环境部署成功！"
echo ""
echo "📊 当前状态:"
kubectl get deployment $APP_NAME -n "$NAMESPACE"
echo ""
echo "🐳 Pods 状态:"
kubectl get pods -l app=$APP_NAME -n "$NAMESPACE" -o wide
echo ""
echo "🌐 Service 状态:"
kubectl get service bubble-chat-service -n "$NAMESPACE"
echo ""

# 显示生产环境访问信息
echo "🌍 生产环境访问信息:"
echo "   K8s Service 内部端口: 80 (ClusterIP)"
echo "   推荐对外方式: Ingress（请使用 ENABLE_INGRESS=true 并配置 loadbalancer/IngressController）"
echo "   临时访问方法: kubectl port-forward pod/<pod-name> 18080:8080" 
echo "   Pod 容器端口: 8080"
echo ""
echo "🔧 后续维护命令:"
echo "   查看日志: kubectl logs -l app=$APP_NAME -n $NAMESPACE --tail=100"
echo "   重启部署: kubectl rollout restart deployment/$APP_NAME -n $NAMESPACE"
echo "   回滚部署: kubectl rollout undo deployment/$APP_NAME -n $NAMESPACE"
echo "   扩缩容: kubectl scale deployment/$APP_NAME --replicas=5 -n $NAMESPACE"

echo ""
echo "🎉 生产环境部署完成！"

# 开始部署base环境
cd bubble-service
echo "🚀 正在部署 Bubble Base 服务到生产环境 Kubernetes..."
docker compose -f docker-compose-base.yml down
docker compose -f docker-compose-base.yml up -d
echo "🎉 Bubble Base 服务部署完成！"
cd ..