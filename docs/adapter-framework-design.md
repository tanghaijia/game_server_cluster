# 游戏适配器公共框架设计（Adapter Framework）

> 状态：**Approved**（决策已确认）  日期：2026-08-20
> 关联：multi-game-platform-design.md / game_server_cluster_development_doc.md §8（游戏适配器规范）、§17B（分层镜像）
> 范围：`adapters/` 目录内部组织 + 平台侧（controller-go / node_agent / asset_service）与适配器的接口改造

## 1. 背景与目标

系统以「每个游戏一个完整适配器」的方式扩展：`adapters/dst/`、`adapters/7daystodie/` 各自包含完整的 Dockerfile + 生命周期脚本 + 模板。当前每新增一个游戏都要重新实现一遍，存在大量逐字重复与平台侧硬编码，公共逻辑的缺陷会以「复制粘贴」的方式传播到每个游戏。

### 1.1 目标

1. **提取公共逻辑**（脚本与代码），一处实现、版本化、可测试；
2. **最小化每类游戏的变化性**：新游戏 = 声明 + 钩子 + 模板，不复制粘贴骨架；
3. **平台零游戏知识**：消灭 controller 侧硬编码的游戏特定变量名；
4. **生命周期契约完整落地**：`start/save/stop/players/health` 五阶段全部由平台驱动；
5. **配置可运营化**：游戏配置文件中的可配置项由 schema 声明，玩家/运营方分级配置，替代模板写死。

### 1.2 现状问题盘点

#### 脚本级重复（逐字重复，已核实）

| 位置 | 重复内容 |
| --- | --- |
| `dst/scripts/prepare-runtime.sh` 与 `7daystodie/scripts/prepare-runtime.sh` | `copy_if_missing` / `copy_dir_files_if_missing` / mkdir / `find` 列文件逻辑逐字一致，仅目录布局不同 |
| `dst/scripts/start.sh` 与 `7daystodie/scripts/start.sh` | `set -Eeuo pipefail`、debug env、`log()`、`find_*_bin()`、SIGTERM trap、LD_LIBRARY_PATH 拼接、`wait $PID` 前台化——差异仅 BIN 名与启动参数 |
| `dst/scripts/health.sh` 与 `7daystodie/scripts/health.sh` | 均为 `pgrep <进程名>` + 输出 JSON，仅进程名不同 |
| `dst/scripts/stop.sh` 与 `7daystodie/scripts/stop.sh` | 均为「save → 优雅停 → killall 兜底」模板 |
| `dst/Dockerfile` 与 `7daystodie/Dockerfile` | 前半段（debian bookworm-slim + i386 依赖 + steamcmd 安装）逐字一致，base image 未落地 |

#### 代码级耦合

| 位置 | 问题 |
| --- | --- |
| `controller-go/internal/biz/reconcile_dispatcher.go:760` | 硬编码 `"SDTD_SERVER_PORT"`，新游戏须认该变量名或改平台代码 |
| `node_agent/src/ports/container_client.rs` | `ContainerClient` trait 无 `exec`；`save.sh`/`players.sh`/`health.sh` 在平台侧无调用点，stop 直接 `docker stop`（30s 后 SIGKILL） |
| `asset_service/src/domain/adapter.rs` | `GameAdapter`/`AdapterVersion`/`min_spec_version` 模型已有，但 service 层无注册/校验逻辑，`adapter_version` 为 seed 手填 |
| env 约定 | `DST_*` / `SDTD_*` 两套前缀，无公共 `GAME_*` 约定；退出码 10/20/21 为隐式魔法数字 |
| 游戏配置项 | `templates/serverconfig.xml` 约 50 个 property 值写死，用户无任何配置渠道 |

## 2. 总体设计：三层架构

```text
L1  base image（adapters/base/）
    └─ 运行环境（steamcmd/依赖）+ 公共脚本库 /scripts/lib/*.sh
       + 生命周期框架 /scripts/lifecycle/*.sh（带默认实现的 5 个入口）

L2  game adapter image（adapters/{game}/）   ← 每类游戏的变化性最小化
    └─ FROM base + adapter.toml（声明式元数据）+ hooks.sh（差异钩子）+ templates/

L3  平台侧公共代码
    └─ node_agent：通用生命周期执行器（exec 五阶段脚本）
       controller：端口注入/配置下发公共化（变量名从元数据读）
       asset_service：schema/metadata 随 GameBuild 存储与返回（无独立 adapter 实体）
```

