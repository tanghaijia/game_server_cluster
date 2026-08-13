# Controller-Go HTTP API 文档

> 游戏服务器集群控制面（controller-go）对外提供的 HTTP 接口。
> 更新日期：与 `internal/handler` 最新代码同步。

## 1. 基础信息

| 项 | 值 |
| --- | --- |
| 基础地址 | `http://<host>:8080`（`HTTP_PORT` 环境变量，默认 8080） |
| 内容类型 | 请求 `application/json`，响应 `application/json` |
| 认证 | 无（当前版本未实现鉴权，仅限内网/调试使用） |
| 数据库 | PostgreSQL（controller 本地表为读路径权威；Game 写操作同步 asset_service） |

### 通用错误格式

所有非 2xx 响应统一为：

```json
{ "error": "错误描述" }
```

### 通用状态码

| 状态码 | 含义 |
| --- | --- |
| 200 | 成功 |
| 201 | 创建成功 |
| 400 | 请求体非法 / 缺少必填字段 / 查询参数非法 |
| 404 | 资源不存在（`gorm.ErrRecordNotFound`） |
| 409 | 状态冲突（如对非 failed 实例 retry、对运行中实例 delete） |
| 500 | 服务端错误（内部错误、下游 asset_service / node_agent 调用失败等） |

---

## 2. 数据模型

> 实体未定义 `json` tag，JSON 字段名与 Go 字段名一致（PascalCase）。

### 2.1 Game 游戏

