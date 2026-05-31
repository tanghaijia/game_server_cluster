# 多游戏 Docker/Kubernetes 游戏服务器集群调度系统开发文档

## 1. 文档目标

本文档定义一个面向多种 SteamCMD 游戏服务器的容器集群调度系统。系统目标是在多节点环境中按需启动、停止、恢复和迁移游戏服务器实例，并在节点资源紧张时优先回收空闲实例，将新实例调度到更合适的节点。

本文档倾向于使用 Rust 作为主要开发语言，底层运行环境推荐 Kubernetes，MVP 阶段可先实现 Docker Agent 模式。

---

## 2. 背景与核心问题

游戏服务器容器与普通 Web 服务不同，具有以下特点：

1. 通常是长连接或 UDP 服务。
2. 运行时有玩家连接、对局状态、世界状态等内存态数据。
3. 运行过程中会产生磁盘数据，例如存档、地图、配置、日志、白名单、banlist、模组配置等。
4. 不适合对活跃中的容器做热迁移。
5. 多数游戏服需要通过 RCON、stdin、命令行或特定协议执行保存和优雅停服。
6. 不同游戏的目录结构差异很大，平台层不能硬编码每个游戏的存档目录。
7. 每个实例可能使用不同模组组合、模组版本和加载顺序。

因此，本系统不是传统 HTTP 负载均衡系统，而是一个**状态型游戏服务器实例调度与生命周期管理系统**。

---

## 3. 核心设计原则

### 3.1 不迁移活跃游戏服

活跃实例指有玩家在线、正在对局、正在保存或处于管理员锁定状态的游戏服。

活跃实例不做跨节点迁移。节点资源紧张时，优先处理空闲实例、待机实例、测试实例或低优先级实例。

### 3.2 空闲实例冷迁移

当实例空闲时，可以执行：

1. 标记停止中。
2. 禁止新玩家进入。
3. 调用保存脚本。
4. 优雅停止容器。
5. 同步 `/data` 到中心存储。
6. 释放节点资源、端口和锁。
7. 后续可在其他节点恢复启动。

### 3.3 平台不理解游戏内部目录

平台只约定统一数据边界：

```text
/data = 实例持久化数据根目录
```

每个游戏适配器负责把具体游戏的存档、配置、模组配置等映射到 `/data`。

平台只负责快照和恢复 `/data`，不关心 `/data` 内部结构。

### 3.4 模组作为独立资产建模

模组不简单归类为游戏二进制，也不全部归类为实例数据。

推荐模型：

```text
公共模组文件：按版本做成可缓存制品
实例模组清单：随实例数据保存
实例模组配置：随实例数据保存
实例自定义覆盖：随实例数据保存
无法标准化的模组：放入 /data/mods 作为兜底
```

### 3.5 容器可销毁，实例数据不可丢

容器生命周期与实例数据生命周期分离。

容器删除不应导致实例丢失。实例恢复依赖：

```text
game_build + mod_manifest + /data snapshot + env + ports
```

---

## 4. 总体架构

推荐长期架构：

```text
                 ┌────────────────────┐
                 │ Matchmaker / Lobby │
                 └─────────┬──────────┘
                           │
                           ▼
                 ┌────────────────────┐
                 │ Game API           │
                 │ Rust / Axum        │
                 └─────────┬──────────┘
                           │
                           ▼
              ┌──────────────────────────┐
              │ GameServer Controller    │
              │ Rust                     │
              └───────────┬──────────────┘
                          │
                          ▼
              ┌──────────────────────────┐
              │ Kubernetes               │
              │ Pod / PVC / Node / Event │
              └───────────┬──────────────┘
                          │
       ┌──────────────────┼──────────────────┐
       ▼                  ▼                  ▼
   ┌────────┐         ┌────────┐         ┌────────┐
   │ node-1 │         │ node-2 │         │ node-3 │
   │ Pod    │         │ Pod    │         │ Pod    │
   └────────┘         └────────┘         └────────┘
                          │
                          ▼
              ┌──────────────────────────┐
              │ Object Storage            │
              │ MinIO / S3 / NAS          │
              └──────────────────────────┘
                          │
                          ▼
              ┌──────────────────────────┐
              │ PostgreSQL / Redis / etcd │
              │ 状态、锁、版本、端口       │
              └──────────────────────────┘
```

MVP 可选架构：

```text
Game API + Scheduler
        │
        ▼
Node Agent over gRPC
        │
        ▼
Docker Engine API
```

---

## 5. 技术选型

### 5.1 语言

主要开发语言：Rust。

推荐 Rust 技术栈：

| 模块 | 建议 |
|---|---|
| Web API | axum |
| 异步运行时 | tokio |
| 序列化 | serde / serde_json / serde_yaml |
| 数据库 | sqlx |
| PostgreSQL | 主状态库 |
| Redis | 锁、心跳、短期状态，可选 |
| gRPC | tonic，可用于 Node Agent |
| Kubernetes Client | kube-rs |
| S3 / MinIO | object_store 或 AWS SDK for Rust |
| 日志 | tracing / tracing-subscriber |
| 指标 | prometheus / opentelemetry |
| 配置 | config / figment |
| 错误处理 | thiserror / anyhow |
| CLI | clap |

### 5.2 底层编排

长期推荐：Kubernetes。

MVP 可选：Docker + 自研 Node Agent。

不推荐长期完全自研容器集群系统。

---

## 6. 系统模块划分

### 6.1 Game API

对外提供游戏服实例管理接口。

职责：

1. 创建实例。
2. 启动实例。
3. 停止实例。
4. 查询实例状态。
5. 查询连接地址。
6. 更新模组清单。
7. 触发快照。
8. 管理游戏适配器。
9. 管理模组资产。

推荐实现：Rust + Axum。

### 6.2 GameServer Controller

核心业务控制器。

职责：

1. 监听期望状态。
2. 将期望状态转化为 Pod / Docker 容器。
3. 维护实例状态机。
4. 处理空闲回收。
5. 执行冷迁移。
6. 管理快照版本。
7. 管理锁和 fencing token。
8. 更新连接地址。
9. 处理节点压力事件。

