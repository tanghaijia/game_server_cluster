# ADR-0002：platform-service 后端技术栈 Go + Gin + GORM

- 状态：**Accepted**
- 日期：2026-08-01

## 背景（Context）

确定 platform-service 的后端技术栈。开发文档原建议 Rust（axum + sqlx），但现有代码库（controller-go / node_agent / asset_service）全部为 Go，团队实际熟悉 Go。

## 决策（Decision）

platform-service 使用 **Go + Gin + GORM + PostgreSQL**，与 controller-go 保持一致。

- Web 框架：Gin（与 controller-go 相同）。
- ORM：GORM + PostgreSQL（复用 controller-go 的版本化迁移模式 `migrations_runner.go`）。
- 代码分层沿用 controller-go 既有模式：`handler`（HTTP）→ `biz`（用例）→ `repository`（接口）+ `repository/gorm`（实现）→ `entity`。
- 与 controller-go 的通信：HTTP 调用其现有 API；如后续需要更严格的契约，可为 controller 增加 gRPC 或 proto 定义。

## 理由（Why）

- 团队已有 Go + Gin + GORM 的成熟实践与代码样板可复用。
- 与 controller-go 技术栈一致，降低维护成本，可共享部署/配置/日志约定。
- 放弃文档中的 Rust 建议是务实的：选型应跟随团队实际能力与现有代码库。

## 后果（Consequences）

### Positive

- 可复用 controller-go 的分层、迁移、配置模式，开发速度快。
- 一个语言一个生态，运维与 CI 简单。

### Negative / 代价

- 与开发文档的 Rust 愿景不一致（文档需后续更新或在 ADR-0002 中标注为已弃用）。
- 若未来引入强并发/强类型偏好的模块（如高吞吐网关），Go 的取舍需另行评估。
