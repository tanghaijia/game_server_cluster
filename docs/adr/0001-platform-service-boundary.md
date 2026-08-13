# ADR-0001：用户/账单等平台业务独立为新服务，不写入 controller-go

- 状态：**Accepted**
- 日期：2026-08-01

## 背景（Context）

需要为系统增加 Web 前端与平台业务逻辑（用户、账单、订单、配额等）。需要决定：这些逻辑是写入现有的 controller-go 服务，还是独立成一个新服务。

现有架构事实：

- controller-go 定位为**内部控制面**：实例生命周期状态机、调度、端口映射、Steam 分支同步与游戏缓存对账，通过 gRPC 调用 asset_service / node_agent。
- controller-go 现有 31 条 HTTP 路由**全部零鉴权**，明确标注“仅限内网/调试使用”。
- 开发文档（`game_server_cluster_development_doc.md` 第 6 章）将 **Game API（对外接口层）** 与 **GameServer Controller（内部控制面）** 划分为两个独立模块。
- 仓库已是多服务 monorepo（controller-go / node_agent / asset_service / adapters），服务间通过 gRPC 或内部 HTTP 通信。

## 决策（Decision）

新建独立服务 **platform-service**（Go）承载用户 / 订单 / 账单 / 配额等平台业务，controller-go 保持内部定位。

```text
Web Console（前端，platform-web）
        │  HTTPS + JWT
        ▼
platform-service（用户/订单/账单/配额）
        │  内部 HTTP 调用 controller-go 现有 API
        ▼
controller-go（内部控制面，不对外）
```

- 前端只与 platform-service 通信（BFF 模式），不直连 controller。
- platform-service 通过 controller 现有 HTTP API（如 `POST /api/game-instances`、`GET /api/game-instances/:id`）编排游戏实例。
- controller-go 后续可加内网鉴权或网络隔离（仅允许 platform-service 访问）。

## 理由（Why）

1. **安全边界**：用户接口需要注册/登录/JWT/角色权限，controller 现有接口零鉴权，混入会同时暴露内部调试接口（`/debug/*`、强制调度、删实例）。
2. **领域边界**：账单/订单是商业域（账本、幂等、审计），实例调度是基础设施域，生命周期与关注点不同。
3. **数据边界**：账单需要强一致 + 审计，实例状态允许最终一致；独立 schema/库避免迁移互相牵制。
4. **发布节奏**：账单服务频繁发版、独立扩缩容；controller 状态机长稳、改动谨慎。
5. **既有模式**：仓库已是多服务布局，新增服务符合现有构建/部署约定。

## 后果（Consequences）

### Positive

- 鉴权与安全边界集中在平台层，controller 可保持内网纯净。
- 账单/实例数据与发布节奏互不牵制。

### Negative / 代价

- 多一个服务需要部署、监控、运维。
- 用户操作实例多一跳网络调用（platform-service → controller）。
- 需要维护 controller 对外 API 的稳定性（作为 platform-service 的内部契约）。

### 例外（何时可以临时写入 controller）

仅当用户/账单只是内部测试用的几张表（无真实支付、无多租户、不对外暴露）时，可临时以 `/console/*` 路由前缀放入 controller，但必须保留独立 handler/use case 边界以便将来整体搬出。