K8s 模式下可实现为自定义 Controller。

### 6.3 Scheduler

游戏服业务调度器。

职责：

1. 根据节点资源选择节点。
2. 根据数据本地性选择节点。
3. 根据模组缓存命中率选择节点。
4. 根据端口池选择节点。
5. 避免调度到压力节点。
6. 处理不同游戏资源需求。

评分参考：

```text
score = cpu_score
      + memory_score
      + disk_score
      + data_locality_score
      + mod_cache_score
      + region_score
      - node_pressure_penalty
      - migration_cost
      - download_cost
```

### 6.4 Snapshot Manager

负责实例数据快照。

职责：

1. 周期快照。
2. 停服最终快照。
3. 快照上传。
4. 快照校验。
5. 快照恢复。
6. 快照保留策略。
7. latest 指针管理。
8. manifest 生成。

平台默认快照 `/data`。

### 6.5 Mod Asset Manager

负责模组资产管理。

职责：

1. 上传模组。
2. 下载 Workshop / 外部模组。
3. 生成不可变模组制品。
4. 计算 checksum。
5. 维护 modpack。
6. 维护实例 mod manifest。
7. 节点模组缓存预热。
8. 模组版本回滚。

### 6.6 Node Agent

K8s 模式下可减少 Node Agent 依赖，但仍可保留 Sidecar / DaemonSet 形式用于节点本地操作。

职责：

1. 上报节点资源。
2. 上报端口池。
3. 准备游戏二进制缓存。
4. 准备模组缓存。
5. 拉取和恢复实例快照。
6. 执行本地快照上传。
7. 调用容器内生命周期脚本。

MVP Docker 模式下，Node Agent 是必要组件。

### 6.7 Game Adapter

每个游戏的适配层。

职责：

1. 定义镜像。
2. 定义端口。
3. 定义资源需求。
4. 定义启动命令。
5. 定义保存命令。
6. 定义停服命令。
7. 定义玩家数查询命令。
8. 将具体游戏目录映射到 `/data`。
9. 组装模组运行目录。

---

## 7. 数据分层设计

### 7.1 镜像层

容器镜像内放：

```text
/opt/steamcmd
/opt/scripts
/opt/tools
/scripts/start.sh
/scripts/save.sh
/scripts/stop.sh
/scripts/players.sh
/scripts/health.sh
```

不建议放：

```text
实例存档
实例日志
玩家上传数据
大体积游戏二进制，除非镜像绑定游戏版本
```

### 7.2 节点游戏缓存层

宿主机路径示例：

```text
/srv/game-cache/{game}/{game_build}/
```

容器挂载：

```text
/server = /srv/game-cache/{game}/{game_build}
```

这一层放 SteamCMD 下载出的游戏服务端文件，可被多个实例共享。

### 7.3 节点模组缓存层

宿主机路径示例：

```text
/srv/mod-cache/{game}/mods/{mod_id}/{version}/
/srv/mod-cache/{game}/modpacks/{modpack_id}/{version}/
```

容器挂载：

```text
/mod-cache = /srv/mod-cache/{game}
```

### 7.4 实例运行层

宿主机路径示例：

```text
/data/game-instances/{server_id}/
```

容器挂载：

```text
/data = /data/game-instances/{server_id}
```

平台只承诺恢复 `/data`。

推荐结构：

```text
/data/
  mod-manifest.json
  mod-config/
  mod-overrides/
  mods/                 # embedded 模式兜底
  saves/
  world/
  config/
  logs/
  tmp/
  runtime/
```

平台不强依赖这些子目录，只将它们作为推荐规范。

### 7.5 中心快照层

对象存储路径示例：

```text
game-data/{game}/{server_id}/latest.json
game-data/{game}/{server_id}/snapshots/v000043/data.tar.zst
game-data/{game}/{server_id}/snapshots/v000043/manifest.json
```

### 7.6 元数据层

建议使用 PostgreSQL 作为主状态库。

Redis 可作为短期锁、心跳、缓存，但不建议只依赖 Redis 保存关键状态。

---

## 8. 游戏适配器规范

每个游戏需要声明一个 Game Adapter。

示例：

```yaml
game: palworld
image: registry.example.com/games/palworld:latest
runtime: kubernetes

storage:
  dataPath: /data
  serverPath: /server
  modCachePath: /mod-cache
  storageMode: separated

lifecycle:
  start: /scripts/start.sh
  save: /scripts/save.sh
  stop: /scripts/stop.sh
  players: /scripts/players.sh
  health: /scripts/health.sh

ports:
  - name: game
    containerPort: 8211
    protocol: udp
  - name: query
    containerPort: 27015
    protocol: udp

resources:
  cpuRequest: "2"
  memoryRequest: "8Gi"
  cpuLimit: "4"
  memoryLimit: "12Gi"

snapshot:
  path: /data
  exclude:
    - "tmp/**"
    - "runtime/**"
    - "logs/**"
    - "**/*.pid"
    - "**/*.lock"

mods:
  policy: hybrid
```

### 8.1 生命周期脚本契约

所有游戏镜像必须提供：

```text
/scripts/start.sh
/scripts/save.sh
/scripts/stop.sh
/scripts/players.sh
/scripts/health.sh
```

#### start.sh

启动游戏服务器。

要求：

1. 可以读取 `/server`。
2. 可以读写 `/data`。
3. 必须以前台方式或可被 supervisor 管理的方式启动主进程。
4. 必须正确处理 SIGTERM。

#### save.sh

触发游戏保存。

要求：

1. 成功时返回 0。
2. 失败时返回非 0。
3. 必须保证内存态尽可能刷入 `/data`。

#### stop.sh

优雅停服。

要求：

1. 禁止新玩家进入，若游戏支持。
2. 广播停服，若游戏支持。
3. 调用保存。
4. 停止主进程。
5. 成功时返回 0。

#### players.sh

输出当前玩家数。

建议输出 JSON：

