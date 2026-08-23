# Backlog 登记表（跨里程碑/跨文档统一收口）

> 目的：散落在各设计文档（adapter-framework-design.md / subscription-design.md 等）和
> 历次讨论中的待办统一登记，避免遗漏。条目以 **B-<编号>** 引用。
> 状态：⬜ 未开始 / 🔄 进行中 / ✅ 已完成 / ⛔ 已决议不做

## 优先级速览

| 优先级 | 含义 | 条目 |
| --- | --- | --- |
| P0 | 稳定性/可靠性硬伤，会造成全局不可用或永久状态漂移 | B-12、B-13、B-14 |
| P1 | 上线/运营底线，不做会出事故 | B-01、B-02、B-15、B-16、B-17、B-18、B-19、B-20 |
| P2 | 产品价值/韧性，主链路稳定后做 | B-03、B-04、B-21、B-22、B-23、B-24、B-25、B-26 |
| P3 | 体验优化或依赖外部条件 | B-05、B-06、B-07、B-08、B-09 |
| 已决议 | 经论证不做/暂缓 | B-10、B-11 |

---

## B-01 凭证 orphan 自动回收超时（P1）

- **状态**：⬜
- **背景**：凭证池（M8）实例异常删除/controller 重启时，token 可能永久停在 `in_use`，
  池子耗尽后新实例 `ErrCredentialPoolEmpty` 开不了服；目前只能管理员手动 `force-release`。
  池子大小 = 可同时开服的 DST 数量，自动回收是运营底线。
- **方案**：controller 定时 sweep（如 1 分钟）：`in_use` 且分配实例已不存在（或超时未释放）→
  标记 orphan → 回收为 available。仓库已有 `MarkOrphan`/`ForceRelease` 骨架可复用。
- **涉及**：controller-go（credential_pool_repository / credential_use_case + 定时器）
- **参考**：adapter-framework-design.md §3.6.5

## B-02 S3 快照保留 / GC（P1）

- **状态**：⬜
- **背景**：node_agent 每次 FinalStop 生成增量快照上传对象存储并**删除本地数据目录**
  （`node_agent/src/service/node_agent_service.rs::clean_instance`）——stopped 实例不占 node 盘，
  但**快照链路无任何删除/保留机制**：每次启停都累积一份增量快照，S3 容量/成本无限增长。
- **方案**：asset_service 侧快照保留策略（每实例保留最近 N 份）+ 过期清理（S3 删除 +
  快照表记录标记），需处理增量链完整性（父快照被删时子快照合并/失效判断）。
- **涉及**：asset_service（snapshots 表 + S3）、node_agent（清理协调可选）
- **参考**：本会话结论；snapshot 机制见 node_agent `directory_upload_download_service.rs`

## B-03 start 幂等（P2）

- **状态**：⬜
- **背景**：对同一已运行实例重复 start 返回内部话术 `invalid instance status`；应改为
  无操作/友好提示，前端双击不迷惑。M10/M11 已覆盖「另一实例活跃」的 409 冲突，这是最后一块。
- **方案**：`StartGameInstance` 对 `running/starting/pending` 等活跃状态返回成功（no-op）。
- **涉及**：controller-go `game_instance_use_case.go`
- **参考**：docs/subscription-design.md §11

## B-04 health.sh / players.sh 运行时接入（P2）

- **状态**：⬜
- **背景**：health.sh/players.sh 已存在但**无运行时调用方**（仅 ci-test.sh）；start.sh/stop.sh/save.sh
  已接入。接上后可回答「服务器真的健康吗（不只是容器活着）」「几个人在线」。
- **方案**：适配器声明式接入（保持平台游戏无关）：node_agent 周期性 exec 上报玩家数/健康 →
  controller 聚合 → 前端「在线人数」展示；可选：深度不健康自动重启（配合现有失败可见性机制）。
- **涉及**：node_agent（exec 周期）、controller（聚合/状态）、适配器脚本、platform-web
- **参考**：适配器框架文档（脚本运行时契约）

## B-05 config 热更新（P3）

- **状态**：⬜
- **背景**：实例配置目前重启生效（`UpdateInstanceConfig` 注释明确"重启生效，热更新为后续增强"）。
- **方案**：适配器声明「运行时命令」（如 7dtd `setgamepref`），平台保持游戏无关靠声明驱动；
  改配置不重启。
- **涉及**：adapter.toml 声明 + node_agent exec + controller 下发；工作量大、适配器特定。
- **参考**：adapter-framework-design.md（config 链路）

## B-06 config 版本比较 / 回滚（P3）

- **状态**：⬜
- **背景**：`game_platform_configs.version` 已存在但无比较/回滚能力；改错配置无法回溯。
- **方案**：配置版本表 + 版本 diff 展示 + 一键回滚。
- **涉及**：controller-go（game_platform_configs）、platform-web