游戏侧写的东西收敛为「**声明（adapter.toml）+ 钩子（hooks.sh）+ 模板（templates/）+ 2 行 Dockerfile**」；平台侧获得一份稳定、可测试、版本化的契约。

### 2.1 镜像依赖链

```text
adapters/base/Dockerfile  ──build──▶  game-base:2026.xx
                                            │
adapters/{game}/Dockerfile  「FROM game-base:2026.xx」  ← 继承全部 lib + lifecycle
   + COPY hooks.sh        → 镜像内 /scripts/hooks.sh
   + COPY templates/      → 镜像内 /templates/
   ──build──▶  {adapter_id}-adapter:{adapter_version}-{upstream_version}
```

游戏 Dockerfile 从约 30 行降到约 5 行；steamcmd/系统依赖只在 base 中安装一次。

### 2.2 元数据消费链

`adapter.toml` **不进镜像**，解析产物（metadata/schema）**随 GameBuild 注册携带**（收敛模型：无独立 adapter 实体），平台按需读取：

```text
adapter.toml
  ├─ lifecycle 路径 / image ──▶ node_agent：docker exec 调 save/stop/players/health
  ├─ port_inject.env ─────────▶ controller：env 变量名从这里读（消灭 SDTD_SERVER_PORT 硬编码）
  ├─ config schema ───────────▶ controller/platform-service：生成表单、校验、按实例/按游戏存储
  ├─ build.steamcmd ─────────▶ asset_service：建 GameBuild 时带 adapter_version + 镜像名 tag
  └─ ports / resources ──────▶ controller：生成 game_container_configs 默认值
```

yaml 声明的是镜像内世界（lifecycle 脚本路径、hooks 行为），平台按声明操作镜像内世界——「声明 → 执行」闭环。

## 3. 详细设计

### 3.1 公共脚本库 `/scripts/lib/*.sh`（base image 内）

每个文件单一职责、可独立测试（shellcheck + bats）：

| 库 | 内容 | 提取自 |
| --- | --- | --- |
| `lib/log.sh` | `log` / `log_error`，统一 `[phase] msg` 前缀 | 两个 start.sh 的 `log()` |
| `lib/fs.sh` | `copy_if_missing`、`copy_dir_files_if_missing`、`ensure_dir` | 两个 prepare-runtime.sh |
| `lib/env.sh` | 公共变量解析：`DATA_ROOT`/`SERVER_ROOT`/`TEMPLATE_DIR`/`GAME_HOST_PORT`，带默认值 | 各脚本环境读取 |
| `lib/process.sh` | `find_bin`、前台启动 + SIGTERM/SIGINT trap、`stop_children`、`wait_all`、分级 kill | 两个 start.sh 骨架 |
| `lib/output.sh` | `emit_health` / `emit_players`，强制 JSON 契约 `{"healthy":true,"reason":...}`、`{"players":N,"max_players":N}` | 两个 health.sh |
| `lib/port-inject.sh` | 通用端口注入：按声明规则（XML 属性/INI/envsubst/正则）改写配置文件 | 7dtd start.sh 的 sed |
| `lib/template.sh` | 模板展开（skip-if-exists，/templates → /data，含占位符校验） | prepare-runtime.sh |
| `lib/config-render.sh` | 配置渲染：按 key 合并 JSON 配置进游戏配置文件（见 §3.4） | 新增 |

### 3.2 生命周期框架：默认实现 + 钩子协议

base 提供 5 个框架脚本（`/scripts/lifecycle/`），所有游戏同一份；游戏差异集中在 `/scripts/hooks.sh`（仅函数定义，不执行），被框架 source。**框架 = 调用者，hooks = 插件**。

```text
lifecycle/start.sh   → 调 hook_pre_start / hook_find_bin / hook_start_command
lifecycle/save.sh    → 调 hook_save          （默认：空操作返回 0）
lifecycle/stop.sh    → 调 hook_graceful_stop （默认：kill -TERM）
lifecycle/health.sh  → 调 hook_health        （默认：按进程名 pgrep）
lifecycle/players.sh → 调 hook_players       （默认：{"players":0}）
```

框架 start.sh 执行序（伪代码）：

```bash
source /scripts/lib/*.sh
source /scripts/hooks.sh 2>/dev/null || true
hook_pre_start || prepare_runtime_default
BIN="$(hook_find_bin || find_bin_default)"
hook_start_command "${BIN}" &
trap stop_children SIGTERM SIGINT
wait_all
```

