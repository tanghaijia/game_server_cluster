# 实例数据目录文件管理 —— 需求设计

> 状态：**Approved**（决策已确认）  日期：2026-08-14
>
> 已确认决策：
> - 方案 B（控制面授权 + 数据面直连）✅（浏览器可达 node_agent）
> - 在线编辑：仅文本文件 ≤2MB ✅
> - 其余决策点取推荐值（新端口 50054、无状态 JWT、单文件上限 10GB）
> 关联架构：ADR-0001（platform-service BFF）/ ADR-0005（部署形态）

## 1. 背景与目标

用户购买实例并运行后，需要管理实例的持久化数据目录（`/data`）：

- **上传**：把配置、模组、地图等文件放进实例的 data 目录；
- **删除**：删除不需要的文件；
- **编辑**：直接修改文本类文件（配置文件、白名单、地图配置等）。

要求：

1. **图形化界面**，普通用户可操作（不需要 SSH/进入容器）；
2. **尽可能减少转发/流量消耗**：大文件上传下载不应经过多个中间服务；
3. 权限：用户只能操作**自己订单关联的实例**的 data 目录；管理员可操作全部。

## 2. 现状盘点（相关组件能力）

| 组件 | 现有能力 | 与本需求的差距 |
| --- | --- | --- |
| node_agent | 仅 gRPC（端口 50052）：Start/Stop/Snapshot/Prepare/Cache 等；`DirectoryUploadDownloadService` 做 data 目录 ↔ 对象存储同步（快照/恢复用） | **无任何交互式文件浏览/上传/下载/编辑接口**，无 HTTP 服务 |
| controller-go | 知道实例所在 node_agent（node_agent_id → node.ip + agent.port）；HTTP 8088 | 无"文件会话"授权接口 |
| platform-service | BFF；校验订单归属（instance_id 关联）；JWT 认证 | 无文件会话接口 |
| platform-web | Vue3 + shadcn；已有实例列表页 | 无文件管理器 |

**结论**：文件能力必须新建。核心设计问题是**数据流路径**——这正是"减少流量"的关键。

## 3. 方案选型：文件数据流走哪条路

### 方案 A：全代理（浏览器 → platform → controller → node_agent）

```text
浏览器 ──> platform-service ──> controller-go ──> node_agent ──> 实例 /data
```

- 优点：实现最简单，权限校验集中在 platform，无需新端口/新协议。
- 缺点：**每个字节都过 3 个中间服务**。上传 1GB 文件，链路流量 ≈ 3GB+；platform/controller 的内存、CPU、带宽被文件流长期占用（大文件会拖垮控制面）；浏览器进度条/断点续传难以做。

### 方案 B：控制面授权 + 数据面直连（推荐）

```text
控制面（轻量，一次性授权）：
浏览器 ──> platform-service（校验订单归属）──> controller（查实例所在 node_agent、签发短效文件会话 token）

数据面（重流量，直连）：
浏览器 ──────────> node_agent 新增的 HTTP 文件服务（50054，携带会话 token）──> 实例 /data
```

- 优点：
  - 上传/下载/列表全部**只走一跳**，1GB 文件链路流量 = 1GB；platform/controller 零文件流负载；
  - 浏览器原生 HTTP（fetch/XHR）→ 进度条、流式、Range 断点续传都容易做；
  - 控制面只做一次权限校验 + 签发 token，之后不再参与。
- 缺点：node_agent 需新增 HTTP 文件服务；浏览器必须能**网络直达** node_agent（见 §7 边界条件）。

### 流量对比（上传 1GB 文件）

| 指标 | 方案 A 全代理 | 方案 B 直连 |
| --- | --- | --- |
| 链路流量 | ~3GB（3 跳 × 1GB） | **1GB（1 跳）** |
| platform/controller 负载 | 承载全部文件流 | **零** |
| 上传进度/断点续传 | 难做 | 原生支持 |
| 大文件对控制面的拖累 | 有（可能阻塞业务请求） | 无 |

**决策：方案 B**。

## 4. 推荐方案详细设计

### 4.1 总体架构

```text
                        ┌────────────── 控制面（轻量）──────────────┐
前端文件管理器             │                                        │
  │  POST /api/me/instances/:orderId/file-session                │
  ▼  （带用户 JWT）            │                                        ▼
platform-service ──────> controller-go ────> node_agent gRPC
（校验订单归属）      （查实例→node_agent）  （无需改动，token 无状态）
  │                                        │
  │  返回 { base_url: node_ip:50054,       │
  │        token: 短效 JWT, instance_id,   │
  │        data_root: /data, expires_at }  │
  ▼                                        │
前端 ──────────────────────── 数据面直连（重流量）────────────────▶
  GET/PUT/DELETE /v1/instances/{id}/files*?path=... + Bearer token
                              ▼
                    node_agent HTTP 文件服务（新端口 50054）
                              ▼
                    实例 data 目录（Docker volume）
```

### 4.2 node_agent：新增 HTTP 文件服务（新端口 50054）