```json
{
  "players": 3,
  "max_players": 32
}
```

#### health.sh

输出健康状态。

建议输出 JSON：

```json
{
  "healthy": true,
  "reason": "ok"
}
```

---

## 9. 模组系统设计

### 9.1 模组存储策略

系统支持三种模组策略。

#### cached

模组本体存储在节点模组缓存中。

实例 `/data` 只保存：

```text
mod-manifest.json
mod-config/
```

适合标准化模组和多个实例共享模组。

#### embedded

模组本体存储在：

```text
/data/mods
```

实例快照包含模组文件。

适合高度定制、玩家上传、无法标准化的游戏。

#### hybrid

推荐默认策略。

公共模组放：

```text
/mod-cache
```

实例配置和覆盖放：

```text
/data/mod-config
/data/mod-overrides
/data/mod-manifest.json
```

### 9.2 mod-manifest.json

每个实例保存一个模组清单。

```json
{
  "game": "palworld",
  "server_id": "game-1001",
  "game_build": "version-20260529",
  "policy": "hybrid",
  "modpack": {
    "id": "survival-pack",
    "version": "1.3.0",
    "checksum": "sha256:..."
  },
  "mods": [
    {
      "id": "mod-foo",
      "version": "1.2.0",
      "source": "workshop",
      "workshop_id": "1234567890",
      "artifact": "s3://mod-artifacts/palworld/mods/mod-foo/1.2.0/mod.tar.zst",
      "checksum": "sha256:...",
      "enabled": true
    }
  ],
  "load_order": [
    "mod-foo"
  ]
}
```

### 9.3 模组版本不可变

禁止使用 `latest` 作为运行依赖。

所有模组运行依赖必须包括：

```text
mod_id
version
checksum
source
artifact uri
load order
```

### 9.4 模组更新流程

```text
1. 上传或下载新模组版本
2. 生成 artifact
3. 计算 checksum
4. 写入 mod_artifacts
5. 修改实例 mod-manifest.json
6. 创建配置版本
7. 停止实例或等待空闲
8. 重启实例生效
9. 生成新快照
```

禁止直接覆盖旧版本模组目录。

---

## 10. 状态机设计

### 10.1 实例状态

```text
Created
Preparing
RestoringData
PreparingGameBuild
PreparingMods
Starting
Running
Idle
Saving
Snapshotting
Stopping
Stopped
Migrating
Failed
Uncertain
Deleting
Deleted
```

### 10.2 状态流转

启动流程：

```text
Created
  -> Preparing
  -> RestoringData
  -> PreparingGameBuild
  -> PreparingMods
  -> Starting
  -> Running
```

空闲停服：

```text
Running
  -> Idle
  -> Saving
  -> Snapshotting
  -> Stopping
  -> Stopped
```

冷迁移：

```text
Stopped
  -> Preparing on new node
  -> RestoringData
  -> PreparingGameBuild
  -> PreparingMods
  -> Starting
  -> Running
```

异常状态：

```text
Running
  -> Uncertain
  -> Failed 或 Recovery
```

### 10.3 Uncertain 状态

节点心跳丢失时，不应立即在其他节点启动同一实例。

应进入 `Uncertain`：

```text
1. 节点心跳超时
2. 检查实例锁
3. 检查最后快照版本
4. 尝试确认旧容器是否还在运行
5. 如果无法确认，等待 fencing 超时或人工介入
6. 确认旧实例不可写后，才允许恢复
```

---

## 11. 锁与 Fencing 设计

同一个 `server_id` 不允许在多个节点同时运行。

### 11.1 锁字段

```text
server_id
lock_owner
fencing_token
lease_expires_at
status
created_at
updated_at
```

### 11.2 fencing token

每次实例启动获取一个单调递增 token。

写入快照、更新状态、上传最终数据时必须携带 token。

如果 token 过期或不是当前 token，则拒绝写入，防止旧节点恢复后覆盖新数据。

### 11.3 锁获取流程

```text
1. BEGIN
2. SELECT server_locks WHERE server_id = ? FOR UPDATE
3. 检查锁是否过期或已释放
4. fencing_token + 1
5. 写入 lock_owner 和 lease_expires_at
6. COMMIT
```

### 11.4 锁释放流程

```text
1. 确认实例已停止
2. 确认最终快照成功
3. 更新 server status = stopped
4. 清理 lock_owner
5. 保留 fencing_token
```

---

## 12. 快照设计

### 12.1 周期快照

运行中定期执行。

默认周期可配置，例如 5 分钟、10 分钟、30 分钟。

流程：

```text
1. 调用 /scripts/save.sh
2. 等待保存完成
3. 打包 /data
4. 应用 exclude 规则
5. 上传到对象存储
6. 生成 manifest.json
7. 更新 latest.json
8. 写入 server_snapshots
```

### 12.2 停服最终快照

停服时必须执行。

流程：

```text
1. 标记 Stopping
2. 禁止新分配到该实例
3. 调用 /scripts/stop.sh
4. 等待容器退出
5. 打包 /data
6. 上传对象存储
7. 更新 latest.json
8. 更新 data_version
9. 释放锁和端口
```

### 12.3 快照 manifest

```json
{
  "server_id": "game-1001",
  "game": "palworld",
  "snapshot_version": 43,
  "created_at": "2026-05-29T20:00:00+08:00",
  "source_node": "node-1",
  "fencing_token": 88,
  "game_build": "version-20260529",
  "data_archive": "s3://game-data/palworld/game-1001/snapshots/v000043/data.tar.zst",
  "checksum": "sha256:...",
  "mod_dependencies": [
    {
      "id": "mod-foo",
      "version": "1.2.0",
      "checksum": "sha256:..."
    }
  ]
}
```

### 12.4 快照保留策略

建议：

```text
保留最近 10 个快照
保留最近 24 小时内每小时一个快照
保留最近 7 天内每天一个快照
手动标记的快照不自动删除
```

---

## 13. 调度策略

### 13.1 节点约束

调度前必须检查：

