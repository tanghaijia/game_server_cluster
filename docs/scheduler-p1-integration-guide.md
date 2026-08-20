# 调度器 P1 联调指南

目标：在本地把「创建实例 → 调度（filter/score/预留事务）→ 生命周期」链路跑通，验证 P1 的核心行为：资源感知选点、预留防超卖、健康判定、失败分类、预留释放。

## 1. 组件拓扑（最小集合）

```text
PostgreSQL ──┬── controller-go (Go, :8090 HTTP)
             │        │  gRPC 心跳探测
             └── node_agent (Rust, debug/fake 模式, 127.0.0.1:50052)
```

- **asset_service 可不启动**：调度决策路径（`StatusScheduling`）不调用 asset_service；
  node_agent debug 模式用 `FakeAssetServiceFace`，也不依赖它。controller 的 `GameCacheManager`
  后台循环同步分支会连 asset_service 失败（仅记日志，不影响调度）。
- node_agent **必须用 debug 模式**（`cargo run`）：fake 实现，不需要 Docker/Steam。

## 2. 前置准备

| 项 | 要求 |
|---|---|
| PostgreSQL | 本地起一个，建库（如 `game_server`），默认 `postgres/postgres@localhost:5432` |
| Rust | `cargo run`（node_agent debug 模式，Fake 实现） |
| Go | 本机 go（controller-go 自动跑 migrations） |

## 3. 数据库准备（controller 自动建表 + 手动补一条分支）

controller-go 启动时用 golang-migrate 自动执行 `migrations/`（含 000002 seed：
节点 `1`、`node-agent-1:50052`、`cfg-dst-demo`、游戏 `343050`、端口片段）。

启动 controller 后补一条 steam 分支记录（**H5 判定依赖**，seed 未提供）：

```sql
INSERT INTO steam_branches (id, branch_name, last_build_id, description, game_id, status, create_time, update_time)
VALUES ('343050:public', 'public', 676042, 'demo', '343050', 1, now(), now())
ON CONFLICT (id) DO NOTHING;
```

> `status=1`（Enable）。实例创建时 `game_build_id` 需等于 `last_build_id`（`676042`），
> 调度器才能通过 `resolveBranch` 解析出分支（否则 H5 全部排除，报"无法解析分支"）。

## 4. node_agent 预置 game-cache（H5 硬约束，D2）

node_agent debug 模式的缓存仓库是空的 → `GetCacheGame` 返回缺失 → 调度器 H5 判定
"无 AVAILABLE 缓存" → 全部排除。在 `node_agent/src/main.rs` 的 debug 分支
（`game_cache_repos` 创建后）预置一条 Available 缓存：

```rust
// ~main.rs 第 3 步之后（debug 分支）
let game_cache_repos = Arc::new(InMemoryGameCacheRepository::default());
// 联调预置：模拟节点已有该 (game, branch, build) 的缓存（H5 判定用）
game_cache_repos
    .save(&crate::domain::GameCache {
        game_id: "343050".to_string(),
        branch_name: "public".to_string(),
        build_id: "676042".to_string(),
        status: crate::domain::GameCacheStatus::Available,
        path: None,
        download_progress: None,
        create_time: chrono::Utc::now(),
        update_time: chrono::Utc::now(),
    })
    .await?;
```

## 5. 启动顺序

```powershell
# 1. PostgreSQL（确保 :5432 可连，库 game_server 已建）

# 2. node_agent（fake 模式，:50052）
cd node_agent
$env:RUST_LOG = "info"
cargo run

# 3. controller-go（自动跑 migration + 心跳探测 + 调度）
cd controller-go
$env:DB_HOST="localhost"; $env:DB_PORT="5432"; $env:DB_USER="postgres"; $env:DB_PASSWORD="postgres"; $env:DB_NAME="game_server"
go run .
```

启动后 controller 日志应出现：`数据库迁移完成` → `Scheduler 就绪` → `NodeAgentHealthMonitor 已启动` → `PressureMonitor 已启动` → `StaleReservationReaper 已启动`。

## 6. 验证步骤（按序）

### 6.1 心跳与健康（等 ~10s 跑完一轮）

```sql
SELECT id, alive, health_status, last_heartbeat_at FROM node_agents;
-- 期望: node-agent-1 | t | 1(healthy) | 最近时间

SELECT id, cpu_used_milli, memory_used_bytes, usage_reported_at FROM nodes;
-- 期望: cpu_used_milli=500（fake 心跳 12.5% × 4核×1000）
```

