# 游戏缓存下载：失败可观测性与磁盘预检 —— 需求设计

> 状态：**Implemented**（P1~P5 已落地，2026-09-05）  日期：2026-09-05
>
> 触发：节点下载失败日志无原因（294420 / 7 Days to Die 专用服务器，`state 0x202` 实为磁盘不足）
>
> 目标：下载失败**看得见（原因进 node_agent 日志与 admin UI）、看得懂（steamcmd 原话透传）、能提前拦（磁盘不足先拒绝，不白跑 steamcmd）**。
>
> 事故根因（已坐实）：294420 下载前 steamcmd 需**预分配 16.40 GB**，节点根分区仅余 11 GB；steamcmd stdout 只报 `state 0x202`，真因只在私有日志 `content_log.txt`（`Failed to preallocate (Not enough disk space)`）。
>
> 实现状态（2026-09-05）：
> - P1 agent 失败日志保真（退出码/耗时/staging+final 双路径；spawn/读行/wait 上下文）✅
> - P2 agent steamcmd stdout/stderr 双流捕获 + `Error!`/`ERROR!` 原话实时转发 + 失败尾部上下文（`DownloadError`+tail）✅
> - P3 agent 下载前磁盘预检（可用空间 vs 历史 size / 首下 `app_info_print` size_on_disk；硬闸 1 GiB + 0.5 GiB 余量）✅
> - P4 `last_error` 全链路：GameCache 落库 + proto 透传 + controller NodeCacheView/观测 + 预热失败原因取真实错误 ✅
> - P5 web 展示：调度观测「失败原因」列 + 分支视图 chip 红字/tooltip ✅
> - 验证：cargo test 14/14 绿（tail 截断/错误行判定/预检判定/size_on_disk 解析/错误摘要）；controller-go go build + test 绿（含 pb.go 重生成，仅 game_cache.pb.go）；vue-tsc 绿。
> - 端到端冒烟边界：Windows 无 steamcmd/`/server`，steamcmd 路径（预检拦截、透传原文）需在 Linux 节点按 §7 清单验证；纯逻辑分支已由单测覆盖。
>
> 关联：docs/node-agent-logging-design.md / docs/cache-placement-design.md（P2-A/B）

## 1. 背景与目标

节点日志只能看到：

```
[ERROR] node_agent::clients::steam_service_client: 下载失败：294420 public 在路径 /server/294420/public/24994542
```

——没有任何原因，admin/开发只能上节点手工排查。本次事故 9 秒秒败、错误码 `state is 0x202`，人工排查最终在 **steamcmd 私有日志**（`~/.local/share/Steam/logs/content_log.txt`）找到真因：`Failed to preallocate (Not enough disk space) "16.40 GB"`——294420（7d2d 专用服务器）更新前需**预分配 16.40 GB**，节点根分区只剩 11 GB。

要求：

1. 下载失败时 node_agent 日志**自带根因**（错误类型、退出码、耗时、staging/正式双路径）；
2. **steamcmd 自身输出**（stdout/stderr，含 `Error! ...` 原话与失败尾部上下文）进入 node-agent.log；
3. **磁盘不足提前拦截**：下载前检查可用空间，明显不足直接拒绝并给出中文原因（不启动 steamcmd 白跑）；
4. 失败原因**落库并透传到 admin UI**；实例「缓存预热失败」的失败原因带上真实错误而非占位符；
5. 不改变缓存放置/调度语义（失败状态仍为 Unavailable，由既有 ISSUE-000004 流程快速失败反馈）。

## 2. 事故复盘（证据链）

| 步骤 | 现象 | 结论 |
| --- | --- | --- |
| 节点日志 | 343050 正常开始下载，294420 开始后 ~9s 失败，仅「下载失败」无原因 | 原因不可见 |
| `systemctl status` + journal | 两任务几乎同时并发 steamcmd；grep steamcmd 只见启动行 | 初疑并发互踩（排除中） |
| 手动复跑 294420 | 单跑仍失败：`Error! App '294420' state is 0x202 after update job`，`df` 余 11G/50G | 排除并发/残留/权限 |
| `+app_info_update 1` 复跑 | 仍 0x202（首次复跑参数顺序有误 `force_install_dir` 在 `login` 后，重跑修正） | 排除 stale app_info |
| content_log.txt | **`update canceled : Failed to preallocate (Not enough disk space) "16.40 GB"` / `preallocated 0 files` / `result Not enough disk space, state 0x202`**（三次复跑同秒同因） | **根因 = 磁盘不足**：steamcmd 下载前按 `size_on_disk` 预分配整包 16.40 GB，余量 11 GB 不足 |
| 343050 成功原因 | content_log：拉取约 2 GB（depot 1006/343052，BuildID 24700372） | 需求小一个数量级，余量够 |

**三处信息黑洞（代码级）**：

