# Backlog 登记表（跨里程碑/跨文档统一收口）

> 目的：散落在各设计文档（adapter-framework-design.md / subscription-design.md 等）和
> 历次讨论中的待办统一登记，避免遗漏。条目以 **B-<编号>** 引用。
> 状态：⬜ 未开始 / 🔄 进行中 / ✅ 已完成 / ⛔ 已决议不做

## 优先级速览

| 优先级 | 含义 | 条目 |
| --- | --- | --- |
| P1 | 上线/运营底线，不做会出事故 | B-01、B-02 |
| P2 | 产品价值高，主链路稳定后做 | B-03、B-04 |
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

## 已收口（本表后续条目迁移到此）

| 条目 | 状态 | 去向 |
| --- | --- | --- |
| 订阅制 M9–M13（套餐/订阅/单活跃/实例上限） | ✅ | docs/subscription-design.md |
| 磁盘配额（node 盘） | ⛔ | 论证不成立（stopped 数据在 S3）→ 被 B-02 吸收 |
