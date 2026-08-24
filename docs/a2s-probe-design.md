# 游戏运行时探针设计（A2S 通用探针 + 生命周期脚本）

> 状态：**实施完成**（P1-1 脚本后端 ✅ / P1-2 前端展示 ✅ / P1-3 A2S 后端 ✅ 均已实现；P1-4 深度不健康自动重启 ⬜ 可选增强）
> 关联：adapter-framework-design.md（§3 生命周期契约）/ game-adapter-roadmap.md（§5）/ docs/backlog.md（B-04、B-21）
> 范围：健康（healthy）与在线人数（players）的**运行时采集与上报链路**，完成 B-04，并以 A2S 作为多游戏复用的通用后端

## 1. 背景与目标

B-04「health.sh / players.sh 运行时接入」：脚本与契约已存在（`adapters/base/scripts/lifecycle/{health,players}.sh`），
`adapter.toml` 的 `[lifecycle]` 也声明了 `players = "/scripts/players.sh"` / `health = "/scripts/health.sh"`，
node_agent 的 `ContainerClient::exec` 已实现——**唯独没有运行时调用方**。当前：

- 控制器只知道「容器活着」（BackendContainerChecker 每秒查 `Exited → Failed`），
  不知道「游戏进程是否真正可服务」「几个人在线」；
- `NodeHeartbeat`（nodeagent/v1/node_agent.proto:250）只有节点级 cpu/mem/disk/net/running_instances，
  **无 per-instance 玩家数/健康**；`NodeAgentGameInstance`（instance.proto:15）也仅 status/fail_reason。

本设计新增一条**通用运行时探针链路**，回答两个问题：
1. 服务器真的健康吗（不只是容器活着）；
2. 几个人在线（players / max_players）。

目标是**游戏无关**：Source 系游戏走 A2S 通用后端（一次实现多游戏复用），非 Source 游戏（如 DST）
走已有的生命周期脚本后端，两者共用同一条上报与展示链路。

## 2. 总体设计

### 2.1 架构与数据流

```text
┌───────────── 容器 ─────────────┐
│ 游戏进程 (UDP 查询端口)           │  ← A2S 后端：UDP A2S_INFO 查询
│ /scripts/health.sh / players.sh │  ← 脚本后端：docker exec
└────────────────────────────────┘
        ▲
        │ 周期探测（node_agent 后台循环）
┌───────┴───────────────────────────────────────────┐
│ node_agent                                         │
│  RuntimeProbeService（后台 loop，如 15~30s/轮）      │
│   ├─ probe_backend: a2s | script | none（adapter 声明）│
│   ├─ 结果写入内存缓存 map[instance_id]InstanceRuntimeStat │
│   └─ GetHeartbeat handler 读取缓存（不阻塞）          │
└────────────────────────────────────────────────────┘
        │ gRPC GetHeartbeat（已有，扩展 NodeHeartbeat）
┌───────┴───────────────────────────────────────────┐
│ controller-go                                      │
│  NodeAgentHealthMonitor.applyHeartbeat（已有）      │
│   └─ 解析 instance_runtime → RuntimeStatsRegistry（内存）│
│  HTTP /api/game-instances/:id/runtime（新增，读缓存） │
└────────────────────────────────────────────────────┘
        │ HTTP
┌───────┴───────────────────────────────────────────┐
│ platform-service（透传，可选） / platform-web       │
│  实例卡片：在线人数 N/M + 健康徽标（健康/异常/未知）  │
└────────────────────────────────────────────────────┘
```

**关键决策**：
1. **探测在 node_agent，不在 controller**——node_agent 与容器同机，能直连容器 UDP 查询端口；
   controller 只通过 gRPC 拿到结果。
2. **探测异步化，心跳只读缓存**——A2S 查询有 UDP 超时（数百 ms ~ 2s），若在 GetHeartbeat 里同步探测，
   会把探测延迟累加到心跳上；改为后台 loop 周期刷新缓存，`GetHeartbeat` 只做廉价读。
