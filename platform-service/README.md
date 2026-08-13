# platform-service

平台服务：用户、订单、账单等商业域逻辑。独立于 controller-go（内部控制面），通过 controller 的 HTTP API 编排游戏实例。
架构决策见 [ADR-0001](../docs/adr/0001-platform-service-boundary.md)、[ADR-0002](../docs/adr/0002-platform-service-backend-stack.md)。

## 技术栈

- Go 1.26 + Gin（HTTP）+ GORM（PostgreSQL）
- golang-migrate 版本化 SQL 迁移（复用 controller-go 模式）
- JWT 认证（ADR-0004）：access（30min）+ refresh（7d），HS256
- 分层：`handler` → `biz` → `repository`（接口）+ `repository/gorm`（实现）→ `entity`

## 快速开始

```bash
# 1. 创建数据库（默认库名 platform）
createdb platform

# 2. 启动（HTTP_PORT 默认 8081）
go run .

# 3. 验证
curl http://127.0.0.1:8081/healthz
```

## 已实现接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/healthz` | 存活探针 |
| POST | `/api/auth/login` | 登录，签发 JWT（access + refresh） |
| POST | `/api/users` | 注册用户（开放；bcrypt 哈希密码） |
| GET | `/api/users/me` | 当前用户信息（登录） |
| GET | `/api/users` | 列出用户（**仅管理员**） |
| GET | `/api/users/:id` | 查询用户（本人或管理员） |
| PATCH | `/api/users/:id/role` | 修改角色（**仅管理员**，不可改自己） |
| PATCH | `/api/users/:id/status` | 启用/禁用（**仅管理员**，不可禁用自己） |
| POST | `/api/orders` | 创建订单（登录；`user_id` 强制取自 token，管理员可指定） |
| GET | `/api/orders?user_id=` | 列出订单（用户只看自己；管理员看全部可过滤） |
| GET | `/api/orders/:id` | 查询订单（本人或管理员） |
| POST | `/api/orders/:id/pay` | 支付（占位）并创建实例（stopped，不开服；本人或管理员） |
| POST | `/api/orders/:id/provision` | 管理员免支付开单并创建实例（stopped；**仅管理员**，订单置 provisioned） |
| POST | `/api/orders/:id/instance/start` | 启动订单关联实例（开服；本人或管理员） |
| POST | `/api/orders/:id/instance/stop` | 停止订单关联实例（停服；本人或管理员） |
| GET | `/api/me/instances` | 我的实例列表（登录；controller 不可达时状态降级为 unknown） |
| GET | `/api/instances` | 全部实例（**仅管理员**） |

> 认证：请求头 `Authorization: Bearer <access_token>`（ADR-0004）。登录响应含 `access_token`（默认 30 分钟）、`refresh_token`（默认 7 天）与 `user`。
> 权限模型：普通用户只能访问自己的数据（数据级隔离）；管理员可访问全部（`RequireAdmin` 中间件）。

### 管理员管理接口（`/api/admin/*`，代理转发 controller-go，仅管理员）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET/POST | `/api/admin/nodes`、`/api/admin/nodes/:id` | 节点列表/新增/查询 |
| GET/POST | `/api/admin/node-agents` | node_agent 列表/新增 |
| POST | `/api/admin/node-agents/:id/enable|`disable` | 启用/停用 node_agent |
| GET/POST | `/api/admin/games` | 游戏列表/新增（写操作同步 asset_service） |
| PUT/DELETE | `/api/admin/games/:id` | 更新/删除游戏 |
| GET | `/api/admin/games/:id/branches` | 列出游戏分支 |
| POST | `/api/admin/games/:id/branches/sync` | 手动同步分支 |
| POST | `/api/admin/games/:id/branches/:branch/cache` | 在指定 node_agent 上触发缓存更新 |

## 管理员播种（方案1）

设置 `ADMIN_USERNAME` / `ADMIN_PASSWORD` 环境变量后启动：若该用户不存在则自动创建为管理员，已存在则跳过（幂等）。启动日志会打印 `管理员已创建/已存在`。

```bash
ADMIN_USERNAME=admin ADMIN_PASSWORD=admin123 go run .
# 登录：POST /api/auth/login {"username":"admin","password":"admin123"}
```

## 快速验证登录

```bash
# 1. 注册
curl -X POST http://127.0.0.1:8081/api/users -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin123"}'

# 2. 登录
curl -X POST http://127.0.0.1:8081/api/auth/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin123"}'
# → {"access_token":"...","refresh_token":"...","user":{...}}

# 3. 带 token 访问受保护接口
curl http://127.0.0.1:8081/api/users -H "Authorization: Bearer <access_token>"
```

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `DB_HOST` / `DB_PORT` | localhost / 5432 | PostgreSQL |
| `DB_USER` / `DB_PASSWORD` / `DB_NAME` | postgres / postgres / platform | 数据库凭据 |
| `HTTP_PORT` | 8081 | HTTP 监听端口 |
| `JWT_SECRET` | dev-secret-change-me | JWT 签名密钥（生产必须改） |
| `JWT_ACCESS_TTL_MIN` | 30 | access token 有效期（分钟） |
| `JWT_REFRESH_TTL_HOUR` | 168 | refresh token 有效期（小时） |
| `CONTROLLER_ADDR` | http://127.0.0.1:8088 | controller-go 地址（订单支付时创建/启动实例） |
| `ADMIN_USERNAME` / `ADMIN_PASSWORD` | 空 | 管理员播种：启动时若该用户不存在则创建为管理员 |

## 待办（按 ADR 演进）

- [x] JWT 认证与登录接口（ADR-0004）
- [x] 角色权限（admin/user）+ 数据级隔离 + 管理员播种
- [x] 订单 → controller 创建/启动实例的编排（`POST /api/orders/:id/pay`）
- [ ] OpenAPI（swaggo）导出 + 前端类型生成（ADR-0004）
- [ ] refresh token 轮换（当前为无状态 JWT）
- [ ] 真实支付渠道 + 账单/退款状态机

## 目录结构

```text
platform-service/
├── main.go                  # 入口：配置/DB/迁移/组装/HTTP
├── migrations_runner.go     # golang-migrate 版本化迁移
├── migrations/              # SQL 迁移文件（up/down）
├── config/                  # 环境变量配置
└── internal/
    ├── auth/                # JWT 令牌管理（签发/校验）
    ├── entity/              # 领域实体
    ├── repository/          # 仓储接口
    ├── repository/gorm/     # 仓储实现
    ├── client/controller/   # controller-go HTTP 客户端
    ├── biz/                 # 用例层
    └── handler/             # HTTP 层 + 中间件
```