## B-07 资源预留模型（VPS 式固定配额）（P3，依赖商业决策）

- **状态**：⬜
- **背景**：现为动态调度（按活跃实例实际需求分配），利用率高；套餐 `resource_*` 字段仅提示/上限。
- **方案**：若卖「保证资源」SLA 才需要固定预留（每订阅按篮子最大游戏预留，节点可卖订阅数变少）。
- **涉及**：controller-go 调度器（scheduler-design.md）；**先不做，等真实销售需求**。

## B-08 自动续期（P3，依赖真实支付）

- **状态**：⬜
- **背景**：支付是占位（直接激活），无支付渠道则无「自动扣费续期」。
- **方案**：接入真实支付后，到期自动扣费续期（与 M12 到期 sweep 联动）。
- **涉及**：platform-service（支付）、M12 sweep

## B-09 老订单 → 单游戏订阅迁移（可选）

- **状态**：⬜（用户已明确**暂缓**）
- **背景**：老 `orders`（订单=游戏=实例 1:1）无订阅概念；现有 `subscription_id IS NULL` 已自动豁免，
  老链路不受影响。
- **方案**：需要时提供回填脚本：老订单 → 「单游戏篮子」订阅（快照 plan 语义）。
- **涉及**：platform-service 数据迁移脚本

## B-10 同订阅同游戏多实例限制（已决议：不做）

- **状态**：⛔（经论证无技术必要）
- **背景**：曾误判「两个 DST 会抢 token/端口」——实际凭证按实例分配、端口按实例映射、
  单活跃约束保证同时只跑一个，**无冲突**。是否限制是产品语义决策（一个订阅=每游戏一台），
  如需限制由套餐配置声明。
- **参考**：docs/subscription-design.md §11

## B-11 前端主题派生色 / 暗色变体（P3）

- **状态**：⬜
- **背景**：多游戏平台 MVP 只覆盖 `--primary`（light/dark 共用）。
- **方案**：按游戏派生次要色/暗色变体。
- **参考**：docs/multi-game-platform-design.md §4.4

---

## 稳定性 / 可靠性评审（系统工程师视角）

> 本组条目来自对 controller-go / platform-service / node_agent 三条链路的 reconcile、调度、
> 心跳、凭证、快照、到期 sweep 的逐段评审。结论均落到具体代码。

### B-12 reconcile 单 goroutine + gRPC 无超时 → 单个失联节点冻结全局（P0）

- **状态**：✅（已实现，需重启 controller 生效）
- **实现**：所有同步 RPC（PrepareGameBuild/RestoreSnapshot/Start/Stop/Clean/GetOperation/Inspect、
  asset 的 GetLatestSnapshot/GetGameBuild）经 `withRPCTimeout` 包裹（默认 30s，`RPC_TIMEOUT_SEC`）；
  node_agent gRPC 连接加 keepalive（30s/10s）；超时/Unavailable/Aborted 归为可重试
  （`isRetryableRPCErr`），`GetOperation` 瞬态错误继续轮询不误杀。
- **背景**：`ReconcileDispatcher` 是单消费 goroutine，`Dispatch` 里 `PrepareGameBuild/RestoreSnapshot/
  StartInstance/StopInstance/CleanInstance` 直接透传长生命周期 `ctx`，**没有 `context.WithTimeout`**
  （`reconcile_dispatcher.go` 各分支；`node_agent_face_client.go` 的 `grpc.NewClient` 未配 deadline/keepalive）。
  只有心跳监控用了 `probeTimeout`。一个"TCP 能连但应用层不回包"的失联节点会让该 RPC 永久阻塞，
  消费端只有一条 goroutine → **整条 reconcile 管线冻结**（启停/清理/排队唤醒/到期 sweep 全停）。
- **方案**：`Dispatch` 每个 RPC 包可配置 `context.WithTimeout`（10~30s）+ gRPC keepalive；
  超时后走现有 rich-error 可重试分类。连接被拒的场景当前已能快速返回 Unavailable，缺的是"半开连接"的 deadline。
- **涉及**：controller-go（reconcile_dispatcher.go、client/nodeagent）

### B-13 派发队列满时消费端自入队 → 自死锁（P0）

- **状态**：✅（已实现，需重启 controller 生效）
- **实现**：`RequestDispatch` 非阻塞（`select + default`），队列满时落入 `overflow` 溢出缓冲；
  消费端 `nextDispatchInstance` 优先取 overflow 再取 channel；`NextDispatch` 支持 ctx 取消，
  `Start` 在取消时优雅退出消费 goroutine。
