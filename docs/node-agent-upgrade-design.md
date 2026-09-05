# node_agent 一键更新（admin 运维视图）—— 需求设计

> 状态：**Implemented**（P1~P6 已落地，2026-09-02）  日期：2026-09-02
>
> 已确认决策：
> - 部署形态：node_agent 由进程管理器（systemd / docker / supervisor）拉起；agent 只负责下载与自检，**不自杀重启** ✅
> - 二进制来源：平台/对象存储作二进制仓库，节点按需拉取 ✅
> - 更新节奏：admin 手动选节点滚动更新（避开繁忙节点，可回滚）✅
>
> 用户纠偏（2026-09-02）：
> - node_agent **非裸启动**，生产经 `start.sh` 启动（内含 4 条 root 命令：`sudo mkdir -p /server /data` + `sudo chown -R` ×2）；
> - start.sh 以 `exec "$BINARY"` 启动 → **无常驻父进程**，重启必须依赖引入的管理器；
> - sudo 当前**需交互输密码** → 方案收敛为「root 准备上移 systemd ExecStartPre / 一次性部署，运行期与更新期零 sudo 零密码」，见 §3.2。
>
> 实现状态（2026-09-02）：
> - P1 controller `agent_releases` 迁移 + `AgentReleaseUseCase`（LocalReleaseStore 落盘+sha256）+ 上传/列表/下载 API + 单测 ✅
> - P2 proto（心跳 `agent_version` + `UpdateNodeAgent` RPC）+ node_agent `update.rs`（HTTP 拉取→sha256→staging→备份→原子替换→exit 42）+ gRPC handler ✅
> - P3 `NodeAgentUpdateOrchestrator`（前置过滤/状态机/等待心跳回归）+ batch-update/rollback + 单测 ✅
> - P4 platform-service 透传（上传/清单/batch-update/rollback）✅
> - P5 platform-web：版本发布表单 + 版本/更新状态列 + 更新/批量/回滚 + 轮询 ✅
> - P6 部署交付：`start.sh`（去 sudo）+ `deploy/node_agent.service`（ExecStartPre root 准备）+ `deploy/install-node-agent.sh` ✅
> - 编译/单测全绿：cargo check（node_agent）、go build + go test（controller/platform）、vue-tsc（web）
> - 端到端冒烟边界：本机为 Windows 无 systemd，`exit(42) → Restart=always 拉起`完整链路需在 Linux 节点验证（清单见 §9）；本地已验证 agent 拒更新逻辑（同版本/活跃实例）+ controller 前置过滤单测。
>
> 关联：docs/node-agent-logging-design.md（同链路的日志前置工程）/ ADR-0001 / ADR-0005

## 1. 背景与目标

node_agent（Rust，gRPC 50052 + HTTP 50054）目前**没有任何线上更新通道**：新版本只能运维 SSH 上节点手动替换二进制并重启。admin 需要**在界面点几下**完成升级，并知道每台节点的版本与升级状态。

要求：

1. admin 上传/登记新版 node_agent 二进制（含平台架构、sha256）；
2. admin 在 NodeAgent 管理页**勾选节点 → 一键更新**，controller 逐个滚动执行；
3. 更新期间避开有运行实例/在途任务的节点；失败自动回滚；
4. 全程可见：每节点当前版本、目标版本、升级进度/结果；
5. 不引入对既有调度/缓存的破坏性改动。

## 2. 现状盘点

| 组件 | 现有能力 | 与本需求的差距 |
| --- | --- | --- |
| node_agent | 生产二进制由 env 驱动（NODE_AGENT_ADDR/NODE_ID/…），**无版本自述**；gRPC 心跳 GetHeartbeat；HTTP 50054（文件/日志） | 无版本号上报、无更新 RPC、无"下载→替换→退出"逻辑；心跳不携带版本 |
| **start.sh（真实启动入口）** | bash 脚本：设 env → `mkdir -p ./data` → **`sudo mkdir -p /server /data` + `sudo chown -R $USER` ×2（共 4 条 root 命令，当前需交互输 sudo 密码）** → `exec "$BINARY"`（默认 `$SCRIPT_DIR/node_agent`，可 `BINARY` env 覆盖） | `exec` 后无任何常驻父进程；root 目录准备每次启动重复做且依赖交互 sudo——**更新/重启链路必须绕过这一障碍** |
| controller-go | node_agents 表（ID/NodeId/Port/Status/Alive/HealthStatus）；NodeAgentHealthMonitor 探活；NodeAgent 管理 API（enable/disable）；FileSessionIssuer（HS256 签发短效 token 的能力可复用） | 无版本/发布清单概念、无更新指令下发与进度持久化 |
| platform-service | BFF：admin 鉴权 + 代理 controller | 无 release 上传/更新 API |
| platform-web | AdminNodeAgentsView（列表/启停/日志） | 无版本列/上传/更新交互 |
| 对象存储 | node_agent 生产走 S3ObjectStore（快照）；asset_service 连 S3（bucket `cluster` 快照） | 无"系统二进制 release"域 |

