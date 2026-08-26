# 游戏缓存（game_cache）摆放与更新设计

- 状态：设计稿（待评审）
- 范围：controller-go 缓存摆放决策 + node_agent 缓存下载/更新/删除 + 调度集成
- 关联：`controller-go/internal/biz/game_cache_manager.go`、`controller-go/internal/biz/node_cache_view.go`、`node_agent/src/domain/game_cache.rs`、`node_agent/src/clients/steam_service_client.rs`、`docs/scheduler-design.md`、`docs/scheduler-requirements.md`

---

## 1. 背景与现状问题

### 1.1 术语：两套 build_id，必须区分

| 名称 | 来源 | 类型 | 存于 | 语义 |
|---|---|---|---|---|
| Steam `buildid` | Steam appinfo 的 `branch_info.buildid`（`asset_service/src/clients/steam_service_http.rs:163`） | `u64`（单调递增，如 DST `676042`） | `steam_branches.last_build_id`、node 缓存记录 `GameCache.build_id` | 游戏**服务端文件**的版本，steamcmd 下载链专用 |
| `GameBuild.build_id` | asset_service 系统生成 `{game_id}-{channel}-{artifact_image_tag}`（`asset_service/src/service/asset_service.rs:280`） | `string`（如 `dst-public-0.2.2`） | `game_instances.game_build_id` | **运行镜像 / artifact** 的版本，与游戏文件无关 |

两者是两条正交的链，**绝不混用、绝不字符串互相比较**。

### 1.2 现状问题清单

| # | 问题 | 类型 | 位置 |
|---|---|---|---|
| 1 | 缓存主键与磁盘路径只到 `game_id:branch_name`，不含 buildid → 更新即原地覆盖 | 设计取舍（省盘省下载，但后果见 #2/#3） | `node_agent/src/clients/sqlite_repositories.rs:321/368`；`node_agent/src/service/node_agent_service.rs:247-249` |
| 2 | 原地覆盖会破坏正在运行的实例（游戏目录只读挂载进容器，`validate` 中途改写文件） | bug | `node_agent_service.rs:290-293`（注释"无需清理"）；`node_agent_service.rs:445-453`（`mapped_permission:"r"`） |
| 3 | 更新失败不可回滚，旧版本文件一起被毁 | bug | `node_agent/src/clients/steam_service_client.rs:72-73` |
| 4 | 下载产物 build_id 是"声称值"，从不校验（`app_update -beta` 只能拉分支最新，无法钉版本，下载后不比对） | bug | `steam_service_client.rs:81-93` |
| 5 | 无 `RemoveCache` 能力：`SteamService::uninstall` 是死代码，proto 无删除 RPC | 缺失能力 | `node_agent/src/service/steam_service.rs:28`；`steam_service_client.rs:185-200` |
| 6 | 分支 `Disable`/`Abandoned` 后节点缓存永不删除，磁盘只增不减 | 缺失能力 | `game_cache_manager.go:390`（只处理 Enable 分支） |
| 7 | 并发更新竞态 / 可能双下载：更新路径是 read-then-write，非原子 | bug | `node_agent_service.rs:273-294`；后台任务持有 stale 快照 |
| 8 | `download()` 中 path 为空时 `unwrap()` on `None` 会 panic | bug | `steam_service_client.rs:40-42` |
| 9 | `resolveBranch` 把 `LastBuildId`(u64) 与 `GameBuildId`(string) 字符串比较，恒不相等 → 精确匹配分支永远失败 | bug | `controller-go/internal/biz/scheduler_impl.go:162` |
| 10 | 缓存全扇出：`games × enabled 分支 × enabled 节点` 全量下载，节点 100G 盘无法承载多游戏/多分支 | 架构缺陷 | `game_cache_manager.go:379-402` |
| 11 | 调度 H5 只认 `AVAILABLE`、不看 build_id，且无缓存即失败（D2），冷启动不友好 | 架构缺陷 | `scheduler-requirements.md` D2/S10；`constraint.go:96-114` |
| 12 | 无任何磁盘淘汰 / GC / 缓存大小度量 | 缺失能力 | — |

