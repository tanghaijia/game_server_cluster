// ============================================================
// adapter_metadata.rs —— 适配器运行时元数据与配置 schema
//
// 由 adapter.toml（M2 落地）经 adapters/tools/gen_manifest.py（开发机/CI，
// python tomllib 解析）生成 metadata.json / schema.json，
// 再经 RegisterAdapter 登记到 asset_service。
// controller 消费：port_inject.env（env 变量名）、config schema（表单/校验）；
// node_agent 消费：lifecycle 脚本路径（docker exec 驱动）。
// ============================================================

use serde::{Deserialize, Serialize};

/// 适配器运行时元数据（adapter.toml [lifecycle] + [port_inject] 解析结果）。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AdapterMetadata {
    /// 端口注入 env 变量名（空 = 无注入），如 GAME_HOST_PORT
    pub port_inject_env: Option<String>,
    /// 生命周期脚本路径（容器内；默认由 base 提供 /scripts/{name}.sh）
    pub start_script: String,
    pub save_script: String,
    pub stop_script: String,
    pub players_script: String,
    pub health_script: String,
}

impl Default for AdapterMetadata {
    fn default() -> Self {
        Self {
            port_inject_env: None,
            start_script: "/scripts/start.sh".to_string(),
            save_script: "/scripts/save.sh".to_string(),
            stop_script: "/scripts/stop.sh".to_string(),
            players_script: "/scripts/players.sh".to_string(),
            health_script: "/scripts/health.sh".to_string(),
        }
    }
}

/// config schema 单条配置声明（adapter.toml `[config."<key>"]` 段）。
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AdapterConfigSetting {
    /// 配置项 key（对应游戏配置文件里的 property name）
    pub key: String,
    /// 类型：string / int / bool / enum
    #[serde(rename = "type")]
    pub type_: String,
    /// 权限分级：player（玩家）/ platform（平台运营方）/ locked（平台锁定）
    pub control: String,
    /// 应用时机：always（每次启动）/ on_first_start（仅首次）
    pub apply: String,
    /// 渲染方式：xml_property（默认）/ envsubst / sed_pattern
    pub render: String,
    pub default: Option<String>,
    pub min: Option<i64>,
    pub max: Option<i64>,
    pub enum_values: Option<Vec<String>>,
    pub secret: bool,
    pub label_key: Option<String>,
    pub description_key: Option<String>,
    pub group_key: Option<String>,
}

/// adapter.toml 解析出的完整 schema（供前端表单生成 / controller 校验）。
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AdapterSchema {
    pub adapter_id: String,
    pub game_id: String,
    /// 全部配置声明（含 locked，供前端展示与渲染清单生成）
    pub settings: Vec<AdapterConfigSetting>,
    /// 配置渲染目标文件（adapter.toml `[config_render] file`，如 /data/serverconfig.xml）
    pub render_file: Option<String>,
    /// i18n 字典：{"fallback": "en", "en": {...}, "zh": {...}}
    pub i18n: serde_json::Value,
}
