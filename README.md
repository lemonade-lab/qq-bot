# QQ Bot WebSocket Gateway

QQ Bot **Webhook → WebSocket 转发网关**，可一端多bot连接

## 快速开始

### 1. 编译

```bash
go build -o qqbot .
```

### 2. 配置环境变量

```bash
cp .env.example .env
```

编辑 `.env`：

```dotenv
PORT=:9000
GIN_MODE=release
GATEWAY_WS_URL=wss://your-domain.com/websocket
ADMIN_KEY=your-admin-secret
```

| 变量             | 说明                                         | 默认值                          |
| ---------------- | -------------------------------------------- | ------------------------------- |
| `PORT`           | 监听端口                                     | `:9000`                         |
| `GIN_MODE`       | `debug` / `release`                          | `debug`                         |
| `GATEWAY_WS_URL` | `/gateway` 接口返回给客户端的 WS 地址        | `ws://localhost:9000/websocket` |
| `ADMIN_KEY`      | `/admin/bots` 接口的访问密钥（为空则不鉴权） | 空                              |

### 3. 运行

```bash
./qqbot-gateway
```

### 4. QQ Bot 后台配置

在 QQ 开放平台将 Webhook 回调地址设置为：

```
https://your-domain.com/webhook/{你的app_id}
```

## API 端点

| 方法   | 路径                     | 说明                                         |
| ------ | ------------------------ | -------------------------------------------- |
| `POST` | `/app/getAppAccessToken` | 代理官方认证接口，同时捕获开发者凭据         |
| `GET`  | `/gateway`               | 返回配置好的 WebSocket 网关地址              |
| `POST` | `/webhook/:app_id`       | 接收 QQ Bot 官方 Webhook 推送                |
| `GET`  | `/websocket`             | WebSocket 网关（完整 QQ Bot WS 协议）        |
| `GET`  | `/health`                | 健康检查                                     |
| `GET`  | `/admin/bots`            | 查看已注册的 Bot 列表（需 `X-Admin-Key` 头） |

## 开发者接入指南

### 零代码改动方案

只需将 SDK 中的两个 API 地址指向本网关：

```typescript
// 原来
const BOTS_API_URL = 'https://bots.qq.com/app/getAppAccessToken';
const API_URL = 'https://api.sgroup.qq.com/gateway';

// 改为
const BOTS_API_URL = 'https://your-domain.com/app/getAppAccessToken';
const API_URL = 'https://your-domain.com/gateway';
```
