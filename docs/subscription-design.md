# 订阅制服务器 —— 需求设计（Subscription / Server Plan）

> 状态：**Draft**  日期：2026-08-15
> 关联：multi-game-platform-design.md / adapter-framework-design.md（§3.6 构建与配置）/ 凭证池（M8）

## 1. 背景与目标

当前用户购买模型是「订单 = 游戏 = 实例」三者 1:1 绑定：`orders(game_id)` 下单后
`provisionInstance` 用 `order.GameID` 创建实例（`platform-service/internal/biz/order_use_case.go:115`）。
用户一旦下单，就被锁死在一个游戏上，无法在不重复购买的前提下切换游戏。

目标：把「服务器」抽象为**订阅（subscription）**——用户一次购买一个订阅，在订阅内可创建多个
不同游戏的实例，自行启停切换；平台**只约束一条不变量**：同一订阅同一时间至多一个活跃实例。
管理员通过**套餐（server_plan）**增删配置、时长、价格。

设计核心原则（沿用既有不变量）：

1. **平台保持游戏无关**——游戏特异性只存在于 adapter.toml；
2. **实例生命周期、数据目录、凭证、端口、调度器、状态机全部零改动复用**；
3. **订阅只是归属容器**，与实例解耦，不参与实例状态机；
4. **不引入「切换」操作**——stop/start 仍由用户自己控制，平台只做 start 前置校验。

## 2. 现状盘点

| 项 | 现状 | 差距 |
| --- | --- | --- |
| 购买单元 | `orders`：user_id + game_id + instance_id 1:1 | 无法跨游戏复用 |
| 实例归属 | `game_instances` 无归属字段 | 需加 `subscription_id` |
| 单活跃约束 | 无（实例间互不感知） | 需新增「订阅内至多一个活跃实例」 |
| 计费/时长 | `orders.amount` 仅金额，无到期 | 需 `expires_at` + 到期停服 |
| 套餐/商品 | 无 | 需 `server_plans`（SKU） |
| 每游戏默认配置 | 已有 `game_platform_configs` + schema 驱动表单（M6） | 套餐内引用为 preset |

## 3. 总体设计

### 3.1 已确认决策

- ✅ **抽象名**：用户侧「订阅」，内部实体 `subscription`；管理员侧「套餐」`server_plan`；
  全程**避开「服务器」**一词（`node` 物理节点在 controller 已占用该词义）。
- ✅ **1:N**：一个订阅绑定多个实例（各实例绑定一个游戏，各自独立持久化）。
- ✅ **单活跃约束**：同一订阅同一时间至多一个活跃实例；平台**不提供切换操作**，
  用户自行 stop/start。
- ✅ **DST 一个实例**：Master + Caves 是适配器层多 shard 细节，平台永远只看到一个
  `game_instance`，不进入平台模型。
- ✅ **单活跃兜底选 A**：Postgres 部分唯一索引 + start 前置校验（见 §5）。

### 3.2 架构分层

```text
controller   game_instances.subscription_id  ← 单活跃不变量（部分唯一索引 + start 前置校验）
             （controller 无用户概念，不看人，只看 subscription_id）

platform     server_plans / subscriptions     ← 商品 SKU、归属、到期（auth middleware + expires_at）
             ├─ start 前校验：归属本人 + 未到期
             └─ 到期 sweep：定时停过期订阅的活跃实例
前端         「我的订阅」卡片 + 订阅内实例列表（含当前活跃高亮 + 一键跳转 stop）
             「套餐管理」admin 页（篮子/价格/时长/每游戏默认配置）
```

## 4. 数据模型

### 4.1 server_plans（platform 新表，管理员 SKU）