1. `steam_service_client.rs` 下载失败分支（原 :99）只打 `game/branch/path`，真正的错误对象（含 `DownloadError` 的退出码）**一个字未记**；
2. steamcmd **stdout 只在匹配 `progress:` 时使用，其余行全部丢弃**；`Error! App ... state is 0x202` 这类原话就在被丢弃的输出里；
3. steamcmd **stderr 未捕获**（默认 inherit，不进 node-agent.log 文件）；steamcmd 更详细的错误只写它自己的 `content_log.txt`（私有路径，平台不可控）。

**教训**：steamcmd stdout 的「最终错误」极简（只有 state 码），真话需要①透传其输出、②在失败前/后结合磁盘上下文解释。

## 3. 现状盘点

| 层 | 位置 | 现状 | 差距 |
| --- | --- | --- | --- |
| node_agent 下载 | `service/steam_service.rs` | `SteamServiceError{ IoError / DownloadError(game,branch,ExitStatus) / RepositoryOperateError / EmptyDownloadPathError }` | 无输出上下文；Display 无 tail |
| | `clients/steam_service_client.rs::download/run_download` | 失败日志丢错误对象；stdout 只解析 progress；stderr 未捕获 | P1/P2 改造点 |
| | `domain/game_cache.rs` | GameCache 无错误字段（SQLite KV-JSON，`#[serde(default)]` 可兼容加字段） | P4 加 `last_error` |
| | `rpc/grpc_server.rs::map_domain_cache_to_proto` | 透传 GameCache（build_id/size_bytes 已加） | +last_error |
| | proto `nodeagent/v1/game_cache.proto` | GameCache 字段 1-9 | +`last_error=10` |
| controller | `biz/node_cache_view.go`（CacheEntry/fetchEntry，30s 快照刷新） | 周期调 node `GetCacheGame` 汇总 status/build/progress/size | +LastError |
| | `biz/observer_use_case.go::CacheOverview/NodeCacheOverview` | admin 观测 JSON | +last_error |
| | `biz/reconcile_dispatcher.go::pollCacheReady` | UNAVAILABLE → 实例失败，reason 为占位符「缓存下载失败（节点缓存不可用）」 | reason 用 last_error 真实原因 |
| | `biz/node_cache_view.go::BranchSizeBytes` | 集群已知分支大小（P2-C 冷节点磁盘约束输入） | 已存在，预检可借用其思路（agent 侧用自己的历史 size） |
| platform-web | `api/observe.ts::NodeCacheItem`；AdminSchedulerObserveView 缓存表；AdminBranchesView 分布 | 状态徽标/大小/进度 | +last_error 展示 |
| 磁盘信息 | node_agent 依赖 sysinfo 0.33（real_system_info.rs 已用 Disks） | heartbeat 只用使用率 | P3 预检取 `/server` 挂载点可用空间 |

## 4. 关键设计

### 4.1 P1 失败日志保真（agent，纯日志）

- `download()` 失败分支补打：`error={:?}`（含退出码）、`elapsed`（秒）、`staging` 与 `final` 双路径、`build_id`；
- `run_download` 的 `spawn()/读行/wait()` 错误处补操作上下文（install_dir、game/branch），不再裸 `?` 上抛；
- 成功路径补一条完成日志（含耗时与最终目录），形成「开始→完成/失败」闭环（可选，P2 一起）。

### 4.2 P2 steamcmd 输出透传 + 失败尾部上下文（agent）

- `run_download` 的 `Command`：`stdout`+`stderr` 均 `piped`，`tokio::select!` 双流逐行消费（两路都读，避免管道缓冲死锁）；
- stdout 行：`progress` 解析不变；**含 `Error!`/`ERROR` 的行立即 `log::error!` 转发**（原话进 node-agent.log）；其余只入尾部缓冲；
- 尾部缓冲：stdout 120 行 / stderr 60 行（环形 VecDeque，行级 O(1)）；
- 非零退出：`DownloadError` 增加第 4 参 `tail: String`（uninstall 传空串），内容 = 两路尾部各 ≤30 行 + 「已截断 N 行」提示；
- `download()` 失败日志即打印完整错误（含 tail）。

### 4.3 P3 下载前磁盘预检（agent）

位置：`download()` 创建 staging 目录前（steamcmd spawn 之前，失败不产生任何下载残留）。

- **可用空间**：`sysinfo::Disks`，选挂载点**前缀匹配 `/server`（GAME_CACHE_SERVER_ROOT_PATH）**的盘取 `available_space()`；无匹配取全部盘最小值并告警；失败（无法获取）→ 跳过预检放行（保守）；
- **需求大小估算（needed）**，三级：
  1. **同 (game,branch) 历史版本参考**：仓库内该 game+branch 全部版本中 `size_bytes>0` 的最大值（更新/重复下载场景几乎总有）→ needed = max_size；
  2. **首下无参考**：steamcmd `+login anonymous +app_info_print {appid} +quit`，正则收集所有 `"size_on_disk" "N"`（字节）求和（≈ 安装所需总量），进程内缓存 12h（`OnceLock<Mutex<HashMap<String,(u64, Instant)>>>`，键 = appid）；查询失败/无输出 → needed 未知；
  3. needed 未知 → 仅硬闸（见下）后放行（维持现状，失败由 last_error 兜底反馈）；
