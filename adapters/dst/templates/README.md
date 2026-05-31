# DST Templates

这是一套 Don't Starve Together Dedicated Server 的最小模板，目标是用于容器化调试：

- 一个逻辑 Cluster
- 两个 shard：Master + Caves
- 无模组
- 玩家可通过 Master 的 UDP 端口连接
- `/data` 作为持久化目录

## 预期运行参数

容器启动 Master：

```bash
dontstarve_dedicated_server_nullrenderer \
  -persistent_storage_root /data \
  -conf_dir DoNotStarveTogether \
  -cluster Cluster_1 \
  -shard Master
```

容器启动 Caves：

```bash
dontstarve_dedicated_server_nullrenderer \
  -persistent_storage_root /data \
  -conf_dir DoNotStarveTogether \
  -cluster Cluster_1 \
  -shard Caves
```

## 预期生成到 /data 的结构

```text
/data/
  DoNotStarveTogether/
    Cluster_1/
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
      Caves/
        server.ini
        worldgenoverride.lua
        modoverrides.lua
```

## 必须手动提供的文件

`cluster_token.txt` 需要从 Klei 账户页面创建，不能由模板自动生成。

你可以先把 `cluster_token.txt.example` 复制成：

```text
/data/DoNotStarveTogether/Cluster_1/cluster_token.txt
```

然后填入真实 token。

## 端口

默认端口：

- Master 对外游戏端口：10999/udp
- Caves shard 端口：11000/udp
- Master/Caves 内部 shard 通信：127.0.0.1:10889

Docker 调试时建议先映射：

```bash
-p 10999:10999/udp
-p 11000:11000/udp
```
