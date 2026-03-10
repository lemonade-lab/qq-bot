# QQ Bot WebSocket Gateway

QQ Bot 官方不再支持 WebSocket 连接方式。本项目通过 Go 实现一个 **Webhook → WebSocket 转发网关**，让开发者仍然可以使用 WebSocket 模式进行开发，**无需修改任何 SDK 业务代码**。

## 原理

```
开发者 SDK                           本网关                            QQ Bot 官方
  │                                    │                                  │
  │─ POST /app/getAppAccessToken ────>│── 代理转发 ─────────────────────>│
  │  {appId, clientSecret}             │  ✅ 捕获 secret → 派生 Ed25519   │
  │<─ {access_token, expires_in} ─────│<─ 原样返回 ────────────────────│
  │                                    │                                  │
  │─ GET /gateway ──────────────────>│                                  │
  │  Authorization: QQBot {token}      │  ✅ 存储 token → app_id 映射     │
  │<─ {url: "wss://你的域名/websocket"}│                                  │
  │                                    │                                  │
  │═ WS 连接 ════════════════════════>│                                  │
  │<═ op=10 Hello ═══════════════════│                                  │
  │═ op=2 Identify ══════════════════>│  ✅ token 查表 → 匹配 bot        │
  │<═ op=0 READY ════════════════════│                                  │
  │═ op=1 Heartbeat ═════════════════>│                                  │
  │<═ op=11 ACK ═════════════════════│                                  │
  │                                    │                                  │
  │                                    │<── POST /webhook/{app_id} ─────│
  │                                    │  ✅ Ed25519 签名验证              │
  │<═ op=0 事件推送 ═════════════════│                                  │
```

## 快速开始

### 1. 编译

```bash
go build -o qqbot-gateway .
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

每个开发者的 `app_id` 作为 URL 的一部分，实现零过滤路由。

## API 端点

| 方法   | 路径                     | 说明                                         |
| ------ | ------------------------ | -------------------------------------------- |
| `POST` | `/app/getAppAccessToken` | 代理官方认证接口，同时捕获开发者凭据         |
| `GET`  | `/gateway`               | 返回配置好的 WebSocket 网关地址              |
| `POST` | `/webhook/:app_id`       | 接收 QQ Bot 官方 Webhook 推送                |
| `GET`  | `/websocket`             | WebSocket 网关（完整 QQ Bot WS 协议）        |
| `GET`  | `/health`                | 健康检查                                     |
| `GET`  | `/admin/bots`            | 查看已注册的 Bot 列表（需 `X-Admin-Key` 头） |

## WebSocket 协议

完全兼容原版 QQ Bot WebSocket 协议：

| OpCode | 方向 | 说明                                        |
| ------ | ---- | ------------------------------------------- |
| 10     | S→C  | Hello（包含 `heartbeat_interval`）          |
| 2      | C→S  | Identify（`token: "QQBot {access_token}"`） |
| 0      | S→C  | Dispatch（`READY` 事件 / 业务事件推送）     |
| 1      | C→S  | Heartbeat                                   |
| 11     | S→C  | Heartbeat ACK                               |
| 9      | S→C  | Invalid Session                             |

### Identify 支持两种 token 格式

```jsonc
// 方式 1：无感模式（推荐，SDK 零改动）
{ "op": 2, "d": { "token": "QQBot {access_token}" } }

// 方式 2：直连模式
{ "op": 2, "d": { "token": "Bot {app_id}.{secret}" } }
```

## 开发者接入指南

### 零代码改动方案

只需将 SDK 中的两个 API 地址指向本网关：

```typescript
// 原来
const BOTS_API_URL = 'https://bots.qq.com';
const API_URL = 'https://api.sgroup.qq.com';

// 改为
const BOTS_API_URL = 'https://your-domain.com';
const API_URL = 'https://your-domain.com';
```

SDK 内部调用链路完全透明：
1. `POST /app/getAppAccessToken` → 代理到官方 API，捕获凭据
2. `GET /gateway` → 返回你的 WS 地址
3. WS 连接和事件推送 → 与原版 QQ Bot 完全一致

### 事件消息格式

转发到 WebSocket 的事件消息：

```json
{
  "op": 0,
  "t": "GROUP_AT_MESSAGE_CREATE",
  "d": { ... },
  "access_token": "当前有效的access_token"
}
```

`access_token` 字段由网关自动附加，开发者可直接用于调用 QQ Bot API。

## 项目结构

```
├── main.go                 入口 & 路由
└── forward/
    ├── crypto.go           Ed25519 签名（密钥派生、验签、挑战签名）
    ├── auth.go             FetchAccessToken + TokenManager 自动续期
    ├── store.go            多租户 BotStore（Bot 管理、WS Client 管理）
    ├── gateway.go          /app/getAppAccessToken 代理 + /gateway 端点
    ├── hub.go              WebSocket 网关协议实现
    └── webhook.go          Webhook 接收与事件转发
```

## 安全特性

- **Ed25519 签名验证**：通过开发者的 `secret` 派生密钥对，验证 Webhook 请求签名
- **协议内认证**：WS 连接通过 op=2 Identify 认证，无敏感信息暴露在 URL
- **access_token 自动续期**：在过期前 80% 时自动刷新
- **并发安全**：每个 WS Client 独立写锁，防止并发写入冲突
- **Admin 接口保护**：可选的 `ADMIN_KEY` 鉴权

## License

MIT