- **背景**：`RequestDispatch` 用 `d.queue <- instance`（缓冲 100，无 `ctx.Done()` 分支）。消费端在处理时
  **自我重新入队**（`StatusPending→Scheduling`、`Scheduling 成功→PreparingBuild`、各成功回调）。
  突发下（`Recover` 批量 + `QueueWaker` 每轮 50 + 大量 `PollingResult` 回调 + 用户操作）队列打满，
  消费端 `<-` 出 1 条却要再塞 1 条 → **唯一消费者被自己塞死，无人 drain**。
- **方案**：入队改非阻塞 `select`（`ctx.Done()` + 满则落库兜底/记日志），或把"自我推进"与外部入队解耦。
- **涉及**：controller-go（reconcile_dispatcher.go）

### B-14 节点失联 fencing / Uncertain（S26）未实现 → 运行实例永久 Running（P0）

- **状态**：✅（已实现，需重启 controller 生效；`NODE_OFFLINE_FENCE_MIN` 默认 3 分钟）
- **实现**：`WatchRunningInstances` 中 Inspect 失败且健康监控已判失联（`Alive=false` 或 `unhealthy`）
  时累计失联计时（`offlineSince`），持续超过阈值 → 置 Failed（走 `FailedInstance` 完整路径：
  释放订阅槽位/凭证/预留）；瞬态不可达（健康未判失联）不累计，恢复可达即清零。
  备注：fencing 置失败后端口映射保留（防同端口碰撞），回收归属 B-16。
- **背景**：`node_agent_health_monitor.go` 只把节点标 `unhealthy` 并让调度器过滤；`WatchRunningInstances`
  （`reconcile_dispatcher.go:1002`）在 `InspectInstance` 失败（节点不可达）时 `continue`，**不置失败**。
  代码注释写"节点失联优先走 Uncertain/fencing（S26）"，但 S26 无任何实现。节点永久宕机 → 其上 Running 实例
  永远 Running，**一直占用预留、凭证、端口、订阅"单活跃"槽位**，用户既开不了新服、资源也不回收。
- **方案**：补 S26 —— 节点 `unhealthy` 超过阈值后把其上 Running 实例置 Uncertain/Failed 并释放槽位；
  节点恢复后与 node_agent 对账真实状态。
- **涉及**：controller-go（health monitor + dispatcher + 状态机）

### B-15 轮询超时无对账 → 容器孤儿（P1）

- **状态**：⬜
- **背景**：`PollingResult` 3 分钟超时（`OPERATION_POLLING_MINITE=3`）→ `FailedInstance` 释放凭证/预留并
  清空绑定，但 node_agent 上 Start/Stop 可能**仍在跑或已成功**。controller 记 Failed（无绑定），node_agent
  却有真容器在跑、占端口/资源，**无清理路径**（Stop 只接受 Running/Failed，Failed 又无 NodeAgentID → 清不掉）。
- **方案**：轮询超时前先 `InspectInstance` 拿真实状态再决定置 Failed；加周期对账
  （controller 实例 vs node_agent `GetInstances`）回收孤儿容器。
- **涉及**：controller-go（reconcile_dispatcher.go）、node_agent

### B-16 多步落库非原子 → 崩溃窗口漏端口/预留（P1）

- **状态**：⬜
- **背景**：`onCleanInstanceSucceeded` 顺序为 `释放预留 → Save(Stopped,清绑定) → ReleaseMapPort`，凭证/预留/
  端口是三次独立 DB 写。在 `Save` 之后、`ReleaseMapPort` 之前崩溃 → 端口映射永久泄漏（实例已终态 Stopped，
  `Recover` 只重入队中间态，**无终态泄漏兜底扫描**）。凭证/预留同理。
- **方案**：端口释放并入预留事务（释放与 `TryReserve` 镜像）；或加启动时"终态实例仍有端口/预留 → 回收"对账。
- **涉及**：controller-go（reservation_repo、container_port_mapping_repo、reconcile_dispatcher.go）

### B-17 优雅退出把在途操作误标 Failed（P1）

- **状态**：⬜
- **背景**：`SIGTERM → cancel()` → 在途 `PollingResult` 的 `GetOperation(ctx)` 报错 → `FailedInstance` 置终态。
  重启后 `Recover` 不拾取 Failed → 本可能成功的启动/停止被永久打成失败。无 graceful drain。
- **方案**：区分 `codes.Canceled/DeadlineExceeded` 与真实业务错误；停机停止消费、给在途 Polling 收尾窗口，
  Canceled 时**重新入队**而非置 Failed。
- **涉及**：controller-go（main.go 停机、reconcile_dispatcher.go）

### B-18 卡死哨兵误杀"慢但仍在推进"的启动（P1）

- **状态**：⬜
- **背景**：`StaleReservationReaper` 用 `inst.UpdateTime` 判卡死（默认 10min），但活跃操作轮询期间
  **不更新 UpdateTime**。合法的大快照恢复（>10min）会被当 stale 杀掉。
