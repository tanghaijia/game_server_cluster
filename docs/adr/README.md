# 架构决策记录（ADR）

本目录记录 game_server_cluster 的关键架构决策。每条 ADR 遵循 [Michael Nygard 的 ADR 模式](https://github.com/joelparkerhenderson/architecture-decision-record)：状态 / 背景 / 决策 / 后果。

| 编号 | 决策 | 状态 | 日期 |
| --- | --- | --- | --- |
| [0001](0001-platform-service-boundary.md) | 用户/账单等平台业务独立为新服务（platform-service），不写入 controller-go | Accepted | 2026-08-01 |
| [0002](0002-platform-service-backend-stack.md) | platform-service 后端技术栈：Go + Gin + GORM | Accepted | 2026-08-01 |
| [0003](0003-frontend-stack-vue3-tailwind-shadcn-vue.md) | 前端技术栈：Vue 3 + Vite + TS + Tailwind CSS v4 + shadcn-vue | Accepted | 2026-08-01 |
| [0004](0004-api-contract-and-auth.md) | 前后端契约 OpenAPI 自动生成；认证 JWT | Accepted | 2026-08-01 |
| [0005](0005-deployment-spa-nginx-sse.md) | 部署形态 SPA + Nginx 反代；实时性用 SSE | Accepted | 2026-08-01 |

> 日期为决策讨论发生日；如后续推翻某项决策，将对应 ADR 状态改为 Superseded 并链接到新 ADR。