3. **复用 GetHeartbeat 承载 per-instance 指标**，不新增 RPC——controller 已每 10s 拉一次心跳，
   顺势带上每个运行实例的运行时统计。

### 2.2 探测后端

| 后端 | 适用 | 实现 | 数据来源 |
| --- | --- | --- | --- |
| `a2s` | Source 系（Valheim/Palworld/V Rising/7dtd 等） | UDP A2S_INFO 查询 | 查询端口 |
| `script` | 非 Source（DST 等）、已有脚本 | `docker exec` health.sh/players.sh | 容器内脚本 |
| `none` | 无探测能力 | 跳过 | — |

**默认 `script`**：新老适配器零改动即可接入（脚本契约已存在）；`a2s` 由 adapter.toml 显式声明启用。

## 3. A2S 后端

### 3.1 协议要点

A2S = Valve Source Query 协议（UDP）：

- **A2S_INFO**：`\xFF\xFF\xFF\xFF` + `"TSource Engine Query\0"` → 响应含 `players`（当前人数）与
  `max_players`，以及服务器名/map/bot 数等。
- **A2S_PLAYER**：需要 challenge 握手（0x41 challenge 后再发），返回逐个玩家名——**本设计 MVP 不需要**，
  人数直接取 A2S_INFO 的 `players` 字段即可。

**健康判定**：收到合法 A2S_INFO 响应 = `healthy=true`（游戏进程在监听查询端口）；超时/无响应 = `healthy=false`。

### 3.2 查询端口解析

A2S 要查**查询端口**（非游戏端口），不同游戏约定不同（Valheim = 游戏端口 +1，7dtd/部分游戏 = 游戏端口本身）。
由 adapter.toml 声明推导规则：

```toml
[probe]
mode = "a2s"
query_port_offset = 1     # 查询端口 = 游戏宿主端口 + offset；0 = 游戏端口本身
```

node_agent 在 `start_instance` 时已持有该实例的 `port_mapping`（含 `is_game_port` 标记的宿主端口），
据此解析出**查询宿主端口**并记入本地实例记录（见 §6.2），探测循环直接读该记录。

### 3.3 实现选型

- Rust `a2s` crate（或手写 ~50 行 UDP 编解码）——推荐用成熟 crate 避免协议边界 bug；
- tokio UDP，每次探测设超时（如 1s），失败记 `probe_error`，不影响其它实例。

## 4. 脚本后端

复用现有 `exec` 能力 + 已有契约：

- `players.sh` → `{"players": N, "max_players": M}`（`emit_players`）
- `health.sh` → `{"healthy": bool, "reason": "..."}`（`emit_health`）

node_agent 对 Running 实例 `exec` 这两个脚本，解析 JSON 写入缓存。脚本路径取自 `adapter.toml [lifecycle]`。

## 5. adapter.toml 扩展

新增可选 `[probe]` 段（缺省 = `mode = "script"`，保证向后兼容）：

```toml
# 运行时探针（可选；缺省 mode="script" 走 [lifecycle] 脚本）
[probe]
mode = "a2s"             # "a2s" | "script" | "none"
query_port_offset = 1    # 仅 a2s：查询端口相对游戏宿主端口的偏移
# interval_sec 不在此声明，由平台统一控制（见 §9）
```

> 已适配的 7dtd / dst 无需改文件即可获得「在线人数 + 健康」能力（走 script 后端）；
> 未来适配 Valheim/Palworld 时声明 `mode="a2s"` 即可获得更轻量、更准的探测。

## 6. 数据模型 / proto 变更

### 6.1 nodeagent/v1/node_agent.proto

```proto
message NodeHeartbeat {
  string node_id = 1;
  float cpu_usage_pct = 2;
  float memory_usage_pct = 3;
  float disk_usage_pct = 4;
  uint32 running_instances = 5;
  uint64 net_rx_bps = 6;
  uint64 net_tx_bps = 7;
  repeated InstanceRuntimeStat instance_runtime = 8;   // 新增
}

message InstanceRuntimeStat {
  string instance_id = 1;
  uint32 player_count = 2;
  uint32 max_players = 3;
  bool healthy = 4;
  string probe_mode = 5;    // "a2s" | "script" | "none"
  string probe_error = 6;   // 探测失败原因（空=正常）
  string sampled_at = 7;    // RFC3339，最近一次成功/失败探测时间
}
```