> `Unavailable` 的语义**保留**：系统无法自动处理时置 `Unavailable` 交人工，不自动重试。这是设计本意，本文不改，但补上"人工要有明确下手动作 + 足够诊断信息"。

---

## 2. 设计目标与原则

1. **缓存 = 有界热缓存，不是永久镜像**：稳态每分支每节点最多 1 份，靠引用计数延迟删除回收磁盘。
2. **摆放决策集中在 controller，下载/更新/删除机制在 node**：node 不做自主 LRU（否则集群可能没人兜底某游戏）。
3. **副本数由实例落点"涌现"**，不用常数公式（见 §4.2）。
4. **缓存"有没有"是软偏好，缓存"放不放得下"是硬约束**。
5. **无缓存不失败，走 `cache_warming` 状态**（见 §6）。
6. **范围外**：中心缓存、多版本常驻、实例 pin 具体 buildid、`K` 常数公式（见 §12）。

---

## 3. 节点侧：单槽 + staging + 原子切换（安全更新）

目标：保证分支"最新"，同时更新过程不破坏运行实例、失败可回滚。**不是多版本，是"安全地保证最新"。**

### 3.1 目录布局

```
/server/{game_id}/{branch_name}/
├── current                       # 指针（symlink 或 DB 字段），指向当前对外提供的 buildid
├── {buildid}/                    # 内容目录（不可变），如 676042/
└── .staging/{buildid}/           # 下载中的暂存目录（切换前不对外可见）
```

- 内容目录按 `(game, branch, buildid)` 落盘；主键从 `game_id:branch_name` 改为 `game_id:branch_name:buildid`。
- 运行实例挂载**具体 buildid 路径**（不是 `current` 符号链接，避免 Docker 解析漂移），并把挂载的 buildid 记进实例。

### 3.2 更新流程

```
1. 下载到 .staging/{new_buildid}/
2. 校验产物 == new_buildid：
   - steamcmd app_update -beta {branch} 完成后，读 steamapps/appmanifest_{appid}.acf 的 buildid 字段
   - 用真实 buildid 回填缓存记录（修掉 #4 的"声称值"问题）
3. 成功：rename(.staging/{new_buildid} → {branch}/{new_buildid})（同文件系统原子）
         → 翻转 current 指针 → 记录置 Available
   失败：只删 .staging，旧 current 原封不动 → 置 Unavailable + 原因（#4 保留人工语义）
```

> **磁盘口径（下载双倍占用）**：staging 期间该分支磁盘峰值是 2×（staging 新版本 + 旧 current；切换后旧目录因实例引用延迟删除仍占 1×，见 §8.4）。
> 因此"更新"是需要**暂存空间**的操作：节点无足够剩余空间时 **推迟更新**（reason=insufficient_cache_disk，等 GC / 实例停止腾出空间后再试），**绝不回退到原地覆盖**（§3.4）。

### 3.3 引用计数（refcount）与延迟删除

- refcount 来源：**运行中实例引用该 buildid**（`start` 时 +1，`stop/clean` 时 −1）。
- 一个 buildid 目录可删除的条件：`不是 current 指针 且 refcount == 0`。
- 效果：`current` 可随时前进，老实例仍挂自己启动时的 buildid；等老实例全停，旧目录才被回收。稳态磁盘回到"每分支 1 份 + 更新瞬间 1 份 staging"。

### 3.4 顺带修复的 bug

- `download()` 的 `unwrap()` on `None`（#8）。
- 并发更新：下载任务提交前校验"自己仍是目标版本"，避免 stale 快照回写（#7）。
- 下载失败记录 steamcmd 退出码、目标 buildid、失败 depot（供 Unavailable 人工排查）。

---

## 4. 集群侧：缓存摆放（涌现式副本 + 保底 + GC）

### 4.1 需求：`demand`（派生量，非存储字段）

