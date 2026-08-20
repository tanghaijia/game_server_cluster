# dsh-tools / gcdebug — game-server-cluster 调试工具集（DSH 动态插件）

让 DSH（DeepSeek Harness）具备直接**调用与观察** game-server-cluster 控制面的能力：

- 按 [`controller-go/docs/api.md`](../controller-go/docs/api.md) 调用 controller-go 的全部 HTTP 接口（观察 + 控制）；
- 直接连接 PostgreSQL 观察/编辑 controller 的本地权威表（games、game_instances、nodes、node_agents、
  game_container_configs、game_container_port_mappings、steam_branches、scheduling_queue、
  scheduler_events、node_resource_samples …）。

## 组成

| 文件 | 作用 |
| --- | --- |
| `gcdebug-bridge.js` | 执行桥（Node 子进程程序）：HTTP fetch + 自研 PostgreSQL v3 线协议客户端（无 npm 依赖）。由插件在每次工具调用时经 `node -e` 启动，stdin 传 `{script, request}`，stdout 返回单个 JSON。 |
| 动态插件 `gdbg-1` | 注册 4 个模型工具：`controller_api` / `pg_query` / `pg_exec` / `pg_meta`。桥接文件按会话工作区（`workspaceRegistry`）定位读取。 |

## 工具

### controller_api — 调用 controller HTTP 接口
参数：`method`（默认 GET）、`path`（支持 `{param}` 占位，如 `/api/game-instances/{id}/start`）、
`path_params`、`query`、`body`（对象自动 JSON 序列化）、`headers`、`base_url`（默认
`http://127.0.0.1:8090`，本机实际部署常为 `8088`）、`timeout_ms`。
返回 `{ ok, status, statusText, headers, body }`；非 2xx 也正常返回（含错误信息）。

### pg_query — 只读 SQL
只允许 `SELECT / WITH / SHOW / EXPLAIN / VALUES / TABLE`；写语句返回错误提示改用 `pg_exec`。
返回 `{ ok, columns, types, rows, rowCount, truncated }`。`limit` 默认 200，超出截断并标记。

### pg_exec — 写 SQL（事务包裹）
`commit=true`（默认）提交；`commit=false` 回滚（试运行，安全验证写语句效果）。
返回 `{ ok, command, tag, rowCount, transaction }`。
⚠️ 直接改库可能绕过 controller 的状态机/调度逻辑，优先使用 `controller_api` 的正式接口。

### pg_meta — 库结构探查
不传 `table` 列出所有表（含估算行数）；传 `table` 返回列定义、总行数、样例数据（`sample=false` 关闭）。

## 连接配置（默认值已按本机部署设置）

- HTTP 基础地址默认 `http://127.0.0.1:8088`（本机 controller 实际运行端口；其他部署可用 `base_url` 覆盖）。
- PostgreSQL 默认 `myuser@127.0.0.1:5432/cluster_game_server_db`（schema `controller`）——
  即本机真实数据库。其它环境可用 `db: { host, port, user, password, database, schema }` 覆盖（缺省合并）。

## 使用

插件为动态 Cordis 插件（本会话进程内）。定义后 `cordis_run` 激活，工具即出现在模型工具集中，
默认连接已指向本机真实 controller（`:8088`）与数据库，通常无需再传连接参数。示例：

```
controller_api  { "path": "/api/game-instances", "query": { "status": "running" } }
controller_api  { "path": "/api/game-instances/{id}/retry", "path_params": { "id": "inst-xxx" }, "method": "POST" }
pg_meta         {}
pg_query        { "sql": "SELECT id, status, fail_reason FROM controller.game_instances ORDER BY update_time DESC LIMIT 10" }
pg_exec         { "sql": "UPDATE controller.games SET name = $1 WHERE id = $2", "params": ["新名字", "343050"], "commit": false }
```

## 实现说明

- **传输**：宿主插件在受限沙箱中运行，无 `require`/`Buffer`/TCP。因此所有网络工作由
  `ctx.subprocess.spawn(['node', '-e', bootstrap])` 启动的独立 Node 子进程完成；
  stdin 传载荷（collect 模式），stdout 收集 JSON（collect 模式 + spill），不触碰被沙箱禁止的 raw pipe。
- **PostgreSQL 客户端**：`gcdebug-bridge.js` 直接实现 v3 线协议 —— Startup(3.0)、
  cleartext / MD5 / SCRAM-SHA-256 认证、扩展查询协议（Parse/Bind/Describe/Execute/Sync，支持 `$n` 参数）、
  RowDescription/DataRow/CommandComplete 解析、常见类型（bool/int/float/numeric/json/jsonb/时间戳）转换。
  无任何 npm 依赖，可在任意有 node 的机器上独立运行（`node gcdebug-bridge.js` 配合 bootstrap 即可单测）。
- **修改桥接逻辑**：编辑 `gcdebug-bridge.js` 后，重启插件（`cordis_run` 同包重跑）即可生效 ——
  插件每次执行都从工作区实时读取桥接文件。