钩子最小集（每个游戏只需实现 2 个必选，其余可选）：

| 钩子 | 必须 | 说明 |
| --- | --- | --- |
| `hook_find_bin` / `hook_start_command` | ✅ | 唯一真正的游戏差异：BIN 名、启动参数；DST 多 shard 封装于此 |
| `hook_pre_start` | 可选 | 默认 = 模板展开；DST token 校验、7dtd steamclient.so 检查作覆盖 |
| `hook_save` | 可选 | 默认空操作；7dtd = telnet saveworld |
| `hook_graceful_stop` | 可选 | 默认 kill -TERM；7dtd = telnet shutdown |
| `hook_health` | 可选 | 默认按进程名；可换端口探测 |
| `hook_players` | 可选 | 默认 `{"players":0}` |

迁移后预期：DST 从约 250 行 shell 降到约 50 行钩子。

### 3.3 `adapter.toml`：声明式契约文件

#### 3.3.1 为什么 TOML

- Rust 生态原生支持（`toml` crate），Go 侧 `pelletier/go-toml`；
- 人类可编辑、可注释、嵌套结构清晰，优于 JSON/YAML 的缩进敏感性。

#### 3.3.2 结构与示例

```toml
# adapters/7daystodie/adapter.toml
schema_version = 1
adapter_id = "7daystodie"
game_id = "7daystodie"

[image]
name = "registry/game/7daystodie-adapter"
tag = "0.4.0"

[build]
type = "steamcmd"
app_id = 294420
branch = "public"

[lifecycle]                # 默认继承 base，这里只写覆盖项
save = "/scripts/save.sh"
stop = "/scripts/stop.sh"

[port_inject]
enabled = true
env = "GAME_HOST_PORT"

# ── 配置项 schema：每个 property 一个 table，键名 = 配置项名 ──
[config."ServerName"]
label_key = "cfg.ServerName.label"          # 显示名 = i18n key
description_key = "cfg.ServerName.desc"
type = "string"
default = "My Game Host"
control = "player"                          # 权限分级（见 §3.3.4）
apply = "always"
group_key = "grp.general"

[config."ServerMaxPlayerCount"]
label_key = "cfg.ServerMaxPlayerCount.label"
type = "int"
min = 1
max = 64
default = 8
control = "player"
apply = "always"
group_key = "grp.players"

[config."GameWorld"]
label_key = "cfg.GameWorld.label"
type = "enum"
enum = ["Navezgane", "RWG", "Pregen06k01"]
default = "Navezgane"
control = "player"
apply = "on_first_start"                    # 世界：存档生成后改了无意义
group_key = "grp.world"
[config."GameWorld".enum_labels]           # 枚举选项显示名同样是 i18n key
Navezgane = "world.navezgane"
RWG = "world.rwg"

[config."ServerVisibility"]
label_key = "cfg.ServerVisibility.label"
type = "enum"
enum = [0, 1, 2]
default = 2
control = "platform"                       # 平台运营方配置，玩家不可见
apply = "always"
group_key = "grp.network"

[config."TelnetEnabled"]
control = "locked"                         # 平台锁定：save/stop 依赖，写死
[config."EACEnabled"]
control = "locked"                         # 容器 /server 只读
[config."UserDataFolder"]
control = "locked"                         # 固定 /data/7DaysToDie

[i18n]
fallback = "en"                            # 缺语言时的兜底

[i18n.en]
"cfg.ServerName.label" = "Server name"
"cfg.ServerName.desc" = "Name shown in the server browser"
"cfg.ServerMaxPlayerCount.label" = "Max players"
"cfg.GameWorld.label" = "World"
"world.navezgane" = "Navezgane"
"world.rwg" = "Random World Gen"
"grp.general" = "General"
"grp.players" = "Players"
"grp.world" = "World"
"grp.network" = "Network"
"control.player" = "Player"
"control.platform" = "Platform"

[i18n.zh]
"cfg.ServerName.label" = "服务器名称"
"cfg.ServerName.desc" = "在服务器浏览器中显示的名称"
"cfg.ServerMaxPlayerCount.label" = "最大玩家数"
"cfg.GameWorld.label" = "世界"
"world.navezgane" = "纳维兹甘"
"world.rwg" = "随机世界生成"
"grp.general" = "基础"
"grp.players" = "人数"
"grp.world" = "世界"
"grp.network" = "网络"
"control.player" = "玩家"
"control.platform" = "平台"
```