```text
节点在线
CPU 足够
内存足够
磁盘足够
端口可用
游戏类型允许
区域匹配
节点非压力状态
```

### 13.2 数据本地性

如果实例最近在某节点运行，且本地数据仍存在，可提高该节点评分。

### 13.3 模组缓存命中

如果节点已经缓存所需 game_build 和 mods，可提高评分。

### 13.4 节点压力处理

节点压力等级：

```text
Normal
Warning
Critical
Draining
Offline
```

压力处理策略：

```text
Warning:
  不再调度新实例到该节点

Critical:
  优先停止空闲实例
  不杀有玩家实例

Draining:
  停止所有空闲实例
  有玩家实例等待自然结束或人工迁移

Offline:
  标记节点失联
  实例进入 Uncertain
```

---

## 14. 网络与端口设计

### 14.1 连接模型

推荐玩家直连具体节点：

```text
Lobby / Matchmaker 返回 node_ip:port
```

示例：

```json
{
  "server_id": "game-1001",
  "ip": "1.2.3.4",
  "port": 27015,
  "protocol": "udp"
}
```

### 14.2 端口池

每个节点维护端口池。

示例：

```text
27015-28000/udp
28001-29000/tcp
```

端口分配需要写入数据库，避免冲突。

### 14.3 K8s 网络方案

可选方案：

1. HostNetwork + hostPort。
2. NodePort。
3. LoadBalancer，云环境可用。
4. 自研 UDP 代理，不作为首选。

游戏服推荐优先考虑 HostNetwork/hostPort 或明确的 NodePort 分配。

---

## 15. 数据库设计草案

### 15.1 game_servers