**结论**：本需求 = ① 最小「release 仓库 + 版本登记」；② agent 侧「下载→校验→替换→exit(42)」；③ **systemd 接管 start.sh（root 准备上移 ExecStartPre，运行期零 sudo）**；④ controller 侧「滚动编排 + 状态持久化」；⑤ admin UI。每步都可在现有 HTTP/gRPC 能力上扩展。

## 3. 关键架构决策

### 3.1 版本从哪来：发布清单在 controller，二进制存对象存储（推荐）

- **清单（DB）**：controller 新增 `agent_releases` 表：`version`（如 `v0.1.1`）、`os`（linux/windows）、`arch`（amd64/arm64）、`object_key`、`sha256`、`size_bytes`、`note`、`created_at`。
- **二进制（对象存储）**：沿用 node_agent 已连接的 S3（快照同源），桶/前缀 `agent-release/{version}/{os}-{arch}/node-agent`。
- **上传链路**：admin 前端（multipart）→ platform-service（admin 鉴权 + 流式透传）→ controller → 写 S3（controller 增加轻量 S3 client，配置复用 `S3_ENDPOINT/AWS_*`）→ 登记清单 → 回显版本/sha256。
- **拉取链路**：controller 下发更新指令时携带 `{version, object_key, sha256}`；node_agent **用自身已有 ObjectStore 能力直连 S3 拉取**（复用 S3ObjectStore / DirectoryUploadDownloadService 的下载基建），不经 controller 中转 → 大二进制只走「S3 ⇄ 节点」一跳。

> 备选（无 S3 环境/本地冒烟）：release 落 controller 本地目录，agent 经短效签名 URL（复用 FileSessionIssuer HS256 思路）从 controller 拉取。设计按 S3 主线，冒烟可用 controller 本地下载兜底（见 §8）。

### 3.2 谁负责"换 + 重启"：systemd 接管 start.sh（已确认引入）

**现实约束（用户补充，2026-09-02）**：node_agent **不是裸启动**，而是经 `start.sh` 启动；start.sh 内含 4 条 **root 命令**（mkdir/chown `/server`、`/data`），且当前 sudo **需要交互输密码**。因此更新方案必须先解决两个问题：重启时谁重新跑启动逻辑？root 目录准备如何不依赖人工输密码？

**核心解法：把 root 准备从「每次启动的 sudo」收敛为「systemd 部署时的一次性配置」→ 运行期与更新期零 sudo、零密码。**

#### 3.2.1 部署形态（一次性，root 执行）

```text
# repo 提供 deploy/install-node-agent.sh（root 跑一次）：
# 1) 创建运行用户（可选，或用现有 $USER）
# 2) 一次性建全局目录并固定属主：
#      install -d -o <runuser> -g <runuser> /server /data ./data
#    （此后 start.sh 里 mkdir/chown 不再需要 —— 属主已固定）
# 3) 安装 systemd unit（ExecStart 指向 start.sh，见下）
# 4) 如果仍保留 start.sh 里的 sudo 行，则该行只在「脱离 systemd 手动裸跑」时
#    才会执行（开发场景）；生产由 systemd 托管时 unit 里提供等效的 root 准备。
```

#### 3.2.2 systemd unit 模板（repo 提供 `node_agent.service`）