> node_agent 心跳固定 cpu 12.5% / mem 33% / disk 48%（`FakeSystemInfoProvider`），全部低于
> 85% degraded 阈值 → healthy。

### 6.2 调度器状态

```bash
curl http://localhost:8090/api/debug/scheduler
# 期望: type=resource_aware, stats={scheduled:0,...}, queue={queue_len:0,implemented:false}
```

### 6.3 创建 + 启动实例（调度成功路径）

```bash
# 创建（game_build_id 必须 = 分支 last_build_id）
curl -X POST http://localhost:8090/api/game-instances \
  -H 'Content-Type: application/json' \
  -d '{"game_id":"343050","game_build_id":"676042"}'

# 启动 → 进入调度
curl -X POST http://localhost:8090/api/game-instances/{id}/start
```

观察（controller 日志 + DB）：

```sql
SELECT id, status, node_agent_id FROM game_instances;
-- 调度瞬间应到 preparing_build（预留事务内绑定）

SELECT id, cpu_reserved_milli, memory_reserved_bytes FROM nodes;
-- 期望: 扣减了实例 request（config 默认 cpu 1000m / mem 1Gi）

SELECT * FROM game_container_port_mappings;
-- 期望: 2 条映射（cfg-dst-demo 有 2 个 UDP excerpt，各 2 端口 → 4 条？以实际为准）
```

> **预期**：调度成功后 `preparing_build` 阶段会失败——fake node_agent 的 `PrepareGameBuild`
> 拿不到真实 artifact（`resolve_game_build` 返回 fake build 无 artifact）→ 实例转 `Failed`，
> **同时验证 7.2 挂点：预留被释放**（`cpu_reserved_milli` 回落到 0）。这是 P1 故意保留的
> 行为：调度决策正确 + 失败回滚正确，实例真正跑起来需要真实环境（P2/P3 或真实 node_agent）。

### 6.4 资源不足 → 失败（P1 无排队）

连续创建并启动实例，塞满节点（4 核 × 80% = 3200m，默认 request 1000m → 3 个后第 4 个失败）：

```bash
for i in 1 2 3 4; do
  id=$(curl -s -X POST http://localhost:8090/api/game-instances -H 'Content-Type: application/json' \
    -d '{"game_id":"343050","game_build_id":"676042"}' | jq -r .id)
  curl -s -X POST http://localhost:8090/api/game-instances/$id/start > /dev/null
done
```

- 前 3 个：调度成功（`status=preparing_build` 或后续 failed）
- 第 4 个：日志 `[Scheduler] 调度失败 code=5(ResourceShortage) reason=资源/端口不足`，实例 `status=failed`
- controller 日志可看到排除明细（`Excluded` 中"cpu 不足: 缺 Xm"）

### 6.5 区域偏好（可选）

给 nodes 表 `location` 设为 `sg`，创建实例时带 `region` 字段观察评分差异（debug 接口可见）。

## 7. 常见问题排查

| 现象 | 原因 | 处理 |
|---|---|---|
| `node_agents.health_status=0(unknown)` 持续 | node_agent 未起 / 端口不对 / 防火墙 | 确认 `cargo run` 在 50052；`Test-NetConnection 127.0.0.1 -Port 50052` |
| 调度失败 reason=无法解析 game_build 分支 | `steam_branches` 无记录或 build_id 不匹配 | 执行第 3 节 INSERT，创建时用 `676042` |
| 调度失败 reason=无 AVAILABLE 缓存 | node_agent 缓存仓库未预置 | 执行第 4 节补丁后重启 node_agent |
| 所有节点被排除 reason=健康状态不可调度 | 心跳失败达阈值 | 看 node_agent 日志；重启后等一轮探测（10s） |
| `preparing_build` 阶段 Failed | fake node_agent 无真实 artifact（预期行为） | 属正常；验证预留释放即可 |
| 数据库迁移失败 | PostgreSQL 连接/权限 | 检查 `DB_*` 环境变量与库是否创建 |

## 8. 联调后的清理

```sql
-- 重置预留/实例（多次联调后）
UPDATE nodes SET cpu_reserved_milli=0, memory_reserved_bytes=0, disk_reserved_bytes=0;
DELETE FROM game_container_port_mappings;
DELETE FROM game_instances;
```
