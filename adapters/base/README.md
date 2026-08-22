# adapters/base —— 游戏适配器公共基础镜像

公共层：所有游戏适配器（dst / 7daystodie / ...）共享的运行环境、脚本库与生命周期框架。
详见 [adapter-framework-design.md](../../docs/adapter-framework-design.md)。

## 目录结构

```text
adapters/base/
  Dockerfile                     # game-base 镜像
  scripts/lib/                   # 公共脚本库（每个文件单一职责）
    env.sh         公共环境变量解析（DATA_ROOT/SERVER_ROOT/TEMPLATE_DIR/GAME_HOST_PORT...）
    log.sh         统一日志（log / log_error / log_warn）
    fs.sh          文件工具（copy_if_missing / copy_dir_files_if_missing / ensure_dir...）
    process.sh     进程骨架（find_bin_default / run_in_foreground / stop_children / kill_remaining）
    output.sh      标准 JSON 输出（emit_health / emit_players）
    port-inject.sh 端口注入（inject_xml_property / apply_port_inject）
    template.sh    模板展开与占位符校验（expand_templates / assert_no_placeholder）
    config-render.sh 按 key 合并平台配置进游戏配置文件
  scripts/lifecycle/             # 生命周期框架（所有游戏同一份）
    start.sh / save.sh / stop.sh / players.sh / health.sh / prepare-runtime.sh
  tests/                         # bats 契约测试
    run_tests.sh                 运行全部测试
```

## 构建

```bash
docker build -t registry/game/base:2026.08 -f adapters/base/Dockerfile adapters/base
```

镜像内契约路径（软链）：`/scripts/{start,save,stop,players,health,prepare-runtime}.sh`
指向 `/scripts/lifecycle/` 同名框架脚本，保持既有容器契约不变。

## 测试

```bash
# 依赖：bats-core、jq（可选 shellcheck）
adapters/base/tests/run_tests.sh          # 运行 bats 契约测试
adapters/base/tests/run_tests.sh shellcheck  # 追加 shellcheck
```

## 游戏适配器如何使用

游戏镜像只需（M2 迁移目标形态）：

```dockerfile
FROM registry/game/base:2026.08
COPY hooks.sh /scripts/hooks.sh      # 游戏差异钩子（可选）
COPY templates/ /templates/          # 游戏配置模板
```

钩子协议（`hooks.sh` 内定义，全部可选）：

| 钩子 | 默认实现 | 覆盖场景 |
| --- | --- | --- |
| `hook_pre_start` | 模板展开 | DST token 校验、7dtd steamclient.so 检查 |
| `hook_find_bin` | `GAME_BIN_NAME` 按名查找 | 自定义查找逻辑 |
| `hook_start_command` | `run_in_foreground "${BIN}"` | DST 双 shard、特殊启动参数 |
| `hook_save` | 空操作 | 7dtd telnet saveworld |
| `hook_graceful_stop` | `kill -TERM` 子进程 | 7dtd telnet shutdown |
| `hook_health` | 按进程名 pgrep | 端口探测 |
| `hook_players` | `{"players": 0}` | 玩家数查询 |
| `hook_process_pattern` | `GAME_BIN_NAME` | stop 兜底匹配模式 |

## 环境变量约定

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `DATA_ROOT` | `/data` | 实例持久化数据根目录 |
| `SERVER_ROOT` | `/server` | 游戏服务端文件目录（只读） |
| `TEMPLATE_DIR` | `/templates` | 游戏配置模板目录 |
| `MOD_CACHE` | `/mod-cache` | 模组缓存目录 |
| `GAME_HOST_PORT` | 空 | 平台注入的游戏端口宿主端口 |
| `PLATFORM_CONFIG_FILE` | `/data/.platform/game-config.json` | 平台下发的实例配置 |
| `HOOKS_FILE` | `/scripts/hooks.sh` | 钩子文件位置 |
| `GAME_BIN_NAME` | 空 | 默认 find_bin 的可执行文件名 |
| `GAME_DEBUG` | `0` | 设为 1 开启 set -x |