- **判定**：`available < needed + 0.5 GiB 余量` → 预检拒绝，错误文案带两侧数值；硬闸 `available < 1 GiB` 恒拒；
- 文案示例：`磁盘可用空间不足：需约 16.9 GiB（含 0.5 GiB 余量），当前可用 11.0 GiB`。
- 可测性：判定抽纯函数 `fn preflight_reject_reason(available: u64, needed: Option<u64>) -> Option<String>`，单测覆盖四分支。

### 4.4 P4 last_error 全链路（agent → controller → 实例失败原因）

- node_agent `domain::GameCache` + `#[serde(default)] last_error: Option<String>`（SQLite KV-JSON，旧记录反序列化为 None）；
- 下载失败（含预检拒绝）→ 写 last_error（可读中文原因）；成功 Available / 重新下载开始时清空；
- proto `GameCache` + `string last_error = 10`（Rust 侧 build.rs 自动生成）；`map_domain_cache_to_proto` 透传；
- controller：重生成 `game_cache.pb.go`；`CacheEntry` + `LastError`；`fetchEntry` 填 `gc.GetLastError()`；`NodeCacheOverview` + `last_error,omitempty`；
- `pollCacheReady`（reconcile_dispatcher.go）：UNAVAILABLE 分支 `reason = last_error 非空 ? last_error : 原占位符` → 实例 FailReason / 事件流直接展示真实原因（如「磁盘可用空间不足：需约 16.9 GiB…」）；
- 磁盘记账/调度语义不变：`CacheDiskUsageBytes`、`CacheState`、`BranchSizeBytes` 仅读既有字段。

### 4.5 P5 web 展示（admin）

- `observe.ts::NodeCacheItem` + `last_error?: string`；
- `AdminSchedulerObserveView` 缓存表：状态徽标旁，`unavailable`/`downloading` 且 last_error 非空时显示失败原因列/`title` tooltip（全宽列放不下时折叠）；
- `AdminBranchesView` 分支分布 chip：加 `title`（悬停看原因），下载中/不可用 chip 颜色区分不变。

## 5. 数据/接口改动清单

| 改动 | 内容 | 兼容性 |
| --- | --- | --- |
| proto `game_cache.proto` | `GameCache.last_error = 10` | 后向兼容 |
| controller `game_cache.pb.go` | protoc 重生成（仅此文件；node_agent.pb.go 的 CacheGameResponse 结构不变无需重生成） | — |
| Rust GameCache（SQLite JSON） | `#[serde(default)] last_error` | 旧记录 None |
| `SteamServiceError::DownloadError` | +`tail: String` | 构造点 2 处（download/uninstall） |
| CacheEntry / NodeCacheOverview / NodeCacheItem | +`last_error` | JSON 增量 |
| pollCacheReady | UNAVAILABLE reason 取 last_error | 行为增强 |

## 6. 错误文案规范

- node-agent.log ERROR（P1/P2）：`下载失败：game=294420 branch=public build=… final=/server/294420/public/… staging=…/.staging/… elapsed=9s error=DownloadError(…, exit status: …, tail="…")`
- last_error（P4，给 admin/实例失败原因）：单行中文，优先具体化，如：
  - `磁盘可用空间不足：需约 16.9 GiB（含余量），当前可用 11.0 GiB`
  - `steamcmd 退出码 10：App '294420' state is 0x202 after update job（尾部见节点日志）`

## 7. 验证清单

1. cargo check/test（node_agent）：新增纯函数单测（preflight 四分支、size_on_disk 求和解析、尾部缓冲截断）；
2. controller-go：go build + go test ./internal/biz/...；
3. platform-web：vue-tsc；
4. 手工/端到端（Linux 节点，需真实 steamcmd）：
   - 磁盘不足（可 `fallocate` 占满余量）：下发下载 → **不 spawn steamcmd**，秒级失败，node-agent.log 有原因行、admin 缓存视图/实例失败原因可见中文文案；
   - 空间充足：正常下载成功，last_error 清空；
   - 运行中下载失败一次后重试：残留 Unavailable + last_error 展示，重复触发不再刷 steamcmd。

## 8. 边界与后续

- 首下（无历史大小）预检需跑一次 `app_info_print`（~5-10s），结果缓存 12h；查询失败静默放行；
- 预检是**节点自治**的第一道闸；controller 调度侧已有 `BranchSizeBytes`（集群已知大小）冷节点磁盘约束，首下未知大小的调度级磁盘感知留后续（观察 Unavailable+last_error 数据）；
- Windows 本机无 steamcmd/无 `/server`：预检「获取可用空间/steamcmd 查询」路径需 Linux 冒烟，纯逻辑分支由单测覆盖；
- 并发多下载（多 steamcmd 同跑）不在本次修复范围（本次已排除为主因，但串行化仍建议后续）。