```sql
CREATE TABLE server_plans (
    id             TEXT PRIMARY KEY,
    display_name   TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    price_cents    BIGINT NOT NULL,            -- 金额：分（整数）
    duration_hours INT NOT NULL,               -- 时长：小时（0 = 永久/手动）
    resource_cpu_milli   INT NOT NULL DEFAULT 0,   -- 上限提示（真实调度仍按实例需求）
    resource_memory_bytes BIGINT NOT NULL DEFAULT 0,
    resource_disk_bytes   BIGINT NOT NULL DEFAULT 0,
    -- 篮子：允许的游戏 + 每游戏默认配置 preset（购买时快照到 subscription）
    basket         JSONB NOT NULL DEFAULT '[]',   -- [{game_id, config:{...}}]
    enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    create_time    TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

`basket` 结构（引用现有 schema 驱动配置，复用 M6 表单）：

```json
[
  { "game_id": "343050", "config": { "cluster_token": "", "world_name": "MyWorld" } },
  { "game_id": "294420", "config": { "map": "Navezgane" } }
]
```

- 每游戏默认配置来自该游戏构建的 config schema（adapter.toml），管理员在套餐页用同一 schema 表单填写；
- 凭证类配置（如 DST `cluster_token`）**不进 preset**，由用户侧凭证池注入（M8），避免真实 token 落入套餐。

### 4.2 subscriptions（platform 新表，用户订阅）

```sql
CREATE TABLE subscriptions (
    id             TEXT PRIMARY KEY,
    user_id        TEXT NOT NULL,
    plan_id        TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'active',   -- active / expired / cancelled / suspended
    expires_at     TIMESTAMPTZ,                      -- NULL = 不过期（或按 plan.duration_hours 计算）
    -- 购买时快照：篮子 + 每游戏默认配置（与 plan 编辑解耦，见 §6）
    basket_snapshot JSONB NOT NULL DEFAULT '[]',
    create_time    TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_subscriptions_user ON subscriptions(user_id);
```

### 4.3 game_instances（controller 增量，零破坏）

```sql
ALTER TABLE game_instances ADD COLUMN subscription_id TEXT;  -- 可空；创建后不可变
```

**单活跃不变量（方案 A）——部分唯一索引**：

```sql
CREATE UNIQUE INDEX uq_subscription_single_active
ON game_instances (subscription_id)
WHERE subscription_id IS NOT NULL
  AND status NOT IN (8, 10);   -- 8=stopped, 10=failed；与 entity 枚举强绑定，见 §5
```

- Postgres 部分唯一索引**天然忽略 NULL**：老实例 `subscription_id IS NULL` 自动豁免，
  无需数据迁移；
- `status NOT IN (8, 10)` 覆盖「pending/scheduling/preparing_build/restoring_snapshot/
  starting/running/stopping/cleaning/queued」全部非终态，杜绝「start A 后、A 尚未 running
  前抢 start B」的窗口。

## 5. 活跃状态语义与防漂移

实例状态机（`controller-go/internal/entity/game_instance.go`）共 11 态，iota 编号：

| 值 | 状态 | 终态? |
| --- | --- | --- |
| 0 | pending | 否 |
| 1 | scheduling | 否 |
| 2 | preparing_build | 否 |
| 3 | restoring_snapshot | 否 |
| 4 | starting | 否 |
| 5 | running | 否 |
| 6 | stopping | 否 |
| 7 | cleaning | 否 |
| 8 | stopped | **是** |
| 9 | queued | 否 |
| 10 | failed | **是** |

「占用槽位」= **一切非终态** = `status NOT IN (8, 10)`。

- 实体层新增 `func (s InstanceStatus) IsActive() bool { return s != StatusStopped && s != Failed }`；
- start 前置校验（controller `game_instance_use_case.go`，现已有
  `if status != Stopped && status != Failed` 的形态）扩展到订阅维度：创建/启动时查本订阅
  是否已有 `IsActive()` 实例，有则拒绝并返回「哪个实例在占用」；
- **防漂移测试**：断言「实体枚举的非终态集合 == 部分唯一索引谓词中的状态集合」，
  将来新增状态（如 `rebooting`）未同步索引时测试即红。

## 6. 套餐编辑 vs 已购订阅（快照语义）

- 购买时把 `server_plans.basket` **快照**进 `subscriptions.basket_snapshot`；
- 管理员后续编辑套餐（价格/时长/篮子）**只影响新订单**，不追溯已购订阅；
- 删除套餐 = `enabled=false` 下架（禁止新购），不删除已购订阅；
- 与现有 `OrderStatusGameRemoved`（游戏下架标记订单）思路一致。

## 7. 到期停服（platform 新增机制）

controller 不认识 `expires_at`，到期强制停服只能由 platform 负责：

1. **start 前置**：platform 校验订阅归属本人 + `expires_at > now`（未到期/永不过期）；
2. **到期 sweep**：platform 后台定时任务（建议 1 分钟）扫描
   `status='active' AND expires_at < now` 的订阅，对其活跃实例调 controller stop，
   订阅标记 `expired`；恢复购买/续费后重新 `active`；
3. 到期不删除实例数据（用户续费后数据仍在）。

伪代码：

```go
func (s *SubscriptionSweeper) Run(ctx context.Context) {
    for _, sub := range repo.ListExpired(ctx) {          // active && expires_at < now
        if inst := s.controller.GetActiveInstance(ctx, sub.ID); inst != nil {
            s.controller.StopGameInstance(ctx, inst.ID)  // 失败记录，下轮重试
        }
        repo.MarkExpired(ctx, sub.ID)
    }
}
```

## 8. 接口增量

### 8.1 platform 管理端（admin）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/admin/plans` | 创建套餐（篮子/价格/时长/默认配置） |
| PUT | `/api/admin/plans/:id` | 编辑套餐（仅影响新购） |
| DELETE | `/api/admin/plans/:id` | 下架（enabled=false） |
| GET | `/api/admin/plans` | 套餐列表 |
| GET | `/api/admin/subscriptions` | 订阅列表（admin 可过滤 user/plan/status） |
| POST | `/api/admin/subscriptions/:id/suspend` | 停用（停活跃实例 + 禁 start） |

### 8.2 platform 用户端

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/me/subscriptions` | 我的订阅（含每订阅活跃实例状态） |
| POST | `/api/me/subscriptions/:id/instances` | 在订阅内创建实例（game_id ∈ basket_snapshot） |
| POST | `/api/me/subscriptions/:id/instances/:instanceId/start` | 启动（前置：未到期 + 本订阅无其他活跃实例） |
| POST | `/api/me/subscriptions/:id/instances/:instanceId/stop` | 停止（用户自控） |

### 8.3 controller 增量

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| — | `game_instances` 表 | 加 `subscription_id` + 部分唯一索引（迁移） |
| POST | `/api/game-instances` | 创建时接受可选 `subscription_id`，落库前校验单活跃 |
| POST | `/api/game-instances/:id/start` | 校验本订阅无其他活跃实例，否则 409 拒绝 |

> 单活跃校验在 controller 内做（DB 唯一索引兜底），归属/到期校验在 platform 内做。
> controller 不引入 user 概念。

## 9. 前端

- **我的订阅**：订阅卡片列表（套餐名、到期倒计时、当前活跃实例高亮）；卡片内实例列表，
  每个实例显示游戏图标 + 状态 + 「启动/停止」按钮；活跃实例高亮并在其他实例的启动按钮上
  提示「先停止当前实例」。
- **创建实例**：订阅内选择游戏（来自 basket_snapshot），配置表单用该游戏 schema 驱动
  （复用 M6），默认值预填 preset。
- **套餐管理（admin）**：篮子多选游戏 + 每游戏 schema 表单填默认配置 + 价格/时长。

## 10. 里程碑

| 里程碑 | 内容 | 状态 |
| --- | --- | --- |
| M9 | `server_plans` + `subscriptions` 表、admin 套餐 CRUD、前端套餐页 | ✅ |
| M10 | `game_instances.subscription_id` + 部分唯一索引 + start 前置校验 + 防漂移测试 | ✅ |
| M11 | 订阅内创建实例 + 单活跃校验闭环 + 并发 race 测试 | ✅ |
| M12 | 到期 sweep + suspend/cancel + 前端「我的订阅」 | ✅ |
| M13 | 实例数量上限（`server_plans.max_instances` + 购买快照 + 创建校验） | ✅ |
| M14 | 兼容/迁移：老订单→单游戏订阅快照（可选） | ⬜ |

## 11. 开放问题 / Backlog

统一登记表见 **docs/backlog.md**（B-01 起）。本设计相关的关键决策备忘：

1. **存储风险在 S3 而非 node 盘**：stopped 实例数据已快照上传对象存储并删除本地目录
   （`node_agent/src/service/node_agent_service.rs::clean_instance`），但快照无保留/GC →
   **B-02（S3 快照保留/GC，P1）**。
2. **实例数量上限已落地（M13）**：套餐 `max_instances`（0=不限），购买时快照进订阅，创建时校验。
3. **同订阅同游戏多实例：允许**（技术上无冲突——凭证按实例分配、端口按实例映射、单活跃约束
   保证同时只跑一个；见 B-10）。是否限制为产品语义决策，如需由套餐配置声明。
4. **续费 MVP 手动**（M12 renew）；自动续期见 B-08（依赖真实支付）。
5. **老订单迁移暂缓**（B-09）：`subscription_id IS NULL` 已豁免，老链路不受影响。

## 12. 风险与对策

| 风险 | 对策 |
| --- | --- |
| 状态枚举与索引谓词漂移 | 防漂移测试（§5）钉死 |
| 并发双 start 抢同一订阅 | 部分唯一索引 DB 兜底 + race 测试 |
| 到期后实例仍在跑 | platform 到期 sweep 兜底 + start 前置 |
| 「服务器」一词歧义 | 全链路统一「订阅/套餐」，避开物理 node |
| 真实 token 进入套餐 preset | 凭证类配置不进 preset，走凭证池注入（M8） |