### 6.2 node_agent 本地记录

`GameInstance`（本地 SQLite，node_agent 域）新增探测所需字段：
`query_host_port: Option<u16>`（a2s 后端，start 时由 port_mapping 解析）、
`probe_mode`（由 start 下发的 InstanceRuntimeSpec 携带，或从 adapter 元数据解析）。

## 7. 各组件改动

### 7.1 node_agent

- 新增 `RuntimeProbeService`（后台 loop，间隔可配置，默认 20s）：遍历本地 `Running` 实例，
  按 `probe_mode` 执行探测，写内存缓存 `HashMap<InstanceId, InstanceRuntimeStat>`。
- `GetHeartbeat` handler：组装 `NodeHeartbeat` 时从缓存填充 `instance_runtime`。
- `start_instance`：解析并记录 `query_host_port`（a2s）与 `probe_mode`。
- 探测与 BackendContainerChecker 正交：后者管「容器 Exited→Failed」，前者管「进程可服务性/人数」，
  互不替代。

### 7.2 controller-go

- `NodeAgentHealthMonitor.applyHeartbeat`：解析 `instance_runtime`，写入新增的
  `RuntimeStatsRegistry`（内存 `sync.Map[instanceID]InstanceRuntimeStat`，心跳到达即刷新；
  实例已不在 running 列表则清理陈旧条目）。
- 新增 HTTP 端点 `GET /api/game-instances/:id/runtime`（读 registry；实例非 running 返回空/未知）。
- 可选：`GET /api/game-instances` 列表响应附带 `player_count/max_players/healthy` 字段。

### 7.3 platform-service / platform-web

- platform-service：按需透传 `/runtime`（或直接由前端调 controller，与现有实例连接信息一致）。
- platform-web：实例卡片展示「在线 N/M」+ 健康徽标（健康绿 / 异常红 / 未知灰），周期轮询刷新。

## 8. 语义边界（重要）

1. **healthy=false ≠ 立刻杀实例**：探测失败可能是瞬时（重启、加载大世界、UDP 抖动）。
   不并入 B-14 fencing（那基于 gRPC Inspect 失败），也不自动 Failed。仅在**持续 N 轮 unhealthy**
   后作为「深度不健康自动重启」的候选信号（后续增强，见 B-04 可选项）。
2. **healthy 三态**：`healthy / unhealthy / unknown`（尚无数据或探测错误）——前端展示未知态，
   避免把「没数据」误判为「挂了」。
3. **玩家数只读性**：探测不改变实例状态机，不占用调度/凭证/预留；失败只写 `probe_error`。
4. **与 start 就绪判定（B-21）的关系**：A2S 首次 healthy 可作为「游戏真正就绪」的补充信号，
   但**不改动当前 Running 状态机**（B-21 单独排期）。

## 9. 里程碑拆解

| 里程碑 | 内容 | 状态 |
| --- | --- | --- |
| P1-1 | 脚本后端打通：node_agent RuntimeProbeService（script 模式）+ 缓存 + proto 扩展 + controller 消费 + `/runtime` 端点 | ✅ 已实现 |
| P1-2 | 前端展示：实例卡片「在线 N/M」+ 健康徽标 | ✅ 已实现 |
| P1-3 | A2S 后端：a2s crate/手写 UDP + query_port 解析 + adapter.toml `[probe]` 声明 | ✅ 已实现 |
| P1-4 | 深度不健康自动重启（可选，依赖 P1-1 三态与持续 N 轮判定） | ⬜ 可选增强 |

## 11. 实现记录

### P1-1（脚本后端）

