# node_agent 日志读取（admin 运维视图）—— 需求设计

> 状态：**Implemented**（P1~P6 已落地，2026-09-02）  日期：2026-09-02
>
> 已确认决策：
> - 读取对象：node_agent 进程自身日志（心跳 / 缓存下载删除 / 任务执行 / 报错），不含游戏容器日志 ✅
> - 数据流路径：控制面授权 + 浏览器直连 node_agent（复用 file-session 通道，新增 scope）✅
> - 能力面：tail（最近 N 行）+ 级别/关键词过滤 + 自动刷新跟随 ✅
>
> 实现状态（2026-09-02）：
> - P1 `logging.rs`（stderr + 滚动文件 node-agent.log，10MB×5）✅
> - P2 `GET /v1/agent/logs/tail`（scope=`agent_logs`，offset 增量 / level / keyword / rotated / 512KB 上限）✅
> - P3 controller `POST /api/node-agents/:id/log-session`（AgentLogSession + 单测）✅
> - P4 platform-service admin 代理 ✅ · P5 web 日志弹层（3s 跟随 / 过滤 / 自动续签）✅
> - P6 本地冒烟：无 token→401、错 scope→401、`lines=3`、`level=error`、offset 增量（330698→381186）均通过 ✅
>
> 关联：docs/file-manager-design.md（同款直连模式已落地）/ ADR-0001 / ADR-0005

## 1. 背景与目标

平台 admin 排障 node_agent 时（缓存下载失败、任务卡住、实例启停异常），需要查看 **node_agent 进程自身日志**，而非登录节点 SSH 去翻日志。

要求：

1. admin 在 Web 页面选择某个 node_agent，能查看其运行日志；
2. 支持 **tail**（最近 N 行）、**级别过滤**（info/warn/error）、**关键词过滤**、**自动刷新**（跟随新日志）；
3. 鉴权：仅 admin；浏览器直连 node_agent 时携带短效 token（同文件会话机制）；
4. 日志读取不得干扰 node_agent 主流程，也不得把大流量打进 controller。

## 2. 现状盘点

