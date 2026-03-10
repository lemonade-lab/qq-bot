#!/bin/bash
# Bubble Chat 生产环境优雅关闭脚本 - 仅停止运行，不删除资源

set -e
K3D_CLUSTER="bubble-system"
NAMESPACE="${NAMESPACE:-default}"
APP_NAME="bubble-chat"
TIMEOUT="${TIMEOUT:-300}"  # 等待超时时间（秒）
FORCE="${FORCE:-false}"    # 强制关闭，不等待优雅关闭

echo "🛑 正在优雅关闭 Bubble Chat 生产环境..."
echo "   集群: $K3D_CLUSTER"
echo "   命名空间: $NAMESPACE"
echo "   应用: $APP_NAME"
echo "   等待超时: ${TIMEOUT}秒"
echo "   强制模式: $FORCE"
echo ""

# 检查集群是否存在
if ! k3d cluster list | grep -q "$K3D_CLUSTER"; then
    echo "❌ 集群 '$K3D_CLUSTER' 不存在。可用的集群："
    k3d cluster list
    exit 1
fi

# 设置 kubectl 上下文
echo "🔧 切换到集群: $K3D_CLUSTER"
kubectl config use-context "k3d-$K3D_CLUSTER"

# 验证集群连接
if ! kubectl cluster-info &> /dev/null; then
    echo "❌ 无法连接到集群 '$K3D_CLUSTER'"
    exit 1
fi

echo "✅ 已连接到集群: $K3D_CLUSTER"

# 检查应用是否存在
if ! kubectl get deployment $APP_NAME -n "$NAMESPACE" &> /dev/null; then
    echo "⚠️  部署 '$APP_NAME' 在命名空间 '$NAMESPACE' 中不存在"
    echo "ℹ️   跳过K8s应用关闭，继续关闭Base服务..."
else
    # 1. 显示当前状态
    echo "📊 当前状态:"
    kubectl get deployment $APP_NAME -n "$NAMESPACE"
    echo ""
    
    CURRENT_REPLICAS=$(kubectl get deployment $APP_NAME -n "$NAMESPACE" -o jsonpath='{.status.replicas}' 2>/dev/null || echo "0")
    READY_REPLICAS=$(kubectl get deployment $APP_NAME -n "$NAMESPACE" -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo "0")
    
    echo "   当前副本数: ${CURRENT_REPLICAS:-0}, 就绪副本: ${READY_REPLICAS:-0}"
    
    # 2. 缩减副本数为0进行优雅关闭
    if [[ ${CURRENT_REPLICAS:-0} -gt 0 ]]; then
        echo ""
        echo "📉 正在缩减副本数到 0 (优雅关闭)..."
        kubectl scale deployment/$APP_NAME --replicas=0 -n "$NAMESPACE"
        echo "✅ 已发送停止指令"
        
        # 等待Pod终止
        if [ "$FORCE" = "false" ]; then
            echo "⏳ 等待 Pod 优雅终止 (最多 ${TIMEOUT}秒)..."
            echo "   Pod将完成现有请求并释放连接..."
            
            START_TIME=$(date +%s)
            LAST_STATUS=""
            
            while true; do
                CURRENT_TIME=$(date +%s)
                ELAPSED=$((CURRENT_TIME - START_TIME))
                
                if [ $ELAPSED -gt $TIMEOUT ]; then
                    echo "⚠️  超时：等待Pod终止超时"
                    echo "   剩余Pod可能需要强制清理或手动检查"
                    break
                fi
                
                # 获取Pod状态
                PODS_INFO=$(kubectl get pods -l app=$APP_NAME -n "$NAMESPACE" --no-headers 2>/dev/null || true)
                POD_COUNT=$(echo "$PODS_INFO" | wc -l)
                
                if [[ "$POD_COUNT" -eq "0" || -z "$PODS_INFO" ]]; then
                    echo "✅ 所有 Pod 已成功终止"
                    break
                fi
                
                # 统计不同状态的Pod
                RUNNING_COUNT=0
                TERMINATING_COUNT=0
                COMPLETED_COUNT=0
                
                while IFS= read -r line; do
                    if [[ -n "$line" ]]; then
                        STATUS=$(echo "$line" | awk '{print $3}')
                        case $STATUS in
                            "Running")
                                RUNNING_COUNT=$((RUNNING_COUNT + 1))
                                ;;
                            "Terminating")
                                TERMINATING_COUNT=$((TERMINATING_COUNT + 1))
                                ;;
                            "Completed"|"Succeeded"|"Error"|"Failed")
                                COMPLETED_COUNT=$((COMPLETED_COUNT + 1))
                                ;;
                        esac
                    fi
                done <<< "$PODS_INFO"
                
                CURRENT_STATUS="运行:${RUNNING_COUNT} 终止中:${TERMINATING_COUNT} 已完成:${COMPLETED_COUNT}"
                
                # 只在状态变化时输出
                if [ "$CURRENT_STATUS" != "$LAST_STATUS" ]; then
                    echo "   等待中... ${CURRENT_STATUS} (已等待 ${ELAPSED}秒)"
                    LAST_STATUS="$CURRENT_STATUS"
                fi
                
                # 如果没有运行中的Pod，只有终止中的Pod，等待更短时间
                if [ $RUNNING_COUNT -eq 0 ] && [ $TERMINATING_COUNT -gt 0 ]; then
                    sleep 2
                else
                    sleep 5
                fi
            done
        else
            echo "⚡ 强制模式：跳过等待Pod终止"
        fi
        
        # 显示关闭后的Pod状态
        echo ""
        echo "📋 Pod 关闭状态:"
        kubectl get pods -l app=$APP_NAME -n "$NAMESPACE" 2>/dev/null || echo "   所有Pod已停止"
        
        # 显示部署状态
        echo ""
        echo "📊 部署状态 (副本数应为0):"
        kubectl get deployment $APP_NAME -n "$NAMESPACE"
    else
        echo "✅ 应用已在停止状态 (副本数: 0)"
    fi
