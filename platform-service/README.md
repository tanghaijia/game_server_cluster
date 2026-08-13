# platform-service

平台服务：用户、订单、账单等商业域逻辑。独立于 controller-go（内部控制面），通过 controller 的 HTTP API 编排游戏实例。
架构决策见 [ADR-0001](../docs/adr/0001-platform-service-boundary.md)、[ADR-0002](../docs/adr/0002-platform-service-backend-stack.md)。

## 技术栈

- Go 1.26 + Gin（HTTP）+ GORM（PostgreSQL）
- golang-migrate 版本化 SQL 迁移（复用 controller-go 模式）
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

环境变量：`DB_HOST` `DB_PORT` `DB_USER` `DB_PASSWORD` `DB_NAME`（默认 `platform`）`DB_SCHEMA` `HTTP_PORT`（默认 8081）。

## 已实现接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/healthz` | 存活探针 |
| POST | `/api/auth/login` | 登录，签发 JWT（access + refresh） |
| POST | `/api/users` | 注册用户（开放；bcrypt 哈希密码） |
| GET | `/api/users` | 列出用户（需登录） |
| GET | `/api/users/:id` | 查询用户（需登录） |
| POST | `/api/orders` | 创建订单（需登录；amount 单位：分） |
| GET | `/api/orders?user_id=` | 列出订单（需登录，可按用户过滤） |
| GET | `/api/orders/:id` | 查询订单（需登录） |

> 认证：请求头 `Authorization: Bearer <access_token>`（ADR-0004）。登录响应含 `access_token`（默认 30 分钟）与 `refresh_token`（默认 7 天）。

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

## 环境变量补充

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `JWT_SECRET` | `dev-secret-change-me` | JWT 签名密钥（生产必须改） |
| `JWT_ACCESS_TTL_MIN` | `30` | access token 有效期（分钟） |
| `JWT_REFRESH_TTL_HOUR` | `168` | refresh token 有效期（小时） |

## 待办（按 ADR 演进）

- [ ] OpenAPI（swaggo）导出 + 前端类型生成（ADR-0004）
- [ ] refresh token 轮换（当前为无状态 JWT）
- [ ] 订单 → controller 创建/启动实例的编排（调用 controller HTTP API）
- [ ] 账单/支付状态机

## 目录结构

```text
platform-service/
├── main.go                  # 入口：配置/DB/迁移/组装/HTTP
├── migrations_runner.go     # golang-migrate 版本化迁移
├── migrations/              # SQL 迁移文件（up/down）
├── config/                  # 环境变量配置
└── internal/
    ├── entity/              # 领域实体
    ├── repository/          # 仓储接口
    ├── repository/gorm/     # 仓储实现
    ├── biz/                 # 用例层
    └── handler/             # HTTP 层
```