| 组件 | 现有能力 | 与本需求的差距 |
| --- | --- | --- |
| node_agent（Rust） | gRPC 50052 控制面；axum HTTP 文件服务 50054（实例文件浏览/读写，Bearer JWT 校验 + 路径防逃逸 + CORS） | **日志只写 stderr（env_logger）+ 零星 println，无落盘文件**；无日志读取端点；现有 token scope=`files` 绑定 instance_id，是实例级 |
| controller-go | 管理 node_agent 表（agent.id / node.id / port）；有 FileSessionIssuer（HS256 共享密钥，scope=`files`）；file_session_handler 已实现「查 agent+node → 算 base_url(node.ip : port+2) → 签发 token」 | 无节点级日志会话签发口 |
| platform-service | BFF：admin 鉴权 + 代理 controller /api/admin/* | 无日志会话代理 |
| platform-web | AdminNodeAgentsView（节点代理管理列表） | 无日志查看 UI |

**关键差距**：node_agent 的日志当前**不落盘**（生产由宿主/容器平台收集 stdout），Web 侧无文件可读。因此本设计的第一前置工程是**让 node_agent 日志滚动落盘**，并统一格式化（时间/级别/目标/消息），供过滤与 tail。

## 3. 方案选型

### 3.1 数据流路径：控制面授权 + 数据面直连（推荐，已确认）

完全复用 docs/file-manager-design.md 已验证的方案 B 形态：

```text
控制面（一次性授权）：
浏览器 ──> platform-service（admin 鉴权）──> controller（查 node_agent→node，签短效 agent 日志会话 token）
                                        返回 { base_url: node_ip:50054, token, expires_at }

数据面（轮询读日志，直连）：
浏览器 ──────────> node_agent HTTP 日志端点（50054，Bearer agent-log token）──> 滚动日志文件
```

- 日志量小（tail 数百行/次），直连一跳即够；controller 只签发 token 不碰日志字节；
- 复用已验证的 axum 服务、CORS、JWT 校验、端口约定（`agent.port + 2 = 50054`）与共享密钥 `NODE_AGENT_FILE_SECRET`；
- 与文件会话共用同一个 HTTP 服务与密钥，仅新增 scope 与路由，无新端口。

### 3.2 日志落盘方案（前置工程）

现状 `env_logger::init()` 只输出 stderr，且无统一格式化。改造：

- 引入**文件日志**（滚动），用 `log4rs`（stdout + file 双 appender，size 滚动，如 10MB × 5）；或最小化自研 `log::Log` 实现写文件 + 保留 stderr。
- 统一格式：`2026-09-02T14:00:00.000Z [INFO] target: message`（供前端按级别/关键词过滤）。
- 日志目录：环境变量 `NODE_AGENT_LOG_DIR`（默认 `./logs`，与 jobs.db 同级约定），文件名 `node-agent.log`（滚动 `node-agent.log.1..4`）。
- 遗留 `println!` 启动 banner 保留 stderr 即可（不进日志文件，属启动噪音）；业务日志（现有 `log::info!/warn!/error!`）自然进入文件。
- 落盘为 **debug 与生产双分支共用**的初始化函数（main.rs 两分支都调用）。

### 3.3 端点能力面：tail + 过滤 + 轮询跟随（已确认）

单端点轮询模型（不做 SSE/WebSocket，前端 3s 定时拉一次即可满足跟随）：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/v1/agent/logs/tail?lines=300&level=warn&keyword=xxx&offset=<bytes>` | 返回日志文本与下一次 offset |

- `lines`：返回最大行数（默认 300，上限 2000）；
- `level`：`info/warn/error`（空 = 全部）；按级别过滤时向后扫描足够多行（如 50k 行）直到凑齐 `lines` 条；
- `keyword`：子串匹配（大小写不敏感），与 level 叠加；
- `offset`：上一响应的游标（字节位置，指向已读到的末尾）；前端轮询带上 offset 只取增量；首查不带 offset = 取尾部 N 行；
- 响应：`{ text: "…多行…", offset: 123456, truncated: bool, rotated: bool }`；
  - `rotated`：日志发生滚动后 offset 失效，前端自动重置为尾部模式；
  - 单次响应体上限（如 512KB）防止日志文件过大拉爆浏览器。
- 附带端点（可选，二期）：列出滚动文件 / 指定文件读取，本期不做。

## 4. 详细设计

### 4.1 node_agent

**a) 日志落盘（新模块 `logging.rs`）**

- 初始化函数 `init_logging(log_dir: PathBuf)`：配置 stdout + 滚动文件双输出；
- 滚动策略：单文件 10MB，保留 5 份（覆盖约 50MB / 节点），按文件大小轮换（对排障足够；按天轮换列为二期可选）；
- 幂等：debug/production 分支均调用；失败降级（日志目录不可写时继续 stderr，不阻断启动）。

**b) 日志 tail 服务（扩展 file_server）**

- FileServer 增加可选字段 `log_dir: Option<PathBuf>`（`NODE_AGENT_LOG_DIR`；未设置 = 不启用日志端点）；
- 新路由：`GET /v1/agent/logs/tail`；
- 鉴权：复用 `verify_token`，但接受新 scope `agent_logs`（见 4.2）；**不绑定实例**（agent 级）——claims 中 `instance_id` 可为空/任意，服务端只验 scope + 签名 + 未过期；
- 安全：tail 只读固定 `log_dir` 内的 `node-agent.log*`，**不接受任意 path 参数**（彻底杜绝路径穿越）；文件用 `OpenOptions` 只读打开；无 symlink 跟随；
- 实现要点：
  - 尾部 N 行：`File::seek` 到末尾，向前按块读（如 64KB 步进）累计到足够换行数或文件头；
  - offset 游标：正向定位字节位置，从 offset 读到 EOF 截断为文本；
  - level/keyword 过滤：逐行 split + 前缀匹配级别 + contains 关键词；
  - rotated 检测：读取前 stat 文件（inode/大小），发现当前文件已非 offset 所在文件（大小 < offset 或文件被替换）→ `rotated: true`；
  - 单次响应 ≤512KB（超额截断，truncated: true）。

### 4.2 controller-go：节点日志会话签发

复用 `FileSessionIssuer`，扩展为通用签发：

- 新增 scope 常量 `agentLogsScope = "agent_logs"`；`fileSessionClaims` 增加可选 `node_agent_id` 字段（与 node_agent `LogTokenClaims` 对齐）；
- 新增方法 `IssueForAgent(nodeAgentID string)`：签发 `{scope:"agent_logs", node_agent_id, exp: now+30min}`；
- 新 handler `POST /api/node-agents/:id/log-session`（`NodeAgentLogSessionHandler`）：
  1. `nodeAgentRepo.GetByID` → 校验存在；
  2. 要求 `node_agent.NodeId != ""` → `nodeRepo.GetByID` 得 node（拿 node.Ip）；
  3. `baseURL = http://{node.Ip}:{agent.Port + NodeAgentFilePortOffset}`（复用文件端口 50054，无需新端口）；
  4. 签发 token → 返回 `{ base_url, token, expires_at }`；
- 路由注册在 controller main（与 file-session 并列）。

### 4.3 platform-service：admin 代理

- controller client 增加 `NodeAgentLogSession(agentID)` → 透传 POST；
- admin handler：`POST /api/admin/node-agents/:id/log-session`（RequireAdmin）→ 转发 controller → 返回 JSON；
- 复用现有 admin 错误映射（404/400/502 透传 controller 错误）。

### 4.4 platform-web：日志查看 UI

位置：`AdminNodeAgentsView.vue` 每行新增「日志」按钮（仅 Enabled 且有 NodeId 的 agent 可点，或均可点、错误提示）：

- 弹层组件（终端风格）：
  - 顶部工具条：agent 名、级别下拉（全部/info/warn/error）、关键词输入（防抖）、自动刷新开关（默认开，3s）、行数（默认 300）；
  - 正文：等宽字体、深色底、按行渲染（error 红 / warn 黄 / 其他灰）；
  - 底部状态：`上次更新 xx:xx:xx · 共 N 行 · 已截断/日志已滚动`；
  - 错误态：token 签发失败（controller 不可达 / agent 无 node）、直连失败（node 不可达）、文件不存在（尚未落盘/目录未配置）分别给可读提示。
- 数据流：
  1. `POST /admin/node-agents/:id/log-session` 拿 `{base_url, token}`；
  2. 直连 `GET {base_url}/v1/agent/logs/tail?...`（header `Authorization: Bearer <token>`，CORS 已允许 `*`）；
  3. 轮询携带 `offset` 取增量；`rotated:true` 或 `truncated` 时重置游标；
  4. token 过期（401）→ 自动重新签发一次后重试，仍失败则提示。
- 过滤在**前端也做一层**（服务端过滤 + 前端高亮），减少服务端扫描成本：轮询时只传 level，keyword 可在本地过滤——但 keyword 会导致增量拼接漏行，故 keyword 非空时服务端过滤 + 返回头重新拉尾部（或接受 offset 语义：服务端过滤下 offset 指"已扫过的字节"，两端一致即可）。**简化决策：level 走服务端、keyword 走服务端（同一语义），前端只渲染**——实现与语义都简单。

### 4.5 鉴权与安全

| 项 | 策略 |
| --- | --- |
| 签发入口 | 仅 admin（platform-service RequireAdmin）；controller 内部口由 platform-service 独占 |
| token | HS256，共享密钥 `NODE_AGENT_FILE_SECRET`（与文件会话同密钥），TTL 30min，scope=`agent_logs` |
| 数据面校验 | node_agent 验签 + scope + exp；agent 级（不绑实例） |
| 路径 | tail 端点不接受 path 参数，只读固定 `log_dir` 下 `node-agent.log*` |
| 防滥用 | 响应 ≤512KB、lines ≤2000；日志读取是只读操作，不触发任何 agent 动作 |
| 隐私 | 日志可能含路径/镜像名等运维信息，仅 admin 可见（页面走 admin 路由） |

## 5. 前端交互稿（示意）

```
┌─ NodeAgent 管理 ─────────────────────────────────────────────┐
│ ID       节点ID   端口  状态   存活        操作              │
│ agent-1   n1      9090 Enabled 存活 ·14:02 [停用] [日志]     │
└──────────────────────────────────────────────────────────────┘
  点击[日志] ─▶ 弹层：
┌─ node-agent-1 日志 ─────────────────────────────────────────┐
│ [全部▾] [关键词______] [行数300▾] [●自动刷新]       14:02:31 │
│ 14:00:12.001 [INFO]  heartbeat ok                            │
│ 14:01:03.204 [INFO]  cache_game start build=99999            │
│ 14:01:05.118 [WARN]  steam download slow 12KB/s              │
│ 14:02:00.900 [ERROR] cache download failed: timeout          │
└──────────────────────────────────────────────────────────────┘
```

## 6. 实施拆分（按依赖序）

| 步骤 | 内容 | 涉及 |
| --- | --- | --- |
| P1 | 日志落盘：`logging.rs`（滚动文件 + stderr，统一格式），main.rs 两分支接入 | node_agent |
| P2 | tail 端点：FileServer 扩展（scope `agent_logs` 校验 + `/v1/agent/logs/tail`，offset/过滤/旋转检测/512KB 上限） | node_agent |
| P3 | controller：issuer 扩展 + `POST /api/node-agents/:id/log-session` | controller-go |
| P4 | platform-service：client 方法 + admin handler 代理 | platform-service |
| P5 | platform-web：AdminNodeAgentsView「日志」弹层（签发→直连轮询→过滤/跟随/错误态） | platform-web |
| P6 | 设计文档收尾 + 手动冒烟（本地起 node_agent debug + controller + platform，验证 tail/过滤/跟随/滚动） | 全链路 |

## 7. 已知限制 / 二期候选

- 只读 **agent 进程日志**；游戏容器内日志（docker logs / 游戏 stdout）不在本期（如需要走 node_agent 经 Docker API 代理，另行设计）；
- 日志按大小滚动不按天；超 50MB/节点的历史会被覆盖（运维侧可自行调大轮转份数）；
- 日志文件仅在 node_agent 本地，无中心聚合；跨节点检索需逐节点打开（本期够用）；
- node_agent 若跑在容器内且 `NODE_AGENT_LOG_DIR` 未挂持久卷，滚动文件随容器生命周期消失（部署文档注明挂载 `logs` 目录）；
- 真实节点需先验证 `agent.port + 2` 文件端口可达（与文件管理同一前提，已落地过）。

## 8. 验证方法

1. 本地 debug 起 node_agent（`NODE_AGENT_LOG_DIR=./logs`）→ 确认 `logs/node-agent.log` 生成且含业务日志；
2. controller 起后 `POST /api/node-agents/:id/log-session` → 返回 base_url/token；
3. curl 直连 `GET {base_url}/v1/agent/logs/tail?lines=100`（带 Bearer）→ 返回最近日志；无 token / 错 scope → 401；
4. 触发一次缓存下载失败，web 弹层 3s 内跟随到 ERROR 行；关键词/级别过滤生效；
5. 日志写满 10MB（或手工调小阈值）→ `rotated` 后前端自动重置游标不中断。