fi

# 3. 关闭base服务
echo ""
echo "🛑 正在关闭 Bubble Base 服务..."
if [ -d "bubble-service" ]; then
    cd bubble-service
    
    # 检查docker-compose文件是否存在
    if [ -f "docker-compose-base.yml" ]; then
        echo "正在停止 Base 服务容器 (MySQL, Redis, MinIO)..."
        
        # 显示停止前的容器状态
        echo ""
        echo "🐳 停止前的容器状态:"
        docker ps --filter "name=bubble" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
        echo ""
        
        # 停止容器
        if docker compose -f docker-compose-base.yml down; then
            echo "✅ Bubble Base 服务已停止"
        else
            echo "⚠️  Base 服务停止过程中出现警告或错误"
        fi
        
        # 显示停止后的容器状态
        echo ""
        echo "🐳 停止后的容器状态:"
        docker ps --filter "name=bubble" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
    else
        echo "❌ 错误：未找到 docker-compose-base.yml 文件"
        echo "   当前目录: $(pwd)"
        ls -la *.yml || echo "   没有找到yml文件"
    fi
    
    cd ..
else
    echo "⚠️  bubble-service 目录不存在，跳过Base服务关闭"
fi

echo ""
echo "🎉 优雅关闭完成！"
echo ""
echo "🔧 应用已停止，但K8s资源（Deployment, Service, ConfigMap, Secret等）仍然保留"
echo ""
echo "📝 重新启动命令:"
echo "   1. 恢复K8s应用运行:"
echo "      kubectl scale deployment/$APP_NAME --replicas=2 -n $NAMESPACE"
echo ""
echo "   2. 或者使用完整启动脚本重新部署:"
echo "      ./apply.sh"
echo ""
echo "   3. 如果需要启动Base服务:"
echo "      cd bubble-service && docker compose -f docker-compose-base.yml up -d"
echo ""
echo "🔍 查看状态命令:"
echo "   kubectl get deployment $APP_NAME -n $NAMESPACE"
echo "   kubectl get pods -l app=$APP_NAME -n $NAMESPACE"
echo "   docker ps --filter \"name=bubble\""