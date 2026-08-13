# ADR-0005：部署形态 SPA + Nginx 反代；实时性用 SSE

- 状态：**Accepted**
- 日期：2026-08-01

## 背景（Context）

前端为纯静态 SPA，需要对外提供页面与 API；platform-service 需要调用 controller-go；管理台需要实例/节点状态的实时刷新。

## 决策（Decision）

### 1. 部署形态

```text
Nginx（单入口）
├── /        → platform-web 静态文件（SPA，history 路由回退到 index.html）
└── /api/*   → 反代到 platform-service
                  └── 内部 HTTP 调用 controller-go（内网）
```

- 不引入 SSR；构建产物为纯静态文件。
- 开发环境：Vite dev server 配置 `/api` proxy 到 platform-service。

### 2. 实时性：SSE，不上 WebSocket

- 实例状态 / 节点状态推送使用 **SSE（Server-Sent Events）**：Go 标准库即可实现，前端 `EventSource` 一行接入。
- MVP 阶段甚至可先用轮询（5~10s），后续平滑升级为 SSE。
- 仅当出现双向场景（如 Web 终端、日志流）再引入 WebSocket。

## 理由（Why）

- SPA + Nginx 部署最简单，无 Node 运行时依赖；符合管理后台形态。
- SSE 对“服务端 → 浏览器”单向推送足够，实现成本远低于 WebSocket（无连接升级、无帧协议）。

## 后果（Consequences）

### Positive

- 部署与运维简单（一个 Nginx + 两个 Go 服务）；前后端可独立发布。

### Negative / 代价

- Nginx 需配置 history 回退与反代超时；SSE 长连接需 Nginx 关闭缓冲（`proxy_buffering off`）。
- 多副本时 SSE 广播需引入 Redis pub/sub 等（当前单实例阶段可忽略）。