```sql
CREATE TABLE game_servers (
    server_id TEXT PRIMARY KEY,
    game TEXT NOT NULL,
    status TEXT NOT NULL,
    desired_state TEXT NOT NULL,
    node_name TEXT,
    public_ip TEXT,
    public_port INTEGER,
    protocol TEXT,
    players INTEGER NOT NULL DEFAULT 0,
    max_players INTEGER,
    game_build TEXT,
    data_version BIGINT NOT NULL DEFAULT 0,
    mod_manifest_version BIGINT NOT NULL DEFAULT 0,
    priority INTEGER NOT NULL DEFAULT 100,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 15.2 server_locks

```sql
CREATE TABLE server_locks (
    server_id TEXT PRIMARY KEY REFERENCES game_servers(server_id),
    lock_owner TEXT,
    fencing_token BIGINT NOT NULL DEFAULT 0,
    lease_expires_at TIMESTAMPTZ,
    status TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 15.3 server_snapshots

```sql
CREATE TABLE server_snapshots (
    id BIGSERIAL PRIMARY KEY,
    server_id TEXT NOT NULL REFERENCES game_servers(server_id),
    snapshot_version BIGINT NOT NULL,
    storage_uri TEXT NOT NULL,
    checksum TEXT,
    size_bytes BIGINT,
    source_node TEXT,
    game_build TEXT,
    fencing_token BIGINT NOT NULL,
    is_latest BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(server_id, snapshot_version)
);
```

### 15.4 nodes

```sql
CREATE TABLE nodes (
    node_name TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    region TEXT,
    public_ip TEXT,
    cpu_total_milli INTEGER,
    cpu_available_milli INTEGER,
    memory_total_bytes BIGINT,
    memory_available_bytes BIGINT,
    disk_available_bytes BIGINT,
    pressure TEXT NOT NULL DEFAULT 'Normal',
    last_heartbeat TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 15.5 node_ports

```sql
CREATE TABLE node_ports (
    node_name TEXT NOT NULL,
    port INTEGER NOT NULL,
    protocol TEXT NOT NULL,
    server_id TEXT,
    status TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(node_name, port, protocol)
);
```

### 15.6 mod_artifacts

```sql
CREATE TABLE mod_artifacts (
    id BIGSERIAL PRIMARY KEY,
    game TEXT NOT NULL,
    mod_id TEXT NOT NULL,
    version TEXT NOT NULL,
    source TEXT NOT NULL,
    artifact_uri TEXT NOT NULL,
    checksum TEXT NOT NULL,
    size_bytes BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(game, mod_id, version)
);
```

### 15.7 server_mod_manifests

```sql
CREATE TABLE server_mod_manifests (
    server_id TEXT PRIMARY KEY REFERENCES game_servers(server_id),
    version BIGINT NOT NULL,
    content JSONB NOT NULL,
    checksum TEXT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

---

## 16. API 设计草案

### 16.1 创建实例

```http
POST /api/v1/servers
```

请求：

```json
{
  "game": "palworld",
  "server_id": "game-1001",
  "region": "sg",
  "game_build": "version-20260529",
  "resources": {
    "cpu": "2",
    "memory": "8Gi"
  },
  "mods": {
    "policy": "hybrid",
    "manifest": {}
  }
}
```

### 16.2 启动实例

```http
POST /api/v1/servers/{server_id}/start
```

### 16.3 停止实例

```http
POST /api/v1/servers/{server_id}/stop
```

请求：

```json
{
  "mode": "graceful",
  "reason": "idle_timeout"
}
```

### 16.4 查询实例状态

```http
GET /api/v1/servers/{server_id}
```

响应：

```json
{
  "server_id": "game-1001",
  "status": "Running",
  "players": 3,
  "node": "node-1",
  "connection": {
    "ip": "1.2.3.4",
    "port": 8211,
    "protocol": "udp"
  }
}
```

### 16.5 获取连接地址

```http
POST /api/v1/allocations
```

请求：

```json
{
  "game": "palworld",
  "region": "sg",
  "server_id": "game-1001"
}
```

响应：

```json
{
  "server_id": "game-1001",
  "ip": "1.2.3.4",
  "port": 8211,
  "protocol": "udp"
}
```

### 16.6 触发快照

```http
POST /api/v1/servers/{server_id}/snapshot
```

### 16.7 更新模组清单

```http
PUT /api/v1/servers/{server_id}/mods
```

---

## 17. Kubernetes 资源设计

### 17.1 GameServer CRD 草案

```yaml
apiVersion: game.example.com/v1
kind: GameServer
metadata:
  name: game-1001
spec:
  game: palworld
  gameBuild: version-20260529
  desiredState: Running
  region: sg
  resources:
    cpuRequest: "2"
    memoryRequest: "8Gi"
    cpuLimit: "4"
    memoryLimit: "12Gi"
  storage:
    dataPath: /data
    snapshotPolicy: default
  mods:
    policy: hybrid
    manifestRef: game-1001-mod-manifest
status:
  phase: Running
  nodeName: node-1
  players: 3
  publicIp: 1.2.3.4
  publicPort: 8211
  dataVersion: 43
```

### 17.2 Pod 模板

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: game-1001
  labels:
    app: gameserver
    server-id: game-1001
spec:
  terminationGracePeriodSeconds: 120
  containers:
    - name: gameserver
      image: registry.example.com/games/palworld:latest
      command: ["/scripts/start.sh"]
      resources:
        requests:
          cpu: "2"
          memory: "8Gi"
        limits:
          cpu: "4"
          memory: "12Gi"
      volumeMounts:
        - name: server
          mountPath: /server
          readOnly: true
        - name: data
          mountPath: /data
        - name: mod-cache
          mountPath: /mod-cache
          readOnly: true
      lifecycle:
        preStop:
          exec:
            command: ["/scripts/stop.sh"]
  volumes:
    - name: server
      hostPath:
        path: /srv/game-cache/palworld/version-20260529
    - name: mod-cache
      hostPath:
        path: /srv/mod-cache/palworld
    - name: data
      hostPath:
        path: /data/game-instances/game-1001
```

生产环境可根据存储方案替换 hostPath。

---

## 17A. Agent 通信协议：优先使用 gRPC

Node Agent 与 Game API / Scheduler 之间建议第一版就使用 gRPC，而不是先 HTTP 后切换。

原因：

```text
1. Agent 接口天然是内部 RPC，不是面向浏览器的 REST API
2. protobuf 可以稳定定义 StartInstance、StopInstance、SnapshotInstance 等结构化协议
3. Rust 使用 tonic 可以直接生成强类型 client/server
4. 后续支持流式日志、流式事件、长任务进度更自然
5. 多语言接入也更容易
```

推荐协议分层：

```text
外部控制 API：HTTP/REST，面向 Web 控制台、管理后台、第三方调用
内部节点控制：gRPC，面向 Game API / Scheduler / Node Agent
```

示例 proto：

```proto
syntax = "proto3";

package gamecluster.agent.v1;

service NodeAgentService {
  rpc StartInstance(StartInstanceRequest) returns (StartInstanceResponse);
  rpc StopInstance(StopInstanceRequest) returns (StopInstanceResponse);
  rpc InspectInstance(InspectInstanceRequest) returns (InspectInstanceResponse);
  rpc ExecInstance(ExecInstanceRequest) returns (ExecInstanceResponse);
  rpc SnapshotInstance(SnapshotInstanceRequest) returns (SnapshotInstanceResponse);
  rpc PrepareGameBuild(PrepareGameBuildRequest) returns (PrepareGameBuildResponse);
  rpc PrepareMods(PrepareModsRequest) returns (PrepareModsResponse);
  rpc WatchNode(WatchNodeRequest) returns (stream NodeEvent);
}
```

---

## 17B. 镜像与分层镜像管理

镜像需要分成三类管理：

```text
1. base image：通用运行环境，例如 steamcmd、基础依赖、生命周期脚本框架
2. game adapter image：具体游戏适配器，例如 dst-adapter、minecraft-adapter、7dtd-adapter
3. optional baked game image：少数情况下把游戏服务端版本预烘焙进镜像
```

默认不建议把 SteamCMD 下载出来的大体积游戏服务端直接放进每个实例镜像。推荐：

```text
镜像只放工具和适配逻辑
游戏服务端文件放节点 game-cache
实例数据放 /data
模组公共文件放 /mod-cache
```

### 17B.1 base image

示例：

```text
registry.example.com/game/base-steamcmd:2026.05
```

内容：

```text
steamcmd
bash/sh
curl/wget
jq
zstd/tar
rcon 工具
supervisor 或自研 entrypoint
/scripts/lib/*.sh
```

### 17B.2 game adapter image

示例：

```text
registry.example.com/game/dst-adapter:0.1.0
registry.example.com/game/minecraft-adapter:0.1.0
registry.example.com/game/7dtd-adapter:0.1.0
```

内容：

```text
/scripts/start.sh
/scripts/save.sh
/scripts/stop.sh
/scripts/players.sh
/scripts/health.sh
/scripts/prepare-runtime.sh
```

这些镜像不应该包含某个实例的存档。

### 17B.3 game build cache

游戏服务端文件由 Node Agent 准备：

```text
/srv/game-cache/{game}/{game_build}/
```

例如 DST：

```text
/srv/game-cache/dst/build-676042/
```

容器内挂载为：

```text
/server
```

### 17B.4 什么时候使用 baked game image

某些情况下可以把游戏服务端版本烘焙进镜像：

```text
1. 节点数量很多，希望减少首次启动 SteamCMD 下载
2. 游戏版本固定且更新频率低
3. CI/CD 中已经完成游戏服务端下载和验证
4. 希望通过镜像 tag 固定 game_build
```

示例：

```text
registry.example.com/game/dst-server:build-676042
```

但 baked game image 的缺点是：

```text
1. 镜像体积大
2. 每次游戏更新都要重新 build/push/pull
3. 多游戏、多版本会导致 registry 存储压力变大
```

因此默认策略是：

```text
MVP：adapter image + node game-cache
后期热点版本：可选 baked game image
```

### 17B.5 分层原则

Dockerfile 层级应按变化频率排列：

```text
系统依赖层：低频变化
steamcmd / 工具层：低频变化
通用脚本库层：中频变化
游戏 adapter 脚本层：中频变化
配置模板层：高频变化
```

不要把高频变化的实例配置放入镜像。

---

## 18. Rust 项目结构建议

```text
game-cluster/
  Cargo.toml
  crates/
    api/
      src/
        main.rs
        routes/
        handlers/
        dto/
    controller/
      src/
        main.rs
        reconciler/
        state_machine/
    scheduler/
      src/
        lib.rs
        scoring.rs
        constraints.rs
    agent/
      src/
        main.rs
        docker.rs
        fs.rs
        snapshot.rs
        mods.rs
    core/
      src/
        model/
        error.rs
        config.rs
        events.rs
    storage/
      src/
        postgres.rs
        redis.rs
        object_store.rs
    k8s/
      src/
        crd.rs
        pod_builder.rs
        client.rs
    snapshot/
      src/
        pack.rs
        restore.rs
        manifest.rs
    mods/
      src/
        manifest.rs
        artifact.rs
        resolver.rs
    adapter/
      src/
        game_adapter.rs
        lifecycle.rs
  migrations/
  deploy/
    helm/
    crds/
  examples/
    adapters/
      palworld.yaml
      minecraft.yaml
```

### 18.1 core 模型示例

```rust
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub enum ServerStatus {
    Created,
    Preparing,
    RestoringData,
    PreparingGameBuild,
    PreparingMods,
    Starting,
    Running,
    Idle,
    Saving,
    Snapshotting,
    Stopping,
    Stopped,
    Migrating,
    Failed,
    Uncertain,
    Deleting,
    Deleted,
}
```

```rust
#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct GameServer {
    pub server_id: String,
    pub game: String,
    pub status: ServerStatus,
    pub desired_state: DesiredState,
    pub node_name: Option<String>,
    pub public_ip: Option<String>,
    pub public_port: Option<u16>,
    pub protocol: Option<String>,
    pub players: u32,
    pub max_players: Option<u32>,
    pub game_build: String,
    pub data_version: i64,
    pub mod_manifest_version: i64,
}
```

---

## 19. 关键流程

### 19.1 创建并启动实例

```text
1. Game API 接收创建请求
2. 写入 game_servers
3. 写入初始 mod-manifest
4. Scheduler 选择节点
5. 获取 server lock
6. 准备 game build
7. 准备 mods
8. 恢复 latest /data 快照，若无则初始化空数据
9. 创建 Pod 或 Docker 容器
10. 等待 health 正常
11. 分配并注册连接地址
12. 状态变更为 Running
```

### 19.2 空闲检测与自动停服

```text
1. Controller 定期调用 players.sh 或读取 sidecar 指标
2. 如果 players = 0 且超过 idle_timeout
3. 状态变更为 Idle
4. 调用 stop 流程
5. 最终快照成功后释放资源
```

### 19.3 节点资源紧张处理

```text
1. Node pressure 变为 Warning
2. Scheduler 停止向该节点调度新实例
3. Node pressure 变为 Critical
4. 查找该节点 Idle / players=0 实例
5. 优雅停止并快照
6. 仍然紧张则进入人工告警或驱逐低优先级实例
```

### 19.4 跨节点恢复

```text
1. 实例处于 Stopped
2. 新节点抢锁
3. 拉取 latest 快照
4. 准备 game_build 和 mods
5. 校验 checksum
6. 启动容器
7. 更新连接地址
```

---

## 20. 观测性设计

### 20.1 Metrics

建议指标：

```text
gameserver_instances_total
gameserver_running_total
gameserver_idle_total
gameserver_players_total
gameserver_snapshot_duration_seconds
gameserver_snapshot_size_bytes
gameserver_start_duration_seconds
gameserver_stop_duration_seconds
gameserver_mod_cache_hit_ratio
node_game_cpu_available
node_game_memory_available
node_game_disk_available
```

### 20.2 Logs

所有组件使用结构化日志。

必须包含：

```text
server_id
node_name
fencing_token
snapshot_version
request_id
operation
```

### 20.3 Events

事件类型：

```text
ServerCreated
ServerScheduled
ServerStarted
ServerIdleDetected
ServerSaving
ServerSnapshotCreated
ServerStopped
ServerFailed
NodePressureChanged
ModPrepared
LockAcquired
LockReleased
```

---

## 21. 安全设计

### 21.1 容器权限

原则：

```text
非必要不使用 privileged
限制 capabilities
只挂载必要目录
/server 尽量只读
/data 仅当前实例可写
```

### 21.2 玩家上传模组安全

上传模组应经过：

```text
大小限制
文件类型限制
解压路径检查，防止 Zip Slip
checksum 计算
可选杀毒扫描
人工审核或白名单
```

### 21.3 API 权限

API 应支持：

```text
管理员权限
租户权限
实例所有者权限
只读权限
```

---

## 22. 实施路线与迁移成本建议

结合当前约束：

```text
首批游戏：七日杀、饥荒、我的世界
节点网络：每个节点都有公网 IP，玩家可直连 node_ip:port
玩家模组：允许上传
数据目标：尽量 0 丢失
```

推荐路线不是直接从第一天开始做完整 Kubernetes CRD，而是采用“两阶段架构”：

```text
阶段 1：Rust API + Rust Scheduler + Rust Node Agent + Docker Engine API
阶段 2：保留业务模型，迁移底层执行器到 Kubernetes
```

### 22.1 为什么不建议一开始直接上 K8s

直接上 K8s 的长期收益很高，但首期成本也高：

```text
1. 需要设计 CRD、Controller、Pod 模板、hostPort/NodePort 端口管理
2. 需要处理 K8s 存储、节点调度、权限、事件、Operator 生命周期
3. 游戏服真正复杂的部分仍然要自研，例如 save、stop、mods、snapshot、fencing
4. 首批游戏验证阶段更需要快速迭代 Game Adapter
```

因此，首期更建议先实现 Docker Agent MVP，把业务闭环跑通。

### 22.2 为什么 Docker Agent MVP 后续迁移成本可控

只要从第一版就把“调度决策”和“运行时执行”解耦，迁移成本可以控制在可接受范围内。

核心抽象：

```rust
#[async_trait]
pub trait RuntimeDriver {
    async fn create_instance(&self, spec: InstanceRuntimeSpec) -> Result<RuntimeInstance>;
    async fn stop_instance(&self, server_id: &str) -> Result<()>;
    async fn remove_instance(&self, server_id: &str) -> Result<()>;
    async fn inspect_instance(&self, server_id: &str) -> Result<RuntimeInstanceStatus>;
    async fn exec(&self, server_id: &str, command: Vec<String>) -> Result<ExecOutput>;
}
```

第一阶段实现：

```text
DockerRuntimeDriver
```

第二阶段新增：

```text
KubernetesRuntimeDriver
```

业务层不直接调用 Docker API 或 K8s API，而是只调用 `RuntimeDriver`。

这样迁移时主要替换：

```text
容器创建逻辑
端口暴露逻辑
日志读取逻辑
exec 调用逻辑
状态探测逻辑
```

不需要重写：

```text
GameServer 状态机
调度评分
快照逻辑
模组逻辑
锁与 fencing
Game Adapter 规范
API 层
数据库模型
```

### 22.3 推荐阶段 1：Docker Agent MVP

目标：先跑通真实业务闭环。

组件：

```text
Rust Game API
Rust Scheduler
Rust Node Agent
Docker Engine API
PostgreSQL
MinIO / S3
可选 Redis
```

必须实现：

```text
1. 实例创建、启动、停止
2. 节点资源上报
3. 公网 IP + 端口池分配
4. /data 恢复与快照
5. 周期 save + snapshot
6. 停服最终 snapshot
7. server lock + fencing token
8. 玩家数检测
9. 玩家上传模组
10. 七日杀、饥荒、我的世界三个 Game Adapter
```

暂不实现：

```text
1. K8s CRD
2. 多区域复杂调度
3. 自动跨云伸缩
4. 增量块级快照
5. 完整 Web 控制台
```

### 22.4 推荐阶段 2：Kubernetes RuntimeDriver

当 Docker MVP 稳定后，再引入 K8s。

迁移顺序：

```text
1. 增加 KubernetesRuntimeDriver
2. 新实例优先使用 K8s RuntimeDriver
3. 老实例仍可留在 Docker RuntimeDriver
4. 空闲实例停服后在 K8s 节点恢复
5. 逐步清空 Docker 节点
6. 最终移除 DockerRuntimeDriver 或保留兼容
```

由于实例恢复边界是 `/data + game_build + mod_manifest`，迁移本质上是一次冷迁移：

```text
Docker 节点停服
  -> 最终快照
  -> K8s 节点恢复 /data
  -> 准备 game_build 和 mods
  -> 启动 Pod
```

### 22.5 首批游戏适配优先级

首批游戏：

```text
1. 饥荒 / Don't Starve Together
2. 我的世界
3. 七日杀
```

由于当前决定先从饥荒节点开始，MVP 的第一个目标应改为：

```text
一个逻辑 GameServer 管理一个 DST Cluster
一个 DST Cluster 可包含 Master shard 和 Caves shard
每个 shard 可以是一个容器，也可以在早期版本中由同一个容器内的 supervisor 管理多个进程
```

#### 第一优先级：饥荒 / Don't Starve Together

原因：

```text
1. DST 天然有 cluster 概念，适合验证“逻辑实例 != 单容器”的模型
2. 常见部署包含 Master 与 Caves 两个 shard，能提前验证多进程/多容器生命周期
3. SteamCMD 部署典型，适合验证 game_build 缓存
4. 模组依赖明显，适合验证 mod-manifest 与玩家上传模组流程
5. token、cluster.ini、modoverrides 等配置适合验证 /data 恢复边界
```

DST 的实例边界建议：

```text
GameServer = 一个 DST Cluster
RuntimeUnit = 一个 shard，例如 Master 或 Caves
```

推荐目录：

```text
/data/
  cluster_token.txt
  cluster.ini
  adminlist.txt
  whitelist.txt
  blocklist.txt
  mod-manifest.json
  Master/
    server.ini
    worldgenoverride.lua
    modoverrides.lua
    save/
  Caves/
    server.ini
    worldgenoverride.lua
    modoverrides.lua
    save/
```

MVP 可以先支持两种运行模式：

```text
single-container:
  一个容器内启动 Master + Caves 两个进程
  实现简单，适合第一版

multi-container:
  Master 和 Caves 分别作为 RuntimeUnit
  生命周期由 GameServer Controller 统一管理
  更接近后续 K8s Pod/多容器模型
```

第一版建议使用 single-container，原因是：

```text
1. 调试更简单
2. /data 挂载一致
3. 快照边界清晰
4. 不需要立即处理多容器之间的启动顺序和状态聚合
```

但数据模型必须从第一天支持多 RuntimeUnit，避免后续重构。

#### 第二优先级：我的世界

用于验证更通用的单进程服务端模型。

适配重点：

```text
/data 作为 server.properties、world、plugins/mods、ops、whitelist、banned 等数据边界
支持 Vanilla / Paper / Forge / Fabric 变体
模组策略优先使用 embedded 或 hybrid
```

#### 第三优先级：七日杀

适合在快照、停服最终同步、玩家上传模组流程成熟后接入。

适配重点：

```text
更严格的停服保存流程
更长的 termination grace period
更保守的自动回收策略
启动前校验存档与模组版本
```

### 22.6 公网直连网络策略

由于所有节点都有公网 IP，推荐连接模型为：

```text
玩家不经过中心代理
玩家直接连接 node_public_ip:allocated_port
```

分配流程：

```text
1. Scheduler 选择节点
2. Port Manager 在该节点分配端口
3. RuntimeDriver 以 host port 方式启动容器
4. Game API 返回 node_public_ip:port
```

端口表必须记录：

```text
node_name
public_ip
port
protocol
server_id
status
```

Docker 模式：

```text
-p public_port:container_port/udp
-p public_port:container_port/tcp
```

K8s 模式优先：

```text
hostPort 或 HostNetwork + 平台端口池
```

不建议首期使用中心 UDP 代理，因为它会增加延迟和故障面。

### 22.7 玩家上传模组策略

允许玩家上传模组时，必须区分“上传区、制品区、运行区”。

```text
上传区：临时存储，未信任
制品区：校验后的 mod artifact
运行区：节点 /mod-cache 或实例 /data/mods
```

流程：

```text
1. 玩家上传模组压缩包
2. 平台检查文件大小和类型
3. 解压到隔离临时目录
4. 检查路径穿越，例如 ../ 和绝对路径
5. 计算 checksum
6. 可选执行安全扫描
7. 生成 immutable artifact
8. 写入 mod_artifacts
9. 更新 server_mod_manifests
10. 等待实例空闲或提示需要重启
11. 重启后生效
```

玩家上传模组默认使用：

```text
embedded 或 hybrid
```

如果模组不可复用、来源不标准、会修改自身文件，则放入：

```text
/data/mods
```

并随实例快照保存。

如果模组可复用，则提升为：

```text
mod-artifacts + /mod-cache
```

### 22.8 尽量 0 丢失的数据策略

“尽量 0 丢失”不能只依赖每 5 分钟周期快照。周期快照只能降低损失窗口，不能保证接近 0。

推荐组合策略：

```text
1. 游戏内自动保存开启
2. 平台周期 save + snapshot
3. 关键事件触发 save
4. 停服最终 save + snapshot
5. 节点本地 WAL/变更日志或高频增量备份作为增强
6. 节点异常时保留本地数据，不立即清理
```

关键事件包括：

```text
玩家全部离线
管理员修改配置
模组更新
玩家上传模组
实例即将停止
节点进入 Warning/Critical
```

默认参数建议：

```yaml
snapshotIntervalSeconds: 300
saveIntervalSeconds: 120
finalSnapshotRequired: true
deleteLocalDataAfterStop: false
localDataRetentionHours: 72
```

含义：

```text
每 2 分钟触发游戏 save
每 5 分钟创建一次对象存储快照
停服必须完成最终快照
停止后本地数据至少保留 72 小时
```

对于“尽量 0 丢失”的实例，可以启用更强策略：

```yaml
dataSafetyProfile: strict
saveIntervalSeconds: 60
snapshotIntervalSeconds: 180
snapshotOnPlayerLeave: true
snapshotBeforeModChange: true
snapshotBeforeConfigChange: true
requireFinalSnapshotBeforeStop: true
```

注意：严格意义上的 0 丢失需要底层存储同步复制或游戏事务日志支持。大多数游戏服务器本身不提供真正的事务一致性，因此平台目标应定义为：

```text
正常停服：0 丢失
节点异常宕机：尽可能接近 0，取决于游戏保存机制和最近一次成功快照
```

### 22.9 本地数据保留策略

为了降低异常恢复风险，停止容器后不要立即删除节点本地 `/data`。

推荐：

```text
正常停止后保留 72 小时
迁移成功后仍保留旧节点副本，但标记为 stale
只有确认最新快照可恢复后，才允许清理
```

清理条件：

```text
1. 实例不在该节点运行
2. 该本地副本 fencing_token 小于 latest token
3. latest 快照校验成功
4. 保留时间超过阈值
```

---

## 23. 主要风险与应对

### 23.1 双写导致存档损坏

应对：

```text
server lock
fencing token
节点失联进入 Uncertain
禁止无确认情况下双启动
```

### 23.2 快照过大

应对：

```text
统一 /data，但支持 exclude
模组本体缓存化
日志可选归档
增量快照作为后续优化
```

### 23.3 游戏保存脚本不可靠

应对：

```text
每个 Game Adapter 必须测试 save/stop
失败时阻止删除数据
保留旧快照
暴露 Failed 状态和告警
```

### 23.4 模组版本不兼容

应对：

```text
模组版本不可变
checksum 校验
快照记录 mod_dependencies
更新前创建备份快照
支持回滚
```

### 23.5 K8s hostPort 调度复杂

应对：

```text
平台侧端口池预分配
创建 Pod 前确定节点和端口
使用 nodeSelector/affinity 固定节点
```

---

## 24. 未决问题

以下问题需要根据你的业务进一步确定：

1. 第一批要支持哪些游戏？
2. 每个游戏服是否面向公网直连？
3. 节点是否都有公网 IP？
4. 是否需要多地区调度？
5. 玩家是否允许自行上传模组？
6. 是否有租户隔离需求？
7. 存档允许最大丢失窗口是多少？例如 5 分钟、10 分钟或必须 0 丢失。
8. 是否允许节点压力严重时强制踢出空闲但未完全停止的实例？
9. 是否要求支持 Windows 游戏服务器？
10. 是否需要 Web 控制台？

---

## 25. 推荐默认参数

```yaml
idleTimeoutSeconds: 600
snapshotIntervalSeconds: 300
stopGracePeriodSeconds: 120
lockLeaseSeconds: 60
nodeHeartbeatSeconds: 10
nodeHeartbeatTimeoutSeconds: 45
snapshotCompression: zstd
snapshotRetention:
  latest: 10
  hourly: 24
  daily: 7
modStoragePolicy: hybrid
storageMode: separated
```

---

## 26. 最终推荐方案

推荐长期方案：

```text
Kubernetes + Rust GameServer Controller + Rust Game API + PostgreSQL + MinIO/S3 + 可选 Redis
```

推荐 MVP 方案：

```text
Rust Game API + Rust Scheduler + Rust Node Agent + Docker Engine API + PostgreSQL + MinIO
```

核心抽象：

```text
GameServer = 一个状态型游戏服实例
/data = 实例可恢复边界
Game Adapter = 游戏差异适配层
Mod Artifact = 不可变模组资产
Snapshot = /data 的版本化备份
Lock + Fencing Token = 防止双写
```

系统最重要的设计目标不是让容器随意迁移，而是：

```text
活跃实例稳定运行
空闲实例优雅释放资源
实例数据可恢复
模组依赖可复现
节点资源可被业务调度器感知
```

