# ADR-0004：前后端契约 OpenAPI 自动生成；认证用 JWT

- 状态：**Accepted**
- 日期：2026-08-01

## 背景（Context）

前端（platform-web）与后端（platform-service）接口多且迭代快，手写 DTO 容易对不齐；平台层需要用户认证与权限控制。

## 决策（Decision）

### 1. 前后端契约：OpenAPI 单一来源

- platform-service 使用 swaggo/gin-swagger 从代码注释导出 OpenAPI 3 规范（或在路由层手写维护 spec）。
- 前端用 `openapi-typescript` 从 spec 生成 TS 类型。
- 表单校验的 zod schema 从生成的 TS 类型推导，形成：Go struct → OpenAPI → TS 类型 → zod 的单一契约链。

### 2. 认证：JWT（access + refresh）

- platform-service 签发 JWT；access token 短时（如 30min），refresh token 长时（如 7d）轮换。
- 前端 axios 拦截器统一附加 `Authorization: Bearer <token>`；401 时自动走 refresh 流程，失败跳登录页。
- 路由守卫按角色（admin / user）控制页面与接口权限。

## 理由（Why）

- 契约单一来源：后端改字段，前端编译即报错，杜绝接口漂移。
- JWT 无状态、跨服务友好，platform-service 与 controller-go 之间可用 claims 或内部 token 做服务间信任。

## 后果（Consequences）

### Positive

- 前后端联调成本大幅下降；类型安全贯穿全链路。
- 鉴权集中在平台层，controller 可保持无鉴权内网定位（见 ADR-0001）。

### Negative / 代价

- 需要维护 swag 注释（或 OpenAPI spec）与代码同步；破坏性变更需版本化。
- refresh token 轮换需要存储（如 Redis 或 DB 表），多实例部署时需共享存储。