```
demand(g, b) = COUNT(game_instances
                     WHERE game_id = g AND branch_name = b
                       AND status ∈ {queued, cache_warming, preparing_build,
                                     restoring, starting, running})
```

- **实例本身就是需求记录**，每轮对账现算 COUNT，不存在"加票/减票"的落库动作。
- 前提：实例要**稳定存下解析后的 `branch_name`**（现在只存 `game_build_id`，branch 是调度时临时解析且带 bug）。创建/调度时写一次。

### 4.2 副本数：涌现，不是公式

**不用 `K` / `ceil(demand/K)` 这类常数**——"每节点能装几个实例"本应由调度器资源模型（cpu/mem/disk/带宽 + 每实例 request + 每节点容量）逐维计算，且因游戏而异（ARK 重、Terraria 轻）。

副本数 = **"实际承载该 (game,branch) 实例的去重节点数" + `min_replicas` 保底**，由调度自然产生（见 §5 水位溢出）：

- Terraria 轻 → 一台装 50 个 → 副本数 1；
- ARK 重 → 一台装 2 个就满 → 第 3 个实例落到第二台 → 副本数 2。

### 4.3 保底：`min_replicas`

- 语义：**故障域保底**，管理员设的整数，默认 0，跟资源无关。
- 作用：HA（没实例也常驻 N 份）、防抖动（避免"下完删、删完再下"）。
- 受可用节点数夹取：`实际保底 = min(min_replicas, 可用节点数)`；1 个节点就 1 份。
- 冷启动延迟你已接受 → 冷门游戏默认 `min_replicas=0`，热门游戏手动设 1~2。

### 4.4 placer（对账循环）：只做减法 + 保底加法

复用 `GameCacheManager` 骨架，但把"全扇出"换成收敛：

```
target = 承载该(g,b)实例的节点集合 ∪ min_replicas 保底节点集合
actual = NodeCacheView 快照

for 保底有、actual 无          → CacheGame（预热）
for actual 有、但非目标且非引用 → RemoveCache（GC / 淘汰）
```

**"因需求新增"不归 placer 管**——由实例落到该节点触发（§6）。placer 不再预测扩容。

### 4.5 副本数"超了"怎么办

副本数目标是软性的，actual 可短暂超过（正常，不报错）：

1. 实例未退净：refcount>0 删不掉，等实例停；
2. 收敛滞后：下轮对账即删；
3. 调度竞态：刚触发 `cache_warming`，上轮目标未计入；
4. 跨 region / 故障域强制多留；
5. 人工 pre_warm / 保底。

处理：由 placer **慢慢排掉**——挑"无 refcount、非 current、非保底、最近未用"的删。真正要硬卡的是磁盘预算（见 §8），副本数超目标只要不超盘即无害。

### 4.6 防抖动（hysteresis）

demand 在边界波动会导致"下载→删除→再下载"。收敛策略加阻尼：

- 升副本（新增缓存）：立即执行；
- 降副本（删除缓存）：demand 持续归零 + 冷却时间（如 10 分钟）才删。

---

## 5. 调度集成

### 5.1 缓存从硬约束降为软偏好

- 移除 H5 硬约束（`constraint.go:96-114`），保留 H1 enabled / H2 健康 / H3 资源 / H4 端口。
- 新增评分项 `cache_affinity`：

```
score = w_r·region + w_l·data_locality + w_lb·load_balance + w_c·cache_affinity

cache_affinity(N) = 1   (AVAILABLE 或 DOWNLOADING)
                    0   (冷节点)
```

- **`DOWNLOADING` 也算亲和**：否则同游戏多实例并发到达会落到多个冷节点、触发多次重复下载；算亲和则并发实例聚到正在暖的那台，只下一次。
- `w_c` 默认"强亲和"（大于 region / load_balance），可配置。

### 5.2 水位溢出规则（防实例全堆到一台）

强 `w_c` 会让实例全挤到唯一有缓存的节点，直到塞满 100%。加溢出规则：

