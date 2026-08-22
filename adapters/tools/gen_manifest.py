#!/usr/bin/env python3
# ============================================================
# adapters/tools/gen_manifest.py —— adapter.toml 离线解析工具
#
# 把 adapter.toml（声明式元数据）解析为三个 JSON 产物：
#   metadata.json           AdapterMetadata（port_inject + lifecycle）
#   schema.json             AdapterSchema（config settings + render_file + i18n）
#   config-manifest.json    镜像内渲染清单（key/render/apply，locked 排除）
#
# 收敛模型：metadata/schema 随 GameBuild 注册携带（RegisterGameBuild RPC），
# 不再有独立 adapter 实体；config-manifest.json 由 Dockerfile COPY 进镜像
# 供容器内 config-render.sh 使用。
#
# 依赖：Python 3.11+（tomllib 为标准库），无需第三方包。
# 用法：
#   python gen_manifest.py <adapter_id> <game_id> <adapter.toml> <out_dir>
#   # 例：
#   python gen_manifest.py dst dst adapters/dst/adapter.toml adapters/dst
#   python gen_manifest.py 7daystodie 7daystodie adapters/7daystodie/adapter.toml adapters/7daystodie
# ============================================================

import json
import sys
import tomllib

DEFAULT_SCRIPTS = {
    "start": "/scripts/start.sh",
    "save": "/scripts/save.sh",
    "stop": "/scripts/stop.sh",
    "players": "/scripts/players.sh",
    "health": "/scripts/health.sh",
}


def build_metadata(data: dict) -> dict:
    lifecycle = data.get("lifecycle", {})
    metadata = {
        "port_inject_env": None,
        "start_script": lifecycle.get("start", DEFAULT_SCRIPTS["start"]),
        "save_script": lifecycle.get("save", DEFAULT_SCRIPTS["save"]),
        "stop_script": lifecycle.get("stop", DEFAULT_SCRIPTS["stop"]),
        "players_script": lifecycle.get("players", DEFAULT_SCRIPTS["players"]),
        "health_script": lifecycle.get("health", DEFAULT_SCRIPTS["health"]),
    }
    port_inject = data.get("port_inject", {})
    if port_inject.get("enabled", False):
        metadata["port_inject_env"] = port_inject.get("env", "GAME_HOST_PORT")
    return metadata


def _opt_str(v):
    return v if isinstance(v, str) else None


def build_schema(adapter_id: str, game_id: str, data: dict) -> dict:
    settings = []
    for key, table in data.get("config", {}).items():
        settings.append(
            {
                "key": key,
                "type": table.get("type", "string"),
                "control": table.get("control", "player"),
                "apply": table.get("apply", "always"),
                "render": table.get("render", "xml_property"),
                "default": _opt_str(table.get("default")),
                "min": table.get("min"),
                "max": table.get("max"),
                "enum_values": (
                    [str(v) for v in table["enum"]] if "enum" in table else None
                ),
                "secret": table.get("secret", False),
                "label_key": _opt_str(table.get("label_key")),
                "description_key": _opt_str(table.get("description_key")),
                "group_key": _opt_str(table.get("group_key")),
            }
        )
    settings.sort(key=lambda s: s["key"])
    render = data.get("config_render", {})
    return {
        "adapter_id": adapter_id,
        "game_id": game_id,
        "settings": settings,
        "render_file": _opt_str(render.get("file")),
        "i18n": data.get("i18n", {"fallback": "en"}),
    }


def build_manifest(schema: dict) -> dict:
    return {
        "schema_version": 1,
        "file": schema["render_file"],
        "settings": [
            {"key": s["key"], "render": s["render"], "apply": s["apply"]}
            for s in schema["settings"]
            if s["control"] != "locked"
        ],
    }


def main() -> int:
    if len(sys.argv) != 5:
        print(__doc__)
        return 2
    adapter_id, game_id, toml_path, out_dir = sys.argv[1:5]

    with open(toml_path, "rb") as f:
        data = tomllib.load(f)

    metadata = build_metadata(data)
    schema = build_schema(adapter_id, game_id, data)
    manifest = build_manifest(schema)

    import os

    os.makedirs(out_dir, exist_ok=True)
    for name, obj in (
        ("metadata.json", metadata),
        ("schema.json", schema),
        ("config-manifest.json", manifest),
    ):
        with open(os.path.join(out_dir, name), "w", encoding="utf-8") as f:
            json.dump(obj, f, ensure_ascii=False, indent=2)
            f.write("\n")
        print(f"generated {out_dir}/{name}")

    print(f"summary: {len(schema['settings'])} settings, "
          f"{sum(1 for s in schema['settings'] if s['control'] == 'player')} player, "
          f"{sum(1 for s in schema['settings'] if s['control'] == 'platform')} platform, "
          f"{sum(1 for s in schema['settings'] if s['control'] == 'locked')} locked")
    return 0


if __name__ == "__main__":
    sys.exit(main())