#### 3.3.3 国际化约定

- schema 中所有面向用户字符串（label、description、group 名、枚举选项名、control 级别显示名）一律引用 i18n key；
- 翻译表内嵌 adapter.toml 的 `[i18n]` 段，单文件自包含，可扩展任意语言；
- 解析（asset_service 注册时一次完成）：生成字典 `{ key: { en, zh, ... }, fallback: "en" }` 存入 schema；
- 前端渲染按当前 locale 取值，**查找链：当前语言 → fallback(en) → 原始 key**，永不空白；
- adapter 展示名（游戏列表名称/描述）同样走 i18n key。

#### 3.3.4 配置权限分级：`control` 三值

| control | 语义 | 谁能配置 | 存储位置 | UI 呈现 |
| --- | --- | --- | --- | --- |
| `player` | 玩家（实例所有者）配置 | 用户，创建/管理实例时填 | `instance_configs`（按实例） | 玩家表单（schema 生成） |
| `platform` | 平台运营方配置（**可运营，非锁死**） | 仅 admin | `game_platform_configs`（按游戏全局） | admin 平台配置页 |
| `locked` | 平台固定写死（基础设施依赖） | 无人 | 不落表，值在模板/镜像内 | 不出现（或只读展示） |

要点：

1. **渲染合并优先级（低 → 高，后覆盖前）**

```text
模板默认值（文件自带）
  → locked 项（写死，渲染器跳过不覆盖）
  → platform 级（admin 按游戏设置，所有实例共享）
  → player 级（用户按实例设置）
  → 端口注入 env（运行时，最高）
```

同一 key 只属于一个 control 级别（schema 校验约束）。

2. **platform 级 = 可运营配置**，与 `locked`（纯基础设施）严格区分：

```text
admin：GET/PUT /api/admin/games/:id/platform-config
  表单只渲染 control=platform 的项（如 ServerVisibility、Region）
  存 game_platform_configs（game_id, config JSON, version, updated_by）
  → 该游戏全部实例共享，玩家创建实例时看不到这些项
```

3. **数据流**：

```text
controller 组装渲染输入 = locked（manifest 标记跳过）
                        + platform（游戏级查询）
                        + player（实例级查询）
                        + env（端口注入）
   ↓ 下发 node_agent → 写 /data/.platform/game-config.json
   ↓ 容器内 config-render.sh 按 key 合并
```

### 3.4 游戏配置文件渲染（解决 serverconfig.xml 等写死配置）

#### 3.4.1 通道分工

| 通道 | 适合什么 | 不适合什么 |
| --- | --- | --- |
| env 注入 | **运行时才知道的值**：宿主端口、少量实例标识 | 一大坨静态配置（env 长度/可读性差、secret 进 docker inspect） |
| 配置文件渲染 | **静态配置**：ServerName、密码、人数、世界、难度等 | — |

#### 3.4.2 渲染流程

```text
① 声明：adapter.toml [config] 段（schema，见 §3.3）
② 表单与存储：
   玩家表单 = control=player 项（schema 生成）
   admin 表单 = control=platform 项
   存储：instance_configs（player）/ game_platform_configs（platform）
③ 下发：controller → InstanceRuntimeSpec.config（proto 扩展）
        node_agent start_instance 时写入 /data/.platform/game-config.json
④ 合并：容器内 /scripts/lib/config-render.sh
   按 key 合并进游戏配置文件：
     key 存在 → 只更新该属性值
     key 不存在 → 插入一行
     apply=on_first_start 且文件已存在 → 跳过（不覆盖已有存档的世界设置）
```

关键性质：**按 property 粒度合并，不是整体重写**。用户通过平台表单配置的项每次启动被覆盖（平台管理生效）；用户通过文件管理手工添加的项（模板中没有的属性、模组设置）原样保留——managed 项与自由项天然兼容。

#### 3.4.3 渲染清单（镜像内）

构建时从 adapter.toml 生成 `/scripts/config-manifest.json`（只含 key + control + apply + 定位规则），供 config-render.sh 使用；XML/JSON/INI 格式差异全部收敛在公共库，游戏只写声明。

### 3.5 环境变量注入：三来源