> 亲和节点的可用资源低于水位（如剩余 < 该实例 request 的 2 倍，或利用率 > 80%）时，把 `w_c` 压到 0（或给 load_balance 强加成），让调度器**主动溢到冷节点并暖它**。

- 水位以下：缓存亲和主导 → 聚到缓存节点，几乎不产生新下载；
- 水位以上：负载均衡主导 → 溢出到冷节点，副本数自然 +1。
- 阈值用调度器已有资源模型算（非常数 K），对 ARK / Terraria 自动不同。

### 5.3 冷节点磁盘硬约束（唯一不能让步的磁盘项）

缓存"有没有"是软偏好，但"放不放得下"是硬约束：

```
对冷节点候选（会触发 cache_warming）：
    额外检查  size(g,branch) ≤ 可用缓存预算(N)
    其中 可用缓存预算(N) = cache_budget(N) − 已落地缓存 − 下载中 staging − 更新缓冲（§8.4）
    不满足 → 该节点对本实例排除（物理上装不下这份缓存）
```

`AVAILABLE / DOWNLOADING` 节点不需要为缓存再花盘，只走正常 H3（实例自身磁盘）。

退化路径：

| 情况 | 处理 |
|---|---|
| 所有冷节点都放不下 | 不硬失败 → 排队 `reason=insufficient_cache_disk`，触发 placer 驱逐低优先级缓存腾盘后唤醒 |
| 估算低估 → 下载中 ENOSPC | 转 `Unavailable` + 目标/实际 size 对照，人工或 GC 后重试 |
| 缓存本身 > 任何单节点预算 | 结构性失败，上报"该游戏无节点可承载缓存" |

### 5.4 全冷退化

某 (game,branch) 全集群冷节点 → `cache_affinity` 全 0 → 退化为纯资源评分，选一台最合适冷节点 → `cache_warming` 下载 → 后续同类实例因 DOWNLOADING 亲和聚到它。无需特殊分支。

---

## 6. 实例状态机：`cache_warming`

把"下载缓存"做成实例状态机状态（复用 `ReconcileDispatcher` 状态机），而非独立排队机制：

```
pending → scheduling → (选中节点无缓存) → cache_warming → preparing_build → ... → running
                            │
                 node 缓存转 AVAILABLE
```

1. 调度选中节点后，若该节点无该 `(game,branch)` 缓存 → 实例进入 `cache_warming`，dispatcher 发一次 `CacheGame`。
2. 缓存转 `AVAILABLE` → 实例继续 `preparing_build → starting`。
3. 幂等：多实例共享同一次下载（复用 `insert_if_absent`：一个节点一个分支只跑一次下载），实例只是"等它变 AVAILABLE"。
4. 进度对用户可见（"正在准备游戏缓存"），冷启动不失败、不卡死。
5. `cache_warming` 状态本身计入 `demand`（§4.1），无需额外"加一票"。

> 资源类排队仍走现有 `onCacheReady` 唤醒机制（`node_cache_view.go:133-136`），缓存类由状态机直接接管。

---

## 7. `RemoveCache` 语义与触发时机

新增 `RemoveCache` RPC（现在 `uninstall` 是死代码）。node 端删除前过 refcount 检查：有 running 实例引用 → 拒绝或置 `pending-delete`，等 refcount=0 再真正 `rm -rf`。删除幂等、可重复下发。

触发时机（按优先级）：

1. **分支 `Disable` / `Abandoned`**（管理员动作或分支同步发现）→ 该分支全集群下发删除；有引用则先 `pending-delete`。
2. **demand 归零 且 `min_replicas=0`** → 该 (game,branch) 可淘汰，refcount=0 后删（含防抖动冷却）。
3. **actual 超出目标**（§4.5 各场景）→ 删多余副本（LRU / 无引用 / 非保底）。
4. **节点磁盘压力**（心跳 `disk_usage_pct` 超阈值，或该节点缓存总 size 超 `cache_budget`）→ 在该节点按优先级驱逐，回到预算内。
5. **管理员显式删除**某版本 / 某分支缓存。

---

## 8. 磁盘与容量：`size` / `cache_budget` / 预留

