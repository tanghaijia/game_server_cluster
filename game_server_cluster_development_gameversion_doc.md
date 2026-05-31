# 多游戏 Docker/Kubernetes 游戏服务器集群调度系统开发文档（更新版）

## 27. 游戏多版本选择与节点缓存

针对多版本选择与节点缓存问题，建议如下：

### 27.1 版本模型

多版本必须作为一等对象建模，避免存档兼容、模组兼容、节点缓存、回滚、迁移出现问题。推荐把版本分成四层：

```text
Game Adapter Version   平台适配器版本
Game Build Version     游戏服务端版本
Mod Version            模组版本
Instance Data Version  实例数据/存档版本
```

实例依赖组合为：

```text
adapter_version + game_build + mod_manifest + data_snapshot
```

禁止直接使用 `latest`。用户界面可以展示“最新版”，但后端必须解析成不可变 `game_build`。例如：

```json
{
  "game": "dst",
  "channel": "public",
  "game_build": "676042",
  "resolved_at": "2026-05-29T20:00:00+08:00"
}
```

---

### 27.2 GameVersion 与 GameBuild

- **GameVersion**: 用户可选择的高层版本，如 DST public latest、Minecraft 1.21.1 Paper。
- **GameBuild**: 平台实际运行的不可变构建，如 `dst-public-676042` 或 `paper-1.21.1-130`。调度和节点缓存以 GameBuild 为单位。
- **GameChannel**: 更新通道，例如 stable、public、beta、experimental。用户选择通道后，平台解析为具体 GameBuild。

---

### 27.3 实例绑定具体 build

实例必须记录：

```json
{
  "server_id": "dst-1001",
  "game": "dst",
  "desired_game_build": "dst-public-676042",
  "current_game_build": "dst-public-676042"
}
```

升级流程：

1. 检测新版本
2. 创建升级计划
3. 检查模组兼容性
4. 创建升级前快照
5. 停服并最终保存
6. 切换 desired_game_build
7. 准备新 build
8. 启动实例
9. 健康检查
10. 升级成功后更新状态

失败时回滚到**旧 build + 升级前快照**。

---

### 27.4 节点缓存策略

- 每个节点只缓存它实际需要运行的 GameBuild。
- 节点缓存路径示例：

```text
/srv/game-cache/{game}/{build_id}/
```

- 容器启动时挂载：

```bash
-v /srv/game-cache/dst/dst-public-676042:/server:ro
```

- 调度器会考虑缓存命中，节点已有 build 的启动更快。
- 节点缓存是按需、可清理、可预热的本地热缓存，不必全量保存每个游戏的每个版本。
- 节点缓存清理策略：

  1. 正在被运行实例使用的 build 不可删除
  2. pinned 的 build 不可删除
  3. 最近使用优先保留
  4. 磁盘压力大时清理最久未使用且 ref_count = 0 的 build
  5. 中心缓存存在的 build 可安全清理本地副本

---

### 27.5 中心缓存

- 中心缓存用于版本构建 artifact，例如 MinIO/S3：
  
```text
game-builds/dst/dst-public-676042/server.tar.zst
game-builds/minecraft/paper-1.21.1-130/server.jar
```

- 节点准备 GameBuild 时优先：

  1. 本地缓存已有 → 直接使用
  2. 中心缓存已有 → 从中心拉取
  3. 中心缓存无 → SteamCMD 下载，完成后上传中心缓存

---

### 27.6 三层缓存模型

```text
1. Registry 镜像仓库：存 base image + adapter image
2. Build Artifact Store / 中心版本仓库：存游戏服务端构建包
3. Node Local Cache / 节点本地缓存：存当前节点实际运行或预热的 GameBuild
```

---

### 27.7 多版本选择策略

1. **固定版本模式**（默认推荐）  
   用户选择具体 GameBuild，实例固定该 build，升级必须显式流程。

2. **跟随通道模式**  
   用户选择 channel（如 public），平台解析为具体 build，升级需预先确认并快照。

3. **自定义版本模式**  
   高级用户上传或指定构建，灵活但风险高，需要隔离和校验。

---

### 27.8 DST 特殊处理

- DST 服务端通过 SteamCMD 下载，版本选择涉及 app_id、branch、build_id。
- 建议第一版仅支持 public channel，落库时记录解析出的 build_id。
- Node Agent PrepareGameBuild 负责下载和准备 game_build，StartInstance 只使用已准备好的 `/server`。
- Adapter 版本和游戏版本解耦：adapter_image 表示脚本能力，game_build 表示具体游戏版本。

---

### 27.9 MVP 最小实现

1. 支持 DST public channel。
2. 启动前解析成具体 build_id。
3. 实例绑定 build_id。
4. 节点缓存按需 PrepareGameBuild。
5. 重启时继续使用原 build_id。
6. 升级必须显式操作，前置快照，保证可回滚。
7. 不要求每个节点保存所有游戏版本。

