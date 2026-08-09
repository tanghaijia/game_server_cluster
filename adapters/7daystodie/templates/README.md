# 7 Days to Die Templates

这是一套 7 Days to Die 专用服务器（Steam AppID 294420）的最小模板，用于容器化运行：

- 单实例专用服务器（单进程，无 DST 那样的 Master/Caves 分片）
- `/data` 作为持久化目录（存档、世界、日志、Mods、serverconfig.xml）
- 无模组
- 玩家通过 UDP/TCP 26900 端口连接

## 预期运行参数

容器启动服务器（由 `start.sh` 执行）：

```bash
7DaysToDieServer.x86_64 \
  -configfile=/data/serverconfig.xml \
  -logfile /dev/stdout \
  -quit -batchmode -nographics -dedicated
```

- `/server`：只读挂载的游戏安装目录（由 node_agent 用 steamcmd 下载 AppID 294420 到此目录）
- `/data`：可写持久化目录，`start.sh` 首次启动把模板复制过来（`copy_if_missing`，重启保留用户修改）

## 预期生成到 /data 的结构

```text
/data/
  serverconfig.xml                 # 服务器配置（-configfile 指向此处）
  7DaysToDie/
    Saves/
      serveradmin.xml              # 管理员/用户/组配置
      <GameName>...                # 各世界存档、RWG 生成的世界
    Mods/                          # 模组放这里（UserDataFolder 下自动识别）
    logs/                          # 服务器日志
```

## 环境变量（均可覆盖，供平台后续注入）

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `SDTD_DATA_ROOT` | `/data` | 持久化数据根目录 |
| `SDTD_HOME` | `${SDTD_DATA_ROOT}` | 容器 HOME（Unity PlayerPrefs / EOS DeviceId 凭据目录，必须可写） |
| `SDTD_SERVER_ROOT` | `/server` | 只读游戏安装目录 |
| `SDTD_CONFIG_FILE` | `/data/serverconfig.xml` | `-configfile` 指向的配置路径 |
| `SDTD_USER_DATA` | `/data/7DaysToDie` | 对应 XML 中 `UserDataFolder` |
| `SDTD_TELNET_PORT` | `8081` | Telnet 端口（save/stop 用） |
| `SDTD_BIN` | 自动查找 | 覆盖服务器二进制路径 |
| `SDTD_DEBUG` | `0` | 设为 1 开启 bash -x |

> 前缀用 `SDTD_`（字母开头）：bash 参数展开 `${VAR:-default}` 不允许以数字开头的变量名（如 `7DTD_`）。

## 端口

| 端口 | 协议 | 用途 |
|------|------|------|
| 26900 | TCP | 游戏连接 |
| 26900-26902 | UDP | 游戏数据（LiteNetLib） |
| 8081 | TCP | Telnet（平台 save/shutdown 用） |
| 8080 | TCP | Web 控制台（默认关闭） |

## 容器化注意事项

- **EACEnabled 默认 false**：EAC 组件需要写入安装目录，而 `/server` 只读，开启会导致启动失败。
- **SteamNetworking 已禁用**：其端口（25000-25002）与 P2P 特性不适合 NAT 容器映射，保留 LiteNetLib 即可。
- **必须加 `-logfile /dev/stdout`**：否则 Unity 默认往安装目录写日志（只读会失败）。
- **首次启动会生成世界**（尤其 `GameWorld=RWG`），耗时几分钟属正常，不要中断。