| 来源 | 例子 | 谁定义 | 谁注入 | 脚本里怎么读 |
| --- | --- | --- | --- | --- |
| 平台注入（运行时） | `GAME_HOST_PORT`=宿主端口 | `adapter.toml` 的 `port_inject.env` | controller 每次开服按分配结果注入 | `env.sh` 解析 → 端口改写 |
| 平台约定（镜像边界） | `DATA_ROOT=/data`、`SERVER_ROOT=/server`、`TEMPLATE_DIR=/templates` | base 的 `lib/env.sh` 默认值 | 无人注入，靠默认值 | 所有脚本 source env.sh |
| 游戏自有（实例配置） | `DST_SHARD=both` 等 | `adapter.toml` 的 `config` 段（type/默认/枚举） | 用户表单 → controller 注入 | hooks 里 `${DST_SHARD:-both}` |

controller 生成 env 的知识来源：端口注入变量名来自 `port_inject.env`，实例配置项来自 `config` 段声明（经 asset_service 在 `ResolveGameBuild` 响应中附带 `adapter_metadata` 流转），值来自调度结果与用户请求；未知 key 拒绝、default/enum 校验。

### 3.6 平台侧代码改造

#### 3.6.1 node_agent：通用生命周期执行器

- `ContainerClient` trait 增加 `exec(container_id, cmd) -> ExecOutput`（bollard exec）；
- `node_agent_service` 增加阶段执行：`start → exec stop.sh → 超时 → docker stop → SIGKILL` 分级优雅停；周期 `exec players.sh/health.sh` 驱动空闲检测与健康上报；
- 使 `save.sh`/`players.sh`/`health.sh` 从「死脚本」变成平台真实调用点。

#### 3.6.2 controller：端口注入与配置下发公共化

- `buildInstanceEnv`（`reconcile_dispatcher.go:753`）改为从 `adapter_metadata.port_inject.env` 读取变量名，默认 `GAME_HOST_PORT`；
- `InstanceRuntimeSpec` 增加 `config` 字段，透传校验后的配置 JSON（platform + player 合并）；
- `game_container_configs` 增加 `port_inject_env` 字段（默认 `GAME_HOST_PORT`），管理员开关逻辑（`InjectGamePort`）不变；
- 新增 `instance_configs`（按实例）、`game_platform_configs`（按游戏）两张配置表。

#### 3.6.3 asset_service：schema/metadata 随 GameBuild 注册

> **收敛决策（2026-08-20）**：适配器作为**镜像分层**保留（构建期概念），作为**独立数据实体**删除。
> 理由：镜像不保存游戏（游戏在宿主机 game-cache），适配器镜像 = 静态驱动（steamcmd + 脚本 + 模板），
> 其 schema/metadata 本就应绑定 build 引用的镜像版本；独立 adapter 表带来跨表关联与版本错配风险。
> 收敛后 `ResolveGameBuild` **一次返回全套**（build + adapter_metadata + schema_json），无二次查询。

- `RegisterGameBuild` 请求携带 `adapter_metadata` + `schema_json`（gen_manifest.py 产物），随构建存储（`t_asset_service_game_builds` 加 `metadata_json`/`schema_json` 列）；
- `ResolveGameBuild` 直接返回 build 自带的 metadata/schema——controller 不需要二次查询，按 adapter_version 匹配的缺口自动消失（schema 天然绑定 build）；

#### 3.6.4 RegisterGameBuild：增量迭代注册（build_id 系统生成）

> **注册模型（2026-08-21）**：build_id 是**系统生成标识**，管理员不可手填、不可编辑；
> 注册是**基于旧版本的增量迭代**，管理员只提交需要更新的字段，其余从基准版本继承。

- **build_id 生成规则**：`{game_id}-{channel}-{artifact_image_tag}`（channel 为空则 `{game_id}-{tag}`）。
  请求中携带非空且与规则不符的 build_id → `InvalidArgument` 拒绝（防伪造/手改）；
- **迭代基准 `base_build_id`**（可选）：显式指定从哪个 build 继承；缺省 = 同 channel 最新
  `Available`。指定基准时请求 channel 与基准 channel 不一致 → 拒绝（迭代不可跨 channel）；