- **proto**：`NodeHeartbeat` 新增 `repeated InstanceRuntimeStat instance_runtime = 8`；新增
  `InstanceRuntimeStat` 消息（player_count / max_players / healthy / probe_mode / probe_error / sampled_at）。
  5 个 nodeagent proto 补齐 `option go_package`（与既有 `nodeagentv1` 包一致），Go 绑定以
  protoc-gen-go v1.36.11 重新生成（`controller-go/internal/third/nodeagent/v1/node_agent.pb.go`）。
- **node_agent**：新增 `service/runtime_probe.rs` —— `RuntimeProbeService` 后台循环（默认 20s，
  `RUNTIME_PROBE_INTERVAL_SEC`）对 Running 实例 `docker exec /scripts/health.sh` + `/scripts/players.sh`，
  解析 JSON（health.sh 契约 `{"healthy","reason"}`；players.sh 契约 `{"players","max_players"}`），
  结果写入 `Arc<RwLock<HashMap>>` 缓存；每个 exec 带 5s 超时防挂起；`GetHeartbeat` 只读缓存填充
  `instance_runtime`（探测延迟与心跳解耦）。
- **controller-go**：新增 `biz/runtime_stats_registry.go`（内存缓存，按 node_agent 作用域替换/清理）；
  `NodeAgentHealthMonitor.applyHeartbeat` 解析 `instance_runtime` → 缓存；新增
  `GET /api/game-instances/:id/runtime` 端点（running 且有数据 / running 无数据=unknown / 非 running）。

### P1-2（前端展示）

- **platform-service**：controller client 新增 `GetInstanceRuntime`（透传 `/runtime`）；
  `SubscriptionUseCase.GetInstanceRuntime`（归属校验：订阅归属 + 实例归属，只读不做 checkActive）；
  路由 `GET /api/me/subscriptions/:id/instances/:instanceId/runtime`。
- **platform-web**：`MySubscriptionsView.vue` 新增「在线」列 —— 健康圆点（绿=healthy / 红=unhealthy /
  灰=unknown）+ 「N/M」人数，15s 轮询（`refreshRuntimes`，仅运行中实例）；启停后立即刷新。

### P1-3（A2S 后端）

- **proto**：`InstanceRuntimeSpec` 新增 `optional string probe_mode = 10` /
  `optional uint32 query_host_port = 11`（controller 按游戏容器配置下发）。
- **controller-go**：迁移 000028 给 `game_container_configs` 加 `probe_mode`（默认 script）+
  `query_port_offset`（默认 0）；`buildInstanceRuntimeSpec` 按配置下发探针声明，a2s 模式解析
  查询宿主端口 = 游戏宿主端口 + offset。
- **node_agent**：`GameInstance` 新增 `probe_mode` / `query_host_port`（serde default 兼容旧记录，
  SQLite 为 JSON KV 无需迁移）；`start_instance` 记录探针声明；`RuntimeProbeService` 按 `probe_mode`
  路由（a2s：UDP A2S_INFO 手写解析，健康=收到合法响应；script：exec 脚本；none：跳过；
  未解析端口回退 script）。
- **测试**：A2S_INFO 解析单测 3 例（合法响应 / 垃圾与截断 / 空字符串字段）。

### 通用

- **语义**：healthy=false 不触发 fencing（B-14 正交）、不改状态机；仅展示信号。
- **验证**：`cargo build` ✅ / `cargo test` ✅ / `go build ./...` ✅ / `go vet ./...` ✅ /
  `go test ./...` ✅（controller 与 platform-service 双端）。

## 10. 关联 backlog

| 条目 | 关系 |
| --- | --- |
| B-04（health.sh/players.sh 运行时接入） | **本设计即 B-04 的实现方案**，P1-1/P1-2 完成后可标 ✅ |
| B-21（start 判定 Running 太早） | A2S 首次 healthy 可作就绪补充信号，但独立排期 |
| B-14（节点失联 fencing） | 语义正交：fencing 基于 gRPC Inspect，探针失败不触发 fencing |