### 8.1 缓存大小三来源（按可信度）

1. **实测值（最优）**：node 下载完 `du -sb` 上报，placer 落库为 (g,branch) 已知大小，全集群共用。
2. **depot 估算**：asset_service 已解析 `manifest_gid`（`steam_service_http.rs:152-157`），累加 depot 实际大小估算。
3. **兜底默认**：每游戏配 `cache_size_estimate`（管理员填，ARK 60G / Terraria 0.5G 量级），首次冷启动前的占位，+15% 安全余量。

### 8.2 每节点缓存磁盘预算

```
cache_budget(N) = storage_size − 已预留实例/data − 快照临时 − headroom
```

### 8.3 把缓存磁盘纳入预留

实例落到冷节点时，把 `size(g,branch)` 计入该次调度的磁盘预留（扩展 S8 预留机制），防止并发调度同时认为"还放得下"；下载完成按实测 `size_bytes` 冲正。placer 的磁盘压力驱逐与调度器共用同一容量数据源，不再各算各的。

### 8.4 下载双倍占用（staging 口径）

下载（尤其更新）期间磁盘峰值不是 1× 而是 2×（暂存 + 现存）：

| 场景 | 该分支磁盘峰值 |
|---|---|
| 冷启动下载 | 1×（staging 即最终内容，成功后 rename 落位） |
| 更新下载中 | 2×（staging 新版本 + 旧 current） |
| 切换后 | 2×（新 current + 旧目录孤儿，等 refcount=0 回收）→ 1× |

容量记账必须包含"下载中 staging"项，并在每节点预算中保留**更新缓冲**：

```
reserved_cache(N)  = Σ已落地缓存 size + Σ下载中 staging size
更新缓冲(N)        = max(最大已知单分支 size × 1.5, cache_budget(N) × 15%)   # 可配置
可用缓存预算(N)     = cache_budget(N) − reserved_cache(N) − 更新缓冲(N)
```

后果与对策（**回答"下载占双倍，调度器考虑吗"**）：

- 现状：调度器完全不跟踪缓存磁盘（H3 只算实例 `/data`，NodeCacheView 无 size），**没有考虑**——这是 P2 要补的核心缺口；
- 调度器冷启动放置实例时按 §8.3 预留 `size(g,branch)`（1×），且冷节点检查用 §5.3 的"可用缓存预算"口径（含下载中 staging 与更新缓冲）；
- 控制器触发**更新**时若节点 `可用缓存预算 < 新 size` → **推迟更新**（reason=insufficient_cache_disk），等 GC / 实例停止腾出空间后再试，**不回退原地覆盖**；
- 下载完成按实测 `size_bytes` 冲正预留；失败删 staging 释放空间。

---

## 9. 数据模型与接口改动

| 层 | 改动 |
|---|---|
| proto | `GameCache` 加 `size_bytes`、`refcount`、`last_used_at`；新增 `RemoveCache` RPC |
| node | `RemoveCache` 实现（refcount 校验 + `rm -rf`）；下载后上报 `size_bytes`、回填真实 buildid；staging + 原子切换 + 延迟删除 |
| controller 实体 | `game_instances` 加 `branch_name`（创建/调度时写一次）；`steam_branches` 加 `min_replicas`、`pre_warm`；新增缓存大小已知值存储 |
| controller biz | `GameCacheManager` 从"全扇出"改成"placer diff"；`NodeCacheView` 快照加 size/refcount；调度评分加 `cache_affinity` + 水位溢出 + 冷节点磁盘硬约束 |
| 调度 | 移除 H5 硬约束 → 软偏好；缓存缺失 → `cache_warming` 而非失败；修 `resolveBranch` 的 build_id 比较（#9） |

---

## 10. 分阶段落地