- **继承规则**（请求中未显式设置的字段从基准继承）：

  | 字段 | 未设置标记 | 继承来源 |
  | --- | --- | --- |
  | `channel` | `null` | 基准 |
  | `adapter_id` | 空串 | 基准（若携带 schema 则优先 schema.adapter_id） |
  | `adapter_version` | `0.0.0`（未解析） | 基准 |
  | `upstream_version` / `artifact_uri` / `artifact_image_name` | `null` | 基准 |
  | `adapter_metadata` / `schema_json` | `null`（未重新上传） | 基准 |
  | `pinned` | `false` | 基准 |

- **必填**：仅 `artifact_image_tag`（新版本身份，tag 不同 → build_id 不同 → 保留历史版本）；
- **幂等**：同 build_id（同 tag 重传）覆盖更新，不触发 Deprecated；新版本注册后同 channel
  旧 `Available`（非 pinned）自动标为 `Deprecated`；
- **前端**：表单去掉 build_id 输入，改为「迭代基准」下拉 + 只读 build_id 实时预览
  （`{game_id}-{channel}-{tag}`）；留空字段不提交（服务端继承），schema/metadata 不重新上传
  则继承基准的配置能力。

### 3.7 目录结构（目标形态）

```text
adapters/
  base/
    Dockerfile                      # game-base:2026.xx
    scripts/lib/*.sh                # 公共库（shellcheck + bats 单测）
    scripts/lifecycle/{start,save,stop,players,health}.sh
    tests/                          # 公共库与框架的 bats 契约测试
  dst/
    adapter.toml                    # 声明式元数据
    Dockerfile                      # FROM registry/game/base:2026.xx + COPY
    hooks.sh                        # 差异钩子
    templates/
  7daystodie/                       # 同构
  ...
```

## 4. 契约演进与稳定性

- **双版本号**：`adapter.toml` 的 `schema_version`（平台契约）+ `min_spec_version`（生命周期脚本契约，asset_service 已有字段）；公共库破坏性变更 → bump spec version，非破坏 → adapter 小版本；
- **镜像 tag 锁定**：adapter Dockerfile `FROM base:精确tag`；构建 tag `{adapter_id}:{adapter_version}-{upstream_version}`；
- **CI 契约测试**：每个 adapter 跑 `build → prepare → start → health → save → stop` 全流程；公共库跑 shellcheck + bats；新增游戏时契约测试即验收标准；
- **改动方向单向**：base 变 → 全部游戏镜像重建（hooks 不动）；hooks 变 → 只重建该游戏镜像；adapter.toml 变 → 平台行为跟随。无循环依赖。

## 5. 迁移路径