```json
{
  "ID": "343050",
  "Name": "7 Days to Die",
  "AppId": "343050",
  "ContainerConfigID": "cfg-xxx"
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| ID | string | 游戏 ID（= AppId） |
| Name | string | 游戏名称 |
| AppId | string | Steam App ID |
| ContainerConfigID | string | 关联的容器配置 ID（controller 本地维护） |

### 2.2 GameInstance 游戏实例

```json
{
  "ID": "inst-4c9f2a1b8d0e3f7a",
  "GameID": "343050",
  "NodeAgentID": "node-agent-1",
  "Status": "running",
  "LastPendingTime": "2026-01-01T00:00:00Z",
  "CreateTime": "2026-01-01T00:00:00Z",
  "UpdateTime": "2026-01-01T00:00:00Z",
  "GameBuildId": "123456"
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| ID | string | 实例 ID（`inst-` + 16 位 hex） |
| GameID | string | 所属游戏 ID |
| NodeAgentID | string\|null | 调度到的 node_agent ID；未调度/已停止时为 `null` |
| Status | string | 实例状态，见 [2.8 实例状态机](#28-实例状态机) |
| LastPendingTime | string | 最近一次进入 pending 的时间（RFC3339） |
| CreateTime / UpdateTime | string | 创建 / 更新时间（RFC3339） |
| GameBuildId | string | 解析到的游戏构建 ID |

### 2.3 Node 服务器节点

```json
{
  "Id": 1,
  "Ip": "192.168.1.10",
  "CoreNum": 16,
  "CoreFrequency": 3.5,
  "MemorySize": 65536,
  "StorageSize": 1048576,
  "Location": "cn-east-1",
  "ServiceProvider": "alicloud"
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| Id | number | 自增主键 |
| Ip | string | 节点 IP |
| CoreNum / CoreFrequency / MemorySize / StorageSize | number | 节点规格（核心数 / 主频 GHz / 内存 MB / 存储 MB） |
| Location | string | 地域 |
| ServiceProvider | string | 云服务商 |

### 2.4 NodeAgent 节点代理

```json
{
  "ID": "node-agent-1",
  "NodeId": "1",
  "Port": 9090,
  "Status": 1
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| ID | string | 主键（唯一名称） |
| NodeId | string | 所属节点 ID（对应 Node.Id） |
| Port | number | node_agent gRPC 端口 |
| Status | number | `0`=Disabled（停用） `1`=Enabled（启用） |

> 只有 **Enabled** 的 node_agent 才会参与实例调度与游戏缓存循环。

### 2.5 ContainerPortMapping 端口映射

```json
{
  "ID": "pm-xxx",
  "InstanceId": "inst-4c9f2a1b8d0e3f7a",
  "NodeAgentId": "node-agent-1",
  "HostPort": 50100,
  "ContainerPort": 26900,
  "Protocol": 0
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| ID | string | 映射 ID |
| InstanceId | string | 实例 ID |
| NodeAgentId | string | node_agent ID |
| HostPort / ContainerPort | number | 宿主端口 / 容器端口 |
| Protocol | number | `0`=TCP `1`=UDP |

### 2.6 SteamBranch 游戏分支

```json
{
  "Id": "343050:public",
  "BranchName": "public",
  "LastBuildId": 123456,
  "Description": "",
  "GameId": "343050",
  "Status": 1,
  "CreateTime": "2026-01-01T00:00:00Z",
  "UpdateTime": "2026-01-01T00:00:00Z"
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| Id | string | 主键（`game_id:branch_name`） |
| BranchName | string | Steam 分支名 |
| LastBuildId | number | 最新构建 ID |
| Description | string | 分支描述 |
| GameId | string | 所属游戏 ID |
| Status | number | `0`=Disable `1`=Enable `2`=Abandoned（废弃） |
| CreateTime / UpdateTime | string | 创建 / 更新时间（RFC3339） |

### 2.7 GameCache 节点缓存（node_agent 上报）

```json
{
  "game_id": "343050",
  "branch_name": "public",
  "build_id": "123456",
  "status": 1,
  "path": "/data/cache/343050/public",
  "download_progress": 0.85,
  "create_time": "2026-01-01T00:00:00Z",
  "update_time": "2026-01-01T00:00:00Z"
}
```

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| game_id | string | 游戏 ID |
| branch_name | string | 分支名 |
| build_id | string | 节点上缓存的构建 ID（字符串） |
| status | number | `0`=DOWNLOADING `1`=AVAILABLE `2`=REMOVED `3`=UNAVAILABLE |
| path | string | 缓存路径（未设置时省略） |
| download_progress | number | 下载进度 0~1（未设置时省略） |
| create_time / update_time | string | 创建 / 更新时间（未设置时省略） |

### 2.8 实例状态机

状态字符串（`GET /api/game-instances` 的 `status` 过滤参数与响应 `Status` 字段取值一致）：

```text
pending → scheduling → preparing_build → restoring_snapshot → starting → running
running / failed → stopping → cleaning → stopped
任意阶段失败 → failed（可通过 retry 回到 pending）
```

| 状态 | 说明 |
| --- | --- |
| `pending` | 等待调度（start 后的初始态） |
| `scheduling` | 调度中（选择 node_agent、分配端口） |
| `preparing_build` | 在 node_agent 上准备游戏构建 |
| `restoring_snapshot` | 还原快照（无快照则跳过） |
| `starting` | 启动游戏进程 |
| `running` | 运行中（终态） |
| `stopping` | 停止中 |
| `cleaning` | 清理实例资源 |
| `stopped` | 已停止（终态） |
| `failed` | 失败（可重试） |

---

## 3. 接口总览

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/games` | 创建游戏 |
| GET | `/api/games` | 列出游戏 |
| GET | `/api/games/:id` | 查询游戏 |
| PUT | `/api/games/:id` | 更新游戏 |
| DELETE | `/api/games/:id` | 删除游戏 |
| POST | `/api/game-instances` | 创建实例 |
| GET | `/api/game-instances` | 列出实例（按状态过滤） |
| GET | `/api/game-instances/:id` | 查询实例 |
| GET | `/api/game-instances/:id/ports` | 查询实例端口映射 |
| POST | `/api/game-instances/:id/start` | 启动实例 |
| POST | `/api/game-instances/:id/stop` | 停止实例 |
| POST | `/api/game-instances/:id/retry` | 重试失败实例 |
| POST | `/api/game-instances/:id/dispatch` | 强制入队调度（调试） |
| DELETE | `/api/game-instances/:id` | 删除实例 |
| POST | `/api/nodes` | 创建节点 |
| GET | `/api/nodes` | 列出节点 |
| GET | `/api/nodes/:id` | 查询节点 |
| POST | `/api/node-agents` | 创建 node_agent |
| GET | `/api/node-agents` | 列出 node_agent |
| POST | `/api/node-agents/:id/enable` | 启用 node_agent |
| POST | `/api/node-agents/:id/disable` | 停用 node_agent |
| GET | `/api/games/:id/branches` | 列出游戏分支（调试） |
| POST | `/api/games/:id/branches/sync` | 手动同步分支（调试） |
| POST | `/api/games/:id/branches/:branch/cache` | 手动触发节点缓存更新（调试） |
| GET | `/api/node-agents/:id/cache` | 查询节点缓存状态（调试） |
| GET | `/healthz` | 存活探针 |
| GET | `/debug/version` | 构建信息 |
| GET | `/debug/reconcile` | 调度器状态（调试） |
| POST | `/debug/reconcile/recover` | 手动触发调度恢复（调试） |
| GET | `/debug/instances` | 实例聚合视图（调试） |
| GET | `/debug/pprof/*` | Go pprof 性能分析（调试） |

---

## 4. Game 接口

### 4.1 创建游戏

`POST /api/games`

写操作会先同步到 asset_service（同步目标必须成功），再落本地库。

请求体：

```json
{
  "name": "7 Days to Die",
  "app_id": "343050"
}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| name | 是 | 游戏名称 |
| app_id | 否 | Steam App ID（游戏 ID = app_id） |

响应 `201`：

```json
{
  "ID": "343050",
  "Name": "7 Days to Die",
  "AppId": "343050",
  "ContainerConfigID": ""
}
```

错误：`400`（缺少 name / 请求体非法）、`500`（asset_service 同步失败或落库失败）。

### 4.2 列出游戏

`GET /api/games`

响应 `200`：

```json
{ "games": [ { "ID": "343050", "Name": "7 Days to Die", "AppId": "343050", "ContainerConfigID": "" } ] }
```

### 4.3 查询游戏

`GET /api/games/:id`

响应 `200`：Game 对象（见 4.1）。错误：`404`（不存在）。

### 4.4 更新游戏

`PUT /api/games/:id`

请求体：同 4.1（`name` 必填）。写操作先同步 asset_service 再更新本地库（保留本地字段如 `ContainerConfigID`）。

响应 `200`：更新后的 Game 对象。错误：`400`、`500`。

### 4.5 删除游戏

`DELETE /api/games/:id`

写操作先同步 asset_service 删除，再级联删除本地 steam_branches 记录与 game 记录。

响应 `200`：

```json
{ "message": "deleted" }
```

错误：`500`（asset_service 删除失败或本地删除失败）。

---

## 5. GameInstance 接口

### 5.1 创建实例

`POST /api/game-instances`

请求体：

```json
{
  "game_id": "343050",
  "game_build_id": "123456"
}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| game_id | 是 | 游戏 ID |
| game_build_id | 否 | 构建 ID；不传时以 `public` channel 向 asset_service 解析最新可用构建 |

响应 `201`：GameInstance 对象，初始 `Status: "stopped"`。

错误：`400`（缺少 game_id / 请求体非法）、`500`（构建解析失败或落库失败）。

### 5.2 列出实例

`GET /api/game-instances`

查询参数：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| status | 否 | 按状态过滤，取值见 [2.6 实例状态机](#26-实例状态机)，如 `?status=running`；不传返回全部 |

响应 `200`：

```json
{ "instances": [ { "ID": "inst-xxx", "GameID": "343050", "NodeAgentID": null, "Status": "stopped", "LastPendingTime": "0001-01-01T00:00:00Z", "CreateTime": "2026-01-01T00:00:00Z", "UpdateTime": "2026-01-01T00:00:00Z", "GameBuildId": "123456" } ] }
```

错误：`400`（status 参数非法）。

### 5.3 查询实例

`GET /api/game-instances/:id`

响应 `200`：GameInstance 对象。错误：`404`。

### 5.4 查询实例端口映射

`GET /api/game-instances/:id/ports`

响应 `200`：

```json
{
  "instance_id": "inst-xxx",
  "ports": [ { "ID": "pm-xxx", "InstanceId": "inst-xxx", "NodeAgentId": "node-agent-1", "HostPort": 50100, "ContainerPort": 26900, "Protocol": 0 } ]
}
```

### 5.5 启动实例

`POST /api/game-instances/:id/start`

仅 `stopped` / `failed` 状态可启动，成功后状态置为 `pending` 进入调度。

响应 `200`：

```json
{ "message": "started" }
```

错误：`404`、`500`（状态非法，如对 running 实例 start，错误信息含当前状态）。

### 5.6 停止实例

`POST /api/game-instances/:id/stop`

仅 `running` / `failed` 状态可停止，成功后状态置为 `stopping` 进入调度。

响应 `200`：

```json
{ "message": "stopping" }
```

错误：`404`、`500`（状态非法）。

### 5.7 重试失败实例

`POST /api/game-instances/:id/retry`

仅 `failed` 状态可重试：状态置为 `pending` 重新入队调度。

响应 `200`：

```json
{ "message": "retrying" }
```

错误：`404`、`409`（实例非 failed 状态）。

### 5.8 强制入队调度（调试）

`POST /api/game-instances/:id/dispatch`

跳过状态校验，将实例按**当前状态原样**压入调度队列。用于实例卡在中间态但未被消费时强制重新调度。

响应 `200`：

```json
{ "message": "dispatched" }
```

错误：`404`、`500`。

### 5.9 删除实例

`DELETE /api/game-instances/:id`

仅非调度中 / 非运行中状态可删除（如 `stopped`、`failed`）。删除时同步清理该实例的端口映射记录。

响应 `200`：

```json
{ "message": "deleted" }
```

错误：`404`、`409`（实例处于调度中/运行中，需先 stop）。

---

## 6. Node 接口

### 6.1 创建节点

`POST /api/nodes`

请求体：

```json
{ "ip": "192.168.1.10" }
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| ip | 是 | 节点 IP |

响应 `201`：Node 对象（`Id` 为自增主键）。错误：`400`、`500`。

### 6.2 列出节点

`GET /api/nodes`

响应 `200`：

```json
{ "nodes": [ { "Id": 1, "Ip": "192.168.1.10", "CoreNum": 0, "CoreFrequency": 0, "MemorySize": 0, "StorageSize": 0, "Location": "", "ServiceProvider": "" } ] }
```

### 6.3 查询节点

`GET /api/nodes/:id`

响应 `200`：Node 对象。错误：`404`。

---

## 7. NodeAgent 接口

### 7.1 创建 node_agent

`POST /api/node-agents`

请求体：

```json
{
  "name": "node-agent-1",
  "node_id": "1",
  "port": 9090
}
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| name | 是 | node_agent 唯一名称（主键） |
| node_id | 否 | 所属节点 ID（对应 Node.Id） |
| port | 否 | gRPC 端口，默认 `9090` |

创建后默认 `Status: 1`（Enabled）。

响应 `201`：NodeAgent 对象。错误：`400`、`500`。

### 7.2 列出 node_agent

`GET /api/node-agents`

响应 `200`：

```json
{ "node_agents": [ { "ID": "node-agent-1", "NodeId": "1", "Port": 9090, "Status": 1 } ] }
```

### 7.3 启用 / 停用 node_agent

`POST /api/node-agents/:id/enable` ／ `POST /api/node-agents/:id/disable`

启用后进入实例调度与游戏缓存循环的候选池；停用后退出。

响应 `200`：更新后的 NodeAgent 对象（`Status` 分别为 `1` / `0`）。错误：`404`。

---

## 8. 分支与游戏缓存接口

> 提供 GameCacheManager 后台循环的"手动触发"与"状态可见"能力，便于调试分支同步与缓存下载问题。

### 8.1 列出游戏分支

`GET /api/games/:id/branches`

响应 `200`：

```json
{
  "game_id": "343050",
  "branches": [
    { "Id": "343050:public", "BranchName": "public", "LastBuildId": 123456, "Description": "", "GameId": "343050", "Status": 1, "CreateTime": "...", "UpdateTime": "..." }
  ]
}
```

错误：`500`。

### 8.2 手动同步分支

`POST /api/games/:id/branches/sync`

从 asset_service 拉取该 game 的全部分支并记录到本地表（新增 / 更新构建信息 / 标记废弃）。等价于后台缓存循环的一步分支同步。

响应 `200`：

```json
{ "message": "synced" }
```

错误：`500`（asset_service 拉取失败或落库失败）。

### 8.3 手动触发节点缓存更新

`POST /api/games/:id/branches/:branch/cache`

请求体：

```json
{ "node_agent_id": "node-agent-1" }
```

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| node_agent_id | 是 | 目标 node_agent ID |

按本地分支记录的最新构建版本（`LastBuildId`）在指定 node_agent 上执行缓存检查，必要时触发下载。语义与后台循环一致：缓存已最新 / 正在下载 → 幂等成功；节点缓存不可用等 → 错误。

响应 `200`：

```json
{ "message": "cache ok" }
```

错误：`400`（缺 node_agent_id）、`404`（分支未同步，需先 sync）、`500`。

### 8.4 查询节点缓存状态

`GET /api/node-agents/:id/cache`

查询参数：

| 参数 | 必填 | 说明 |
| --- | --- | --- |
| game_id | 是 | 游戏 ID |
| branch | 否 | 分支名，缺省 `public` |

响应 `200`：

```json
{
  "node_agent_id": "node-agent-1",
  "game_id": "343050",
  "branch": "public",
  "cache": {
    "game_id": "343050",
    "branch_name": "public",
    "build_id": "123456",
    "status": 1,
    "path": "/data/cache/343050/public",
    "download_progress": 0.85,
    "create_time": "...",
    "update_time": "..."
  }
}
```

> 节点上无该 (game, branch) 缓存时 `cache` 为 `null`；`status` 枚举见 [2.7 GameCache 节点缓存](#27-gamecache-节点缓存node_agent-上报)。

错误：`404`（node_agent 不存在）、`500`。

---

## 9. 健康检查与调试接口

### 9.1 存活探针

`GET /healthz`

响应 `200`：

```json
{ "status": "ok" }
```

### 9.2 构建信息

`GET /debug/version`

响应 `200`：

```json
{
  "go_version": "go1.26.4",
  "module_path": "controller-go",
  "module_version": "(devel)"
}
```

### 9.3 调度器状态

`GET /debug/reconcile`

返回调度器运行状态：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| queue_len | number | 当前调度队列长度 |
| retry_counts | object | 各实例当前自动重试次数（`instance_id → count`） |
| dispatchable_instances | array | 处于中间态（待调度）的实例列表 |
| scheduler | object | 调度器状态（`type`、`node_ids`、`counter` 轮询游标） |

响应 `200` 示例：

```json
{
  "queue_len": 0,
  "retry_counts": { "inst-xxx": 2 },
  "dispatchable_instances": [],
  "scheduler": { "type": "simple", "node_ids": ["node-agent-1"], "counter": 0 }
}
```

### 9.4 手动触发调度恢复

`POST /debug/reconcile/recover`

把数据库中所有中间态实例重新入队（等价于进程重启后的 Recover 流程，免重启）。

响应 `200`：

```json
{ "message": "recovered", "queue_len": 2 }
```

### 9.5 实例聚合视图

`GET /debug/instances`

返回全部实例 + 端口映射 + node_agent 地址，一次看清 DB 状态与真实调度结果的对应关系。

响应 `200`：

```json
{
  "instances": [
    {
      "instance": { "ID": "inst-xxx", "GameID": "343050", "NodeAgentID": "node-agent-1", "Status": "running", "LastPendingTime": "...", "CreateTime": "...", "UpdateTime": "...", "GameBuildId": "123456" },
      "ports": [ { "ID": "pm-xxx", "InstanceId": "inst-xxx", "NodeAgentId": "node-agent-1", "HostPort": 50100, "ContainerPort": 26900, "Protocol": 0 } ],
      "node_agent": { "id": "node-agent-1", "node_id": "1", "port": 9090, "status": 1, "addr": "192.168.1.10:9090" }
    }
  ]
}
```

> 未调度的实例 `node_agent` 为 `null`；解析 node_agent 地址失败时返回 `error` / `addr_error` 字段。

### 9.6 Go pprof 性能分析

`GET /debug/pprof/*`

Go 标准 `net/http/pprof` 接口，常用：

| 路径 | 说明 |
| --- | --- |
| `/debug/pprof/` | 索引页 |
| `/debug/pprof/goroutine?debug=1` | goroutine 堆栈（排查泄漏） |
| `/debug/pprof/heap?debug=1` | 堆内存 |
| `/debug/pprof/profile?seconds=30` | CPU profile |
| `/debug/pprof/trace?seconds=5` | 执行 trace |

---

## 10. 快速示例（curl）

```bash
BASE=http://127.0.0.1:8080

# 创建游戏
curl -X POST $BASE/api/games -H 'Content-Type: application/json' -d '{"name":"7 Days to Die","app_id":"343050"}'

# 创建实例
curl -X POST $BASE/api/game-instances -H 'Content-Type: application/json' -d '{"game_id":"343050"}'

# 启动 / 停止 / 重试
curl -X POST $BASE/api/game-instances/inst-xxx/start
curl -X POST $BASE/api/game-instances/inst-xxx/stop
curl -X POST $BASE/api/game-instances/inst-xxx/retry

# 列表与过滤
curl $BASE/api/game-instances
curl '$BASE/api/game-instances?status=running'

# 节点与 node_agent
curl -X POST $BASE/api/nodes -H 'Content-Type: application/json' -d '{"ip":"192.168.1.10"}'
curl -X POST $BASE/api/node-agents -H 'Content-Type: application/json' -d '{"name":"node-agent-1","node_id":"1"}'
curl -X POST $BASE/api/node-agents/node-agent-1/disable

# 分支与缓存（调试）
curl $BASE/api/games/343050/branches
curl -X POST $BASE/api/games/343050/branches/sync
curl -X POST $BASE/api/games/343050/branches/public/cache -H 'Content-Type: application/json' -d '{"node_agent_id":"node-agent-1"}'
curl '$BASE/api/node-agents/node-agent-1/cache?game_id=343050&branch=public'

# 调试
curl $BASE/healthz
curl $BASE/debug/reconcile
curl -X POST $BASE/debug/reconcile/recover
curl $BASE/debug/instances
```