```ini
[Unit]
Description=node agent
After=network-online.target docker.service
Wants=network-online.target

[Service]
# root 段：unit 默认以 root 运行 ExecStartPre —— 不需要 sudo，也不需要密码
ExecStartPre=/usr/bin/install -d -o node-agent -g node-agent /server /data
ExecStartPre=/usr/bin/chown -R node-agent:node-agent /server /data
User=node-agent
WorkingDirectory=/opt/node-agent
ExecStart=/opt/node-agent/start.sh        # 复用现有启动脚本（见 3.2.3 改造）
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

> systemd 的 `ExecStartPre`（root 运行）替代了 start.sh 里的 4 条 sudo 命令——**升级/重启全程由 systemd 用 root 权限执行准备动作，无人机交互**。`start.sh` 自身不再需要 sudo 行（生产入口）。

#### 3.2.3 start.sh 改造

```bash
# 删掉 4 条 sudo mkdir/chown（其职责已上移到 systemd ExecStartPre / 一次性部署）
# 保留：DATA_DIR 归属用户自身，mkdir -p "$DATA_DIR" 无需 root
DATA_DIR="${DATA_DIR:-./data}"
mkdir -p "$DATA_DIR"
export RUST_LOG=info
BINARY="${BINARY:-${SCRIPT_DIR}/node_agent}"
exec "$BINARY"
```

> 兼容：若某环境没有 systemd（本地开发/容器内），仍可裸跑改造后的 start.sh（此时 /server、/data 由部署脚本预建好，或由部署方以 root 先跑一次 install——不进入运行循环）。

#### 3.2.4 node_agent 更新流程（agent 侧，Rust）

```text
1. 收 UpdateNodeAgent{version, object_key, sha256}
2. 校验"可更新"：无 running 实例 / 无在途任务（本地状态自检；controller 也前置校验）
3. 下载到 staging：<数据目录>/agent-update/node-agent.new  （复用 ObjectStore）
4. sha256 校验：不一致 → 失败上报，删除 staging
5. 原子替换：备份当前二进制 node-agent.prev → rename 新二进制到当前路径
   （二进制路径 = 自身可执行文件路径；start.sh 的 BINARY 默认同路径，二者一致）
6. 停止任务 worker / 心跳线程优雅收尾（若干秒宽限）
7. log::info 记录更新 → 进程 exit(42)   ← systemd Restart=always 自动重跑 ExecStart（start.sh → 新二进制）
8. 新进程启动 → GetHeartbeat 携带 version → controller 比对目标版本 → 标记完成
```

> docker/容器形态：unit 与 start.sh 语义等价于「容器退出码 42 + 编排 Restart 策略」；目录准备由镜像构建/init 容器完成。本期以 systemd 为主交付。

### 3.3 滚动编排：controller 状态机 + admin 手动触发

新增 `node_agents.update_state` 字段（枚举）与 `target_version`：

```text
IDLE → UPDATING(下载中/校验中) → REBOOTING(已替换，等心跳回归) → UPDATED / FAILED(带原因) 
                                    ↓ 失败
                                  ROLLBACK 可选：用 .prev 恢复（下一版支持）