与 gRPC 端口（50052）分离，独立监听。接口：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/v1/instances/{instanceId}/files?path=/subdir` | 目录列表（名称/类型/大小/修改时间） |
| GET | `/v1/instances/{instanceId}/files/content?path=...` | 下载（流式 + Content-Disposition + 支持 Range） |
| PUT | `/v1/instances/{instanceId}/files/content?path=...` | 上传（流式，Content-Length 或 chunked） |
| DELETE | `/v1/instances/{instanceId}/files?path=...` | 删除文件/空目录 |
| POST | `/v1/instances/{instanceId}/files/rename?from=&to=` | 重命名/移动 |
| POST | `/v1/instances/{instanceId}/files/mkdir?path=...` | 新建目录 |
| GET | `/v1/instances/{instanceId}/files/text?path=...` | 读文本（**限 ≤2MB**，防误用） |
| PUT | `/v1/instances/{instanceId}/files/text?path=...` | 写文本（**限 ≤2MB**） |

**鉴权**：`Authorization: Bearer <file-session-token>`，每次请求校验。

**安全约束（node_agent 强制）**：

- 所有 `path` 必须**规范化后仍落在该实例 data 根目录内**（拒绝 `..`、绝对路径、符号链接逃逸），防止越权访问节点其他目录；
- token 绑定 `instance_id`，与 URL 中的实例不一致直接 403；
- 校验实例记录存在（从本地 SQLite 查）；
- 文本接口限制 2MB，二进制走 content 接口；
- 可选：对运行中的实例，写操作返回 `409 + 提示`（可在 UI 层提示而非强拦，见 §7 风险）。

### 4.3 会话 token：无状态 JWT（controller 签，node_agent 验）

- **签发方**：controller-go（配置 `NODE_AGENT_FILE_SECRET`，与所有 node_agent 共享的 HMAC 密钥）；node_agent 使用**同名环境变量** `NODE_AGENT_FILE_SECRET`，**controller 与全部 node_agent 必须配置一致**，否则验签失败（401）；默认 `dev-file-secret-change-me` 仅限开发；
- **载荷**：`{ instance_id, scope: "files", exp: now+30min }`；
- **校验方**：node_agent 文件服务用同一密钥验签（HS256），**不存会话、无状态**，天然支持多副本；
- 有效期 30 分钟，过期前端重新发起会话。

> 备选：node_agent 增加 `CreateFileSession` gRPC 由 controller 调用生成随机 token——多一次 RPC 且 node_agent 要存会话，不推荐。

### 4.4 controller：文件会话接口

新增（内部供 platform 调用，也可直接暴露给前端但平台是唯一入口）：

```text
POST /api/game-instances/:id/file-session
→ 查实例 → 查 node_agent/node → 用共享密钥签 JWT
→ 200 { base_url: "http://<node.ip>:50054", token, instance_id, data_root: "/data", expires_at }
```

文件服务端口约定：默认 gRPC 端口 + 1（DB 里 node_agent 端口是 9090/50052，文件端口 = 端口+1 = 50053/50054 之类）。为避免歧义，**在 controller 配置里加 `NODE_AGENT_FILE_PORT_OFFSET`（默认 +1）**，或给 node_agent 表加 `file_port` 列（更明确，推荐后置）。MVP 用 offset。

### 4.5 platform-service：文件会话接口（权限门）

```text
POST /api/me/instances/:orderId/file-session     （本人或管理员，auth 中间件）
→ 校验订单存在且属于当前用户（或管理员）→ 取 instance_id → 调 controller file-session
→ 200 { base_url, token, instance_id, data_root, expires_at }

GET  /api/instances/:id/file-session             （仅管理员，可给任意实例）
```

**权限模型**：普通用户只能对自己订单关联的实例开会话；管理员对全部实例。会话签发是唯一权限检查点，之后数据面操作不再经过 platform（token 已绑定实例，node_agent 校验）。

### 4.6 前端：文件管理器

入口：实例列表（我的服务器/实例总览）每行加「文件」按钮 → `/my-servers/:orderId/files`。

```text
┌────────────────────────────────────────────────────┐
│ 我的服务器 / 实例文件      [路径: /data/...] [上传][新建目录][刷新] │
├────────────────────────────────────────────────────┤
│ 📁 ..                      目录                │
│ 📁 saves                  2026-08-01 12:00   │
│ 📄 serverconfig.xml       12KB  2026-08-01   [编辑][下载][重命名][删除] │
│ 📦 world.tar.gz           1.2GB 2026-08-01   [下载][删除]              │
└────────────────────────────────────────────────────┘
   拖拽文件到此处上传（多文件 + 进度条）