| 阶段 | 内容 | 价值 |
|---|---|---|
| **P1** | 修 `resolveBranch`（#9）、`unwrap` panic（#8）；新增 `RemoveCache` RPC + node refcount/`rm -rf`；分支 Disable/Abandoned 下发删除 | 止血：磁盘不再无限涨，管理员可手动清，修掉已知 bug |
| **P2** | node 上报 `size_bytes`、真实 buildid 回填；staging + 原子切换 + 延迟删除；placer diff + `min_replicas`；调度 `cache_affinity` + 水位溢出 + 冷节点磁盘硬约束 + `cache_warming` 状态 | 核心：从"全扇出"变"按需摆放"，最大化磁盘利用，安全更新 |
| **P3（可选）** | depot content-addressed 硬链接去重（`/server/_objects/{manifest_gid}/`，分支目录内全硬链接） | 磁盘/下载量再降一个数量级（UE 系收益最大） |

> 中心缓存**暂不做**（范围外）。

---

## 11. 配置项与待拍板

| 项 | 建议 | 说明 |
|---|---|---|
| `w_c`（缓存亲和权重） | 默认"强亲和"，可配置 | 极端：`w_c→0` = 纯资源调度；`w_c→极大 + 关溢出` ≈ 现全扇出退化 |
| 溢出水位 | 剩余 < N 个实例 request（推荐）或利用率 > X% | 用调度器资源模型算，非常数 |
| `min_replicas` | 默认 0，热游手动 1~2 | 受节点数夹取；0 = 不保留、接受冷启动 |
| 删除冷却（防抖） | 10 分钟 | 降副本侧生效 |
| 更新缓冲 | max(最大已知单分支 size × 1.5, cache_budget × 15%) | 下载双倍占用的安全垫；防止"更新永远没有空间 staging" |
| 缓存磁盘 `headroom` | 随 S4 安全余量配置 | 与实例调度共用 |

---

## 12. 明确不做（范围外）

1. **中心缓存 / S3 层**：暂不做。
2. **多版本常驻 / 每分支保留 current+previous**：不常驻历史版本；回滚靠"需要时用 depot manifest 精确重下"，用完删。
3. **实例 pin 具体 buildid**：实例跟分支 latest，不 pin；重启沿用实例记录的 buildid 不漂移（启动时把实际挂载 buildid 写回）。
4. **`K` 常数 / `ceil(demand/K)` 副本公式**：被调度器资源模型 + 水位溢出取代。
5. **node 自主 LRU**：淘汰决策统一由 controller 下发，node 不自行删。

---

## 13. 实现状态

| 阶段 | 内容 | 状态 |
|---|---|---|
| P1 | RemoveCache RPC + 分支 Disable/Abandoned 删除 + 修 resolveBranch/panic | ✅ 已实现（commit aa7a3a0、1880c07） |
| P2-A | 路径带 buildid + staging 原子切换 + refcount 延迟删除 + 实例落库 game_id/branch_name/cache_build_id | ✅ 已实现 |
| P2-B | size_bytes 上报 + cache_budget 记账 + 更新缓冲 | ⬜ |
| P2-C | 调度亲和评分 + 水位溢出 + 冷节点磁盘硬约束 + `cache_warming` 状态 | ⬜ |
| P2-D | placer diff + `min_replicas` + GC 触发点 2/3/4 | ⬜ |
| P3 | depot content-addressed 去重 | ⬜ 可选 |

P2-A 落地口径：

- 缓存记录主键 `game:branch:buildid`；"current" = 该分支 buildid 最大的 Available（否则最大 Downloading，再否则任意最高版本）；
- 下载到 `/server/{g}/{b}/.staging/{buildid}`，成功后 rename 原子切换；失败只清 staging（§3.2/§8.4）；
- refcount 仅用于旧目录延迟删除：start +1 / clean −1，孤儿（非 current 且 refcount=0）GC 删除目录+记录；
- 旧数据库记录（key=`game:branch`）升级后不可见 → 该分支触发一次重新下载（可接受的迁移成本）；
- 已知限制：start 失败路径的 refcount 可能残留（依赖后续 remove_cache / 周期 GC 收敛）；refcount 读写非事务（SQLite 单写者下并发窗口极小，观测优先，不阻塞 P2-B）。