```

- **触发**：admin 勾选节点 → `POST /api/admin/node-agents/batch-update {versions:{agent_id: release_id}}`。
- **前置过滤（controller 强制）**：agent Alive 且 HealthStatus≠unhealthy；该 agent 上无 running/starting/stopping 等活跃实例；无在途缓存/任务 → 不满足则跳过并在响应中说明，**不阻塞其他节点**。
- **执行**：并行度默认 1（可配 2）；逐个下发 → 轮询心跳回归（超时 60s）→ 确认 version 达标 → 下一个。
- **可回滚**：本次失败节点保留 `.prev`，UI 提供「回滚到上一版」按钮（重新走下载/替换流程，目标是 controller 记录的上一 release）。
- **进度可见**：controller 存状态 + 进度（下载百分比/阶段），admin 列表轮询展示。

### 3.4 协议：node_agent 增量

proto（node_agent.proto）新增：

```proto
message AgentVersionInfo { string version = 1; string os = 2; string arch = 3; }
message UpdateNodeAgentRequest {
  string version = 1;
  string object_key = 2;   // S3 key
  string sha256 = 3;
  int64  size_bytes = 4;
}
message UpdateNodeAgentResponse { string state = 1; string message = 2; }
rpc GetAgentVersion(AgentVersionInfo) returns (AgentVersionInfo);          // 心跳也可带 version，二选一
rpc UpdateNodeAgent(UpdateNodeAgentRequest) returns (UpdateNodeAgentResponse);
```

- `GetHeartbeat` 响应直接**追加 `agent_version`**（最小改动，controller 无需额外 RPC 轮询版本）。

## 4. 详细设计

### 4.1 node_agent

- 启动时读取自身 version（编译期注入 `env!("CARGO_PKG_VERSION")` + 可选 `--version` 覆盖），心跳携带；
- **更新流程（node_agent）**：
  1. 收 UpdateNodeAgent{version, object_key, sha256}；
  2. 校验"可更新"：无 running 实例 / 无在途任务（本地状态自检；controller 也前置校验）；
  3. 下载到 staging：`<数据目录>/agent-update/node-agent.new`（复用 ObjectStore）；
  4. sha256 校验：不一致 → 失败上报，删除 staging；
  5. 原子替换：备份当前二进制 `node-agent.prev` → rename 新二进制到当前路径；
  6. 停止任务 worker / 心跳线程优雅收尾（若干秒宽限）；
  7. `log::info` 记录更新 → 进程 `exit(42)`（约定码：请求重启；systemd Restart=always 拉起 start.sh → 新二进制）；
  8. 新进程启动 → GetHeartbeat 携带 version → controller 比对目标版本 → 标记完成。
- gRPC handler 注册 `UpdateNodeAgent`，**仅接受"目标版本 ≠ 当前版本"**（防重复）。

### 4.2 controller-go

- 迁移 `000031_agent_releases`（清单表）+ `000032_node_agents_update`（`update_state`/`target_version`/`last_update_at`/`last_update_err`）；
- `AgentReleaseUseCase`：登记（写 S3 + 插清单）、列表、删除（可回收对象）；
- `NodeAgentUpdateOrchestrator`（后台任务，复用 dispatcher 风格）：状态机 + 前置过滤 + 串行/小并行 + 心跳回归等待 + 版本达标确认；失败原因落 `last_update_err`；
- API：
  - `POST /api/admin/node-agents/releases`（multipart，admin；platform 鉴权后转发）
  - `GET /api/admin/node-agents/releases`（清单列表）
  - `POST /api/admin/node-agents/batch-update`（body：`{updates:[{agent_id, release_id}]}`）
  - `POST /api/admin/node-agents/:id/rollback`（可选，本期含）
  - 版本/状态并入现有 `GET /api/admin/node-agents` 返回（前端列表直接展示）。

### 4.3 platform-service

- client 透传 4 个新方法（上传走 multipart 流式转发，其余 JSON）；
- admin_handler 路由 4 条（全部 RequireAdmin）。

### 4.4 platform-web（AdminNodeAgentsView 扩展）

- **列**：`版本`（当前/目标）+ `更新状态`（徽标 + title 展示原因/进度）；
- **「上传新版本」**入口（顶部）：选文件 + 版本号 + 平台/架构 → 上传 → 成功后列表刷新；
- **行「更新」按钮**：确认弹窗提示影响（agent 将重启，短暂失联，无运行实例才可执行）→ 调 batch-update（单选即单节点）；
- **批量**：行首 checkbox + 顶部「批量更新选中」（建议一次 ≤5，滚动执行）；
- 状态轮询 3s；`FAILED` 行出现「回滚」按钮。

## 5. 安全

| 项 | 策略 |
| --- | --- |
| 上传/触发 | 仅 admin（platform RequireAdmin）；controller 内部口仅 platform 可达 |
| 二进制完整性 | sha256 强制校验（controller 登记时计算 + agent 下载后复核）；记录在清单不可篡改（DB） |
| 下载 | S3 内网/HTTPS；agent 更新 RPC 走既有 gRPC 信任面（controller↔agent） |
| 降级/并发 | 目标版本 ≠ 当前才执行；同一 agent 同时只允许一个更新（state 非 IDLE 拒绝） |
| staging | 私有目录 + 校验通过才 rename；失败自动清理 |
| 影响面 | 前置过滤活跃实例；串行滚动；失败保留 .prev 可回滚 |
| **root / sudo** | **运行期与更新期零 sudo**：root 目录准备（/server /data）收敛为 systemd `ExecStartPre`（root 上下文执行）或一次性部署脚本，均无交互密码；start.sh 删除 4 条 sudo 行；agent 自身只替换**自身可写路径**下的二进制，不做任何提权操作 |

## 6. UI 交互稿（示意）

```
┌─ NodeAgent 管理 ──────────────────────────────── [上传新版本 v] ─┐
│ 版本清单（最近）：v0.1.1 linux/amd64 sha256:ab12…  · 上传者 admin │
├──────────────────────────────────────────────────────────────────┤
│ ☑ ID      节点ID  端口  状态   存活      版本          更新状态   │
│ ☑ agent-1  n1     9090 Enabled 存活·14:02 v0.1.0→v0.1.1 待更新    │
│ ☐ agent-2  n2     9090 Enabled 存活·14:03 v0.1.1       已是最新   │
│                [启用] [日志] [更新]                              │
├──────────────────────────────────────────────────────────────────┤
│ 点击[批量更新选中] → 确认弹窗：“将滚动更新 1 个 agent（跳过有运  │
│ 行实例的节点）。每个 agent 会重启并短暂失联，约 10~30s。”[确认]  │
└──────────────────────────────────────────────────────────────────┘
```

## 7. 实施拆分

| 步骤 | 内容 | 涉及 |
| --- | --- | --- |
| P1 | controller：迁移 ×2 + `AgentReleaseUseCase`（S3 写 + 清单 CRUD）+ 上传/列表 API | controller-go（新增 S3 client 依赖） |
| P2 | node_agent：版本上报（心跳 + GetAgentVersion）+ `update.rs`（自检/下载/校验/替换/exit 42）+ proto + gRPC handler | node_agent（proto 重生成走 protoc） |
| P3 | controller：`NodeAgentUpdateOrchestrator` + batch-update/rollback API + 状态字段透出 | controller-go |
| P4 | platform-service：透传 ×4 | platform-service |
| P5 | platform-web：版本列/上传/更新/批量/回滚/轮询 | platform-web |
| P6 | **部署改造**：repo 提供 `node_agent.service`（ExecStartPre root 准备 + User + ExecStart=start.sh）+ `install-node-agent.sh`（一次性 root：建目录/装 unit）+ start.sh 去除 sudo 行；本地冒烟（内存/本地仓库兜底走通 下载→替换→exit 42→重启→版本达标） | docs + node_agent + 全链路 |

## 8. 已知限制 / 二期候选

- **部署改造是硬前置**：本方案要求把 node_agent 纳入 systemd 托管（一次性 root 操作）。**未纳入前**（仍手动裸跑 start.sh），"一键更新"无法闭环——agent exit(42) 后无人拉起。改造清单：装 unit + 建目录 + start.sh 去 sudo；全部由 `install-node-agent.sh` 完成；
- **发布即全量**：本期不做灰度（百分比/节点分组），靠 admin 手动选节点近似灰度；
- **回滚粒度**：回滚到「上一 release」（controller 记录），不做多版本并排常驻；
- **docker 形态**：提供退出码 + Restart 语义说明，容器镜像内替换由部署方适配；
- **config 变更**：若新版需要改 env（端口/地址），本期更新流程不代改配置，由 admin 在变更前预置（文档注明）；更优雅的 config 版本化列二期；
- **Windows 节点**：exit code 语义 + 替换已跑进程受限（Windows 无法覆盖运行中 exe），本期更新能力先覆盖 Linux/systemd（Windows 由其他通道），文档注明；
- **start.sh 作为唯一启动入口的语义**：更新替换的二进制路径必须与 start.sh `BINARY`（默认 `$SCRIPT_DIR/node_agent`）一致；若部署用 `BINARY` env 覆盖到别处，agent 通过「自身可执行文件路径」上报部署路径，controller 无需感知。

## 9. 验证方法

1. 上传 release v0.1.1 → 清单出现、sha256 一致；
2. 节点行显示 当前 v0.1.0 → 目标 v0.1.1；点「更新」→ 状态 UPDATING（下载中）→ REBOOTING（心跳短暂失联）→ UPDATED（版本变 v0.1.1）；agent 日志可见更新记录；
3. 人为制造失败（如 sha256 篡改/下载中断）→ FAILED + 原因可见，staging 被清理，当前版本不变；
4. 有 running 实例的节点点更新 → 被跳过并提示；
5. 更新后回滚按钮 → 回到 v0.1.0；
6. 冒烟用 controller 本地 release 目录（无 S3）跑通主链路；
7. **部署链路验证**：在 systemd 环境跑 `install-node-agent.sh` → `systemctl start node-agent` 无需输 sudo 密码；`start.sh` 不再含 sudo 行；更新触发 exit(42) 后 `Restart=always` 自动重跑 start.sh 拉起新版本，/server /data 属主保持正确。