```

- **页面加载**：请求 file-session → 拿 base_url + token → 所有文件操作直连 node_agent；
- **列表**：GET files?path=... 渲染表格 + 面包屑导航；
- **上传**：拖拽/多选，`PUT files/content`，XHR 进度条，可取消；
- **下载**：`<a href="...&token=...">` 或 fetch blob（大文件用前者，浏览器原生流式）；
- **编辑**：文本文件（≤2MB）读入 CodeMirror 弹窗，保存走 `PUT files/text`；二进制不提供编辑按钮；
- **删除/重命名**：确认框后调 DELETE / rename；
- **token 过期**：请求 403 → 静默重新请求 file-session → 重试一次（axios 封装）；
- **UI 安全提示**：实例 `running` 时顶部黄条提示"实例运行中，修改配置文件可能需重启生效；正在被游戏写入的文件请勿编辑"。

## 5. 流量/转发优化分析（对照需求"尽可能减少"）

| 优化点 | 手段 | 收益 |
| --- | --- | --- |
| 大文件上传/下载 | 浏览器直连 node_agent（方案 B） | 1GB 文件链路流量从 3GB+ 降到 1GB；中间层零负载 |
| 目录列表/元数据 | 也走直连（不经过 platform/controller） | 列表操作零中间层占用，且实时 |
| 文本编辑 | 直连 + 2MB 上限 | 小文件不产生额外转发；大文件引导走 content 上传 |
| 下载大文件 | HTTP Range 支持 | 支持断点续传，失败重试只补传缺失段 |
| 会话鉴权 | 无状态 JWT（HMAC） | 每次数据面请求无需回调控制面，减少控制面 RTT |

## 6. 安全设计

| 威胁 | 对策 |
| --- | --- |
| 越权访问他人实例 data | platform 签发会话前校验订单归属；token 绑定 instance_id；node_agent 校验 token 与 URL 实例一致 |
| token 泄露 | 短效（30min）；scope 仅 files；过期后无法使用 |
| 路径穿越（../../etc/passwd） | node_agent 规范化路径 + 必须位于实例 data 根内 + 拒绝符号链接逃逸 |
| 上传超限 | 单文件大小上限（如 10GB，可配）；文本接口 2MB 硬限 |
| 篡改 token | HMAC 共享密钥仅 controller/node_agent 持有，前端不可见 |
| 中间人 | 生产建议 node_agent 文件服务走 HTTPS（或至少内网 + 网络隔离） |

## 7. 边界条件与风险

1. **浏览器必须能直达 node_agent**：若 node_agent 不在用户可达网络（纯内网/无公网），直连方案需前置网关（如 Nginx 反代按 instance_id 路由，或 WebSocket 隧道）。**这是方案 B 的硬前提**，部署时需确认。兜底：可在 node_agent 前面套一层轻量反向代理做 TLS 终止 + 路径转发，仍保持单跳。
2. **运行中实例的文件一致性**：游戏可能正在读写文件（存档/日志）。UI 提示 + 文档说明；不强制加锁（MVP），后续可加"写操作需实例停止"的开关。
3. **超大文件（>10GB）**：MVP 单请求上传，M4 加客户端分片 + Range 续传。
4. **符号链接/特殊文件**：列表跳过特殊文件；编辑/下载遇链接需确认（MVP 直接拒绝，避免逃逸）。
5. **node_agent 端口暴露**：新增 50054 端口需在防火墙/安全组放行；若节点是共享主机，多实例 token 隔离已覆盖。

## 8. 里程碑

| 阶段 | 内容 | 验收 |
| --- | --- | --- |
| M1 ✅ | node_agent HTTP 文件服务（list/content/text/rename/delete/mkdir + JWT 校验 + 路径安全） | **2026-08-15 验收通过**：401/列表/上传/下载/文本读写/重命名/删除/建目录全通，路径穿越被归一化拒绝，token 实例绑定生效 |
| M2 ✅ | controller file-session 接口 + platform 会话接口（权限门） | **2026-08-15 完成**：Go 签发器 + 单测通过；Go↔Rust JWT 交叉验证 PASS（HS256 签名有效、claims 字段兼容）；platform 用户/管理员会话接口 + 编译测试通过 |
| M3 ✅ | 前端文件管理器（浏览/上传/下载/删除/重命名/编辑） | **2026-08-15 完成**：文件会话客户端（401 自动刷新重试）、面包屑/表格/上传进度/下载（query token，a 标签直连）/文本编辑/重命名/删除、实例列表「文件」入口；node_agent download 支持 ?token=；typecheck + 生产构建通过 |
| M4 | 分片上传/断点续传、大文件 Range 下载、文件夹上传、运行中写保护开关 | 10GB 文件可断点续传 |

## 9. 待确认决策点

1. **数据面直连 vs 全代理** → 推荐直连（本设计默认直连）；
2. **node_agent 文件服务端口**：新开 50054（推荐）vs 复用 gRPC 端口（cmux 多路复用）；
3. **token 机制**：controller 签 JWT + node_agent 共享密钥验签（推荐，无状态）；
4. **编辑能力范围**：仅文本（≤2MB）vs 支持二进制在线编辑（不推荐）；
5. **部署前提确认**：浏览器能否直达 node_agent？不能则需网关方案；
6. **文件大小上限**：默认单文件 10GB 是否合适。
