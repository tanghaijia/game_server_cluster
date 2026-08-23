# 游戏适配路线图（Game Adapter Roadmap）

> 状态：**Draft**（评估建议稿，待决策）
> 关联：adapter-framework-design.md（适配器契约）/ multi-game-platform-design.md / docs/backlog.md（B-04 等）
> 范围：新游戏适配的**优先级与选型建议**，不含逐游戏的 adapter 实现细节

## 1. 背景与目标

系统通过「每个游戏一个适配器」扩展：`adapters/{game}/` 内是「声明（adapter.toml）+ 钩子（hooks.sh）+ 模板（templates/）+ Dockerfile」，平台侧（controller-go / node_agent / asset_service）保持游戏无关。

当前已适配：**7 Days to Die**、**Don't Starve Together（DST）**。本文回答：**下一个该适配什么游戏**，以及为什么。

目标：
1. 用最少适配成本，覆盖最大需求（国内 + 全球）；
2. 选型与平台模型（订阅篮子 / 单活跃 / S3 快照续档）强契合；
3. 把「新游戏接入」跑成一个可复制的模板，顺带打通通用探测（B-04）。

## 2. 适配价值评估框架

一个游戏「好适配」按以下标准打分（映射到本平台技术栈）：

| 维度 | 判定 | 对平台的意义 |
| --- | --- | --- |
| 有 Linux 专用服务器 + steamcmd appid | ✅ | 复用 steam_branches / asset_service / steamcmd 下载链路 |
| 纯文本配置文件 | ✅ | 好接 adapter.toml 的 config schema + config-render |
| 支持 Steam A2S（Source Query）/ RCON / 结构化日志 | ✅ | 一套通用探针解决 health/players 探测（B-04） |
| 持久世界（存档有续档价值） | ✅ | 放大 S3 快照卖点 |
| 无反作弊/DRM 阻止 headless | ✅ | 否则无法托管 |
| 资源占用（RAM/CPU/磁盘） | 低→高 | 影响单盒可售订阅数（B-07 相关） |
| 适配成本（是否有 Proton/Wine、复杂模组） | 低→高 | 影响交付周期 |

**核心结论**：最契合的是「**生存 / 建造 / 沙盒**」类——玩家「跟朋友长期玩一个档」，天然契合「一台服务器一个档 + 快照续档 + 单活跃切换」。竞技类（CS2 等）是短暂对局、无持久世界，快照价值低，且需要并行多实例，与本平台模型相悖。

## 3. 候选游戏评估

### 3.1 第一梯队（建议马上做）：Palworld、Valheim

| 游戏 | steamcmd appid | 配置 | A2S | 持久世界 | 适配成本 | 说明 |
| --- | --- | --- | --- | --- | --- | --- |
| 幻兽帕鲁 Palworld | 2394010 | PalWorldSettings.ini（纯文本） | ✅ | ✅ | 低 | 2024 现象级爆款，国内私服需求极大，快照续档价值高 |
| 英灵神殿 Valheim | 896660 | 配置极简 | ✅ | ✅ | 低 | 长青生存建造，BepInEx 模组生态成熟 |

> 两款是「需求最大 + 成本最低 + 完全命中平台模型」的组合，作为样板首选。

### 3.2 第二梯队（高价值、成本略高或需求略窄）：V Rising、Terraria、Project Zomboid

| 游戏 | steamcmd appid | 特点 | 说明 |
| --- | --- | --- | --- |
| V Rising | 1829350 | 生存建造，A2S，持久世界 | 专用服务器成熟，Linux 干净 |
| 泰拉瑞亚 Terraria | 105600 | TShock 插件生态，资源占用极小 | 国内需求强，单盒可堆多档，边际成本几乎为零 |
| 僵尸毁灭工程 Project Zomboid | 380870 | Java 服务器 + Workshop 模组，全文本配置 | 社区服需求稳定 |

### 3.3 第三梯队（需求大但有坑，第二批做）：ARK: SA、Rust、Enshrouded

| 游戏 | 坑点 |
| --- | --- |
| ARK: Survival Ascended | 资源重（RAM/CPU/磁盘大）、模组多 |
| Rust | 走 Steam 服务器浏览器 + uMod，配置复杂 |
| 雾锁王国 Enshrouded | 热度高、持久世界，但 Linux 官方服务器走 Proton/Wine，成本偏高 |

### 3.4 特例：Minecraft（单独评估，不塞第一批）

需求天花板最高，但**不是 Steam**：无 steamcmd appid，现有 `steam_branches` / `asset_service` / steamcmd 下载链路完全不适用。需为「非 Steam 产物」单独开 artifact 通道。**建议作为「非 Steam 适配器」样板单独立项，而非混入第一批 Steam 游戏。**

## 4. 与平台模型的契合度

- **生存/建造/沙盒 = 「跟朋友长期玩一个档」** → 契合订阅篮子 + S3 快照 + 单活跃切换；
- **单活跃约束**在生存类合理（一人通常只同时玩一个生存档），在竞技类反而限制并行多实例；
- **快照续档**是生存类核心价值（防炸档、换机续玩），竞技类无此需求。

## 5. 高杠杆技术建议：A2S 通用探针（联动 B-04）

第一、二梯队游戏**几乎全部支持 Steam A2S（Source Query）**。只需写**一个通用 A2S 探针**，即可一次性解决这些游戏的 `health.sh` / `players.sh` 探测（B-04），无需逐游戏单独对接。

- 参考：[steam-server-on-demand（用 A2S 做按需起停，覆盖 Enshrouded/Valheim/Palworld/V Rising）](https://github.com/jdmcgrath/steam-server-on-demand)
- 收益：`B-04（health/players 运行时接入）` 从「逐游戏适配」降为「一次实现、多游戏复用」，是适配新游戏前最值得先投入的公共件。

## 6. 建议落地顺序

1. **先做 Palworld + Valheim**：以「声明 + 钩子 + 模板 + 2 行 Dockerfile」跑通，形成「新游戏接入清单」模板；
2. **同步做 A2S 通用探针**：顺带完成 B-04；
3. **补 Terraria**（成本极低，验证资源轻量型游戏）；
4. **再评估 ARK / Rust / Enshrouded**（重资源 / Proton 成本）；
5. **单独为 Minecraft 设计非 Steam artifact 通道**。

## 7. 参考资料

- [bigiron.cc — Valheim/Palworld 与 co-op 游戏服务器托管格局（2026）](https://www.bigiron.cc/guides/valheim-palworld-and-coop-game-server-hosting-2026)
- [eastgate.host — 2025 私服生存游戏推荐](https://eastgate.host/the-7-best-survival-games-to-host-for-your-private-friend-group-in-2025/)
- [crux.supercraft.host — 生存/合作游戏后端模式：专用服务器 + 持久世界](https://crux.supercraft.host/blog/survival-coop-game-backend-patterns/)
- [jdmcgrath/steam-server-on-demand（A2S 按需起停参考实现）](https://github.com/jdmcgrath/steam-server-on-demand)

## 8. 关联 backlog

| 条目 | 关系 |
| --- | --- |
| B-04（health.sh / players.sh 运行时接入） | A2S 通用探针直接落地，多游戏复用 |
| B-07（资源预留模型，VPS 式配额） | 重资源游戏（ARK/Rust）选型受影响 |
| B-01（凭证 orphan 回收） | 若新游戏引入受限凭证（类似 DST cluster_token），沿用 M8 池机制 |