| 阶段 | 内容 | 平台无感？ |
| --- | --- | --- |
| M1 ✅ | 提取 base image + 脚本库 + 生命周期框架（独立跑 bats） | **已完成**：`adapters/base/`（Dockerfile、lib/ 8 个脚本库、lifecycle/ 6 个框架入口、bats 测试）；镜像构建与 CI 测试接入待环境就绪（本机沙箱无法运行 bash） |
| M2 ✅ | 迁移 dst / 7daystodie：Dockerfile 改 `FROM base`，脚本改 hooks + adapter.toml，**行为不变**（容器内 `/scripts/start.sh` 路径保留） | **已完成**：两游戏 Dockerfile 改 `FROM base`（各约 15 行），旧 `scripts/` 目录删除，改为 `hooks.sh`（dst 4 钩子 / 7dtd 6 钩子）+ `adapter.toml`（dst 3 项 / 7dtd 69 项配置 schema + i18n 中英）；容器内 `/scripts/*.sh` 契约路径由 base 软链保留，平台零改动 |
| M3 ✅ | node_agent 加 exec + 生命周期完整驱动 + 端口注入公共化（去掉 `SDTD_SERVER_PORT` 硬编码）+ 配置渲染链路 | **已完成**：`ContainerClient` 加 `exec`（bollard，含 ExecOutput）+ 分级停服（exec stop.sh → docker stop）；`InstanceRuntimeSpec.config` 下发 → 写 `/data/.platform/game-config.json`；controller 移除 `SDTD_SERVER_PORT` 硬编码（读 `game_container_configs.port_inject_env`，默认 `GAME_HOST_PORT`）；`game_instances.config` 存储 + 创建接口透传；proto 重新生成（buf）；三端编译通过 | |
| M4 ✅ | asset_service schema/metadata 随 GameBuild 注册（收敛模型）+ CI 契约测试 | **已完成**：`RegisterGameBuild` 携带 `adapter_metadata`+`schema_json`（builds 表加列），`ResolveGameBuild` 一次返回全套；**删除独立 adapter 实体**（RegisterAdapter/GetAdapterSchema RPC、AdapterRepository、adapter 表全部移除）；TOML 解析在 `adapters/tools/gen_manifest.py`（离线，tomllib）生成 metadata.json/schema.json/config-manifest.json（7dtd 69 项：35 player/28 platform/6 locked）；镜像 COPY config-manifest.json；start.sh 接入 `render_config_auto`；CI 契约脚本 `adapters/tools/ci-test.sh`；三端编译通过 | ✅ |
| M5 ✅ | 前后端校验 + 配置表单 + GameBuild 注册适配 | **已完成**：schema 契约校验（key 唯一/control/apply/render/type/enum/render_file 绝对路径，asset_service 单测 23 过）；实例配置校验（controller `ValidateInstanceConfig`：未知 key/locked/int 范围/bool/enum）；`GET /api/games/:id/config-schema`（controller → platform-service 透传）；下单带 config（orders 表加列 → 支付时透传创建实例）；前端 MyOrdersView 按 schema 渲染 player 配置表单（string/int/bool/enum/secret + i18n 中英 + 默认值预填 + 分组）；**GameBuild 注册全链路适配**（admin 表单上传 schema.json/metadata.json → platform-service 透传 → controller → asset_service 校验落库，proto json tag 为 snake_case 天然兼容 gen_manifest 产物） | ✅ |
| M6 ✅ | admin 平台配置页 + 实例配置更新 | **已完成**：`game_platform_configs` 表（按游戏全局，control=platform 项）+ `GET/PUT /api/games/:id/platform-config`（controller，仅 platform key 允许）+ platform-service 透传 `/api/admin/games/:id/platform-config` + 前端 AdminGamePlatformConfigView（schema 驱动表单）；启动时合并下发（platform 为底、player 覆盖，`mergedInstanceConfig`）；实例配置更新 `PUT /api/game-instances/:id/config`（schema 校验，重启生效）+ platform-service 透传 `/api/me/instances/:orderId/config` + MyServersView 配置弹层 | ✅ |
| M7 ✅ | GameBuild 增量迭代注册（build_id 系统生成） | **已完成**（§3.6.4）：注册改为增量语义——`build_id` 由系统按 `{game_id}-{channel}-{tag}` 生成（请求自定义 → 拒绝），新增 `base_build_id` 迭代基准（缺省 = 同 channel 最新 Available），未显式设置字段（channel/adapter_id/adapter_version/upstream/artifact/metadata/schema/pinned）从基准继承，仅 `artifact_image_tag` 必填；proto 加 `base_build_id` 并重新生成（asset_service tonic + controller-go protoc）；controller/platform-service 去掉 build_id 手填校验、透传 base_build_id；前端 AdminGameBuildsView 改为「迭代基准」下拉 + 只读 build_id 预览 + 留空即继承（schema/metadata 不重新上传则继承）；asset_service 单测 31 过、三端编译 + 前端 vue-tsc 通过 | ✅ |
| 后续 | 配置热更新（游戏内命令如 7dtd setgamepref）、配置版本对比/回滚 | — |

> 建议：M1+M2 先行（纯适配器目录内重构，平台零感知，立即消除全部脚本重复）；M3 作为独立第二阶段（动平台两侧，需联调）。

## 6. 风险与边界

- **迁移行为回归**：M2 必须保证容器内脚本路径与行为不变，以契约测试（build→start→health→save→stop）兜底；
- **配置渲染覆盖用户手工编辑**：按 key 合并 + `apply=on_first_start` 约束，managed 项与自由项并存；渲染器需对"文件被用户改坏/缺 key"容错（不存在则插入、解析失败则告警并跳过）；
- **secret 泄漏**：`secret=true` 的项在库中加密、日志脱敏、不进 docker inspect（配置走文件通道而非 env）；
- **TOML 解析兼容**：Go/Rust 两侧解析器需对同一 schema 输出一致；schema 校验在 asset_service 注册时统一执行；
- **i18n 缺失**：查找链兜底（当前语言 → fallback → 原始 key）保证永不空白；新增语言只需扩展 `[i18n]` 段；
- **多 shard 游戏（DST）**：启动/停止/健康仍是单一容器视角，shard 内聚在 hooks 中，框架不感知；
- **平台锁定项误开放**：`locked` 项不落表、不进 manifest 渲染清单，杜绝被覆盖。