- **方案**：改"最后进展时间"（轮询到 `OperationStatus_Running` 刷新心跳）；或 reap 前先查 node_agent 的 operation 状态。
- **涉及**：controller-go（stale_reservation_reaper.go、reconcile_dispatcher.go）

### B-19 哨兵置 Failed 绕过 `FailedInstance` → 漏凭证/端口（P1）

- **状态**：⬜
- **背景**：`stale_reservation_reaper.go:85` 直接 `Save(Failed)`，只释放预留，**不释放凭证、不释放端口映射**。
  卡在 Starting 且已分配凭证的实例被 reap 后，凭证永久 `in_use`（B-01 之外又一条具体泄漏路径）、端口也泄漏。
- **方案**：reap 改走 `FailedInstance`，或显式补 `ReleaseByInstance` + 端口释放。
- **涉及**：controller-go（stale_reservation_reaper.go）

### B-20 到期/停用/取消 sweep 被单个中间态实例阻塞（P1）

- **状态**：⬜
- **背景**：`subscription_use_case.go::stopActiveInstances` 对每个非 stopped/failed 实例都调 `Stop`，
  但 controller 的 `StopGameInstance` 只接受 Running/Failed。订阅里有一个 queued/starting/stopping/cleaning 实例
  就报错 → `firstErr` → 订阅不被标记 expired/suspended/cancelled。需等自然收敛（排队最长 30min），
  结合 B-12 还可能永久卡住。
- **方案**：中间态实例跳过（留给下一轮 sweep 收敛）、不要阻塞状态迁移；queued 直接 cancel 而非 stop。
- **涉及**：platform-service（subscription_use_case.go）

### B-21 start 判定 Running 太早（假 Running 窗口）（P2）

- **状态**：⬜
- **背景**：`node_agent_service.rs:538` 在 `create_container` 后即置 Running，未等游戏进程就绪，崩溃靠
  `backend_container_checker` 事后兜底，存在"controller 已 Running 但游戏未就绪"窗口。
- **方案**：与 B-04（health.sh 探活）联动，就绪后再置 Running。
- **涉及**：node_agent、controller-go

### B-22 崩溃重入 start 非真正幂等（P2）

- **状态**：⬜
- **背景**：node_agent `enqueue_start_instance` 的 `find_active` 只去重"并发入队"，不防"崩溃后重跑
  `start_instance`"；重跑可能重复 `create_container`。与 B-03（HTTP 层友好 no-op）是不同层级。
- **方案**：`start_instance` 按 instance_id 幂等（容器已存在则复用/返回既有 runtime）。
- **涉及**：node_agent（node_agent_service.rs、task_service.rs）

### B-23 node_agent worker 无监督，panic 后静默停摆（P2）

- **状态**：⬜
- **背景**：`task_service.rs` 每个 worker `.expect("... worker crashed")`，单个 worker panic 后该 job 类型
  静默停止处理，进程不退出也无重启。
- **方案**：worker 监督重启，或改为 fail-fast（panic 即进程退出，由外部拉起）。
- **涉及**：node_agent（task_service.rs）

### B-24 HTTP server 无 read/write/idle 超时 + 无指标（P2）

- **状态**：⬜
- **背景**：controller 与 platform 都是 `http.Server{Addr,Handler}` 零超时，慢连接可耗尽连接数；
  观测仅 slog + scheduler_stats，无结构化指标/告警入口。
- **方案**：`http.Server` 配 read/write/idle timeout；加基础 metrics 端点（Prometheus 或 json）。
- **涉及**：controller-go、platform-service（main.go）

### B-25 CreateInstance max_instances check-then-create（TOCTOU）（P2）

- **状态**：⬜
- **背景**：`subscription_use_case.go::CreateInstance` 先 count 再 create，两次调用非原子，并发可能瞬时超限
  （软配额，影响小）。
- **方案**：必要时在 controller 侧用订阅计数约束/事务化；当前接受为软上限。
- **涉及**：platform-service、controller-go

### B-26 DB 无显式连接池 / 抖动重试（P2）

- **状态**：⬜
- **背景**：gorm 默认连接池，瞬时 DB 抖动时部分写操作直接失败（部分被 reconcile 重试兜住，部分没有）。
- **方案**：显式配置 pool 大小/超时；对幂等写加轻量重试。
- **涉及**：controller-go、platform-service（main.go gorm 初始化）

---

## 已收口（本表后续条目迁移到此）

| 条目 | 状态 | 去向 |
| --- | --- | --- |
| 订阅制 M9–M13（套餐/订阅/单活跃/实例上限） | ✅ | docs/subscription-design.md |
| 磁盘配额（node 盘） | ⛔ | 论证不成立（stopped 数据在 S3）→ 被 B-02 吸收 |
