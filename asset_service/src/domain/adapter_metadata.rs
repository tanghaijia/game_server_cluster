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

/// 校验 AdapterSchema 契约（RegisterGameBuild 携带 schema 时调用）。
/// 校验规则：key 唯一、control/apply/render/type 合法、enum 必须有选项、
/// render_file 必须为绝对路径、adapter_id 非空。
/// 失败返回第一条错误信息。
pub fn validate_adapter_schema(schema: &AdapterSchema) -> Result<(), String> {
    if schema.adapter_id.trim().is_empty() {
        return Err("schema.adapter_id 不能为空".to_string());
    }
    if schema.game_id.trim().is_empty() {
        return Err("schema.game_id 不能为空".to_string());
    }
    if let Some(file) = &schema.render_file {
        if !file.is_empty() && !file.starts_with('/') {
            return Err(format!("config_render.file 必须是容器内绝对路径: {file}"));
        }
    }

    let mut seen = std::collections::HashSet::new();
    for s in &schema.settings {
        if !seen.insert(s.key.as_str()) {
            return Err(format!("配置项 key 重复: {}", s.key));
        }
        match s.control.as_str() {
            "player" | "platform" | "locked" => {}
            other => return Err(format!("配置项 {} control 非法: {other}", s.key)),
        }
        match s.apply.as_str() {
            "always" | "on_first_start" => {}
            other => return Err(format!("配置项 {} apply 非法: {other}", s.key)),
        }
        match s.render.as_str() {
            "xml_property" | "envsubst" | "sed_pattern" => {}
            other => return Err(format!("配置项 {} render 非法: {other}", s.key)),
        }
        match s.type_.as_str() {
            "string" | "int" | "bool" | "enum" => {}
            other => return Err(format!("配置项 {} type 非法: {other}", s.key)),
        }
        if s.type_ == "enum"
            && (s.enum_values.is_none() || s.enum_values.as_ref().is_some_and(|v| v.is_empty()))
        {
            return Err(format!("配置项 {} 为 enum 但 enum_values 为空", s.key));
        }
        if s.secret && s.control == "locked" {
            return Err(format!("配置项 {} locked 项不应声明 secret", s.key));
        }
    }
    Ok(())
}

/// 校验实例配置键值对（controller 侧校验用户提交的 config）。
/// 规则：未知 key 拒绝；locked 项拒绝（平台锁定，不可由实例配置覆盖）；
/// enum/range/type 校验；返回 (规范化后的配置, 错误)。
pub fn validate_instance_config(
    schema: &AdapterSchema,
    config: &std::collections::HashMap<String, String>,
) -> Result<std::collections::HashMap<String, String>, String> {
    let by_key: std::collections::HashMap<&str, &AdapterConfigSetting> =
        schema.settings.iter().map(|s| (s.key.as_str(), s)).collect();

    for (key, value) in config {
        let Some(setting) = by_key.get(key.as_str()) else {
            return Err(format!("未知配置项: {key}"));
        };
        if setting.control == "locked" {
            return Err(format!("配置项 {key} 由平台锁定，不可配置"));
        }
        match setting.type_.as_str() {
            "int" => {
                let n: i64 = value.parse().map_err(|_| {
                    format!("配置项 {key} 需要整数，收到: {value}")
                })?;
                if let Some(min) = setting.min {
                    if n < min {
                        return Err(format!("配置项 {key} 小于最小值 {min}"));
                    }
                }
                if let Some(max) = setting.max {
                    if n > max {
                        return Err(format!("配置项 {key} 大于最大值 {max}"));
                    }
                }
            }
            "bool" => match value.as_str() {
                "true" | "false" => {}
                _ => return Err(format!("配置项 {key} 需要 true/false，收到: {value}")),
            },
            "enum" => {
                let values = setting.enum_values.as_deref().unwrap_or_default();
                if !values.iter().any(|v| v == value) {
                    return Err(format!(
                        "配置项 {key} 值非法: {value}（可选: {}）",
                        values.join(", ")
                    ));
                }
            }
            _ => {}
        }
    }
    Ok(config.clone())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample_schema() -> AdapterSchema {
        AdapterSchema {
            adapter_id: "7daystodie".to_string(),
            game_id: "7daystodie".to_string(),
            render_file: Some("/data/serverconfig.xml".to_string()),
            settings: vec![
                AdapterConfigSetting {
                    key: "ServerName".to_string(),
                    type_: "string".to_string(),
                    control: "player".to_string(),
                    apply: "always".to_string(),
                    render: "xml_property".to_string(),
                    default: None,
                    min: None,
                    max: None,
                    enum_values: None,
                    secret: false,
                    label_key: None,
                    description_key: None,
                    group_key: None,
                },
                AdapterConfigSetting {
                    key: "ServerMaxPlayerCount".to_string(),
                    type_: "int".to_string(),
                    control: "player".to_string(),
                    apply: "always".to_string(),
                    render: "xml_property".to_string(),
                    default: None,
                    min: Some(1),
                    max: Some(64),
                    enum_values: None,
                    secret: false,
                    label_key: None,
                    description_key: None,
                    group_key: None,
                },
                AdapterConfigSetting {
                    key: "ServerVisibility".to_string(),
                    type_: "enum".to_string(),
                    control: "platform".to_string(),
                    apply: "always".to_string(),
                    render: "xml_property".to_string(),
                    default: None,
                    min: None,
                    max: None,
                    enum_values: Some(vec!["0".to_string(), "1".to_string(), "2".to_string()]),
                    secret: false,
                    label_key: None,
                    description_key: None,
                    group_key: None,
                },
                AdapterConfigSetting {
                    key: "TelnetEnabled".to_string(),
                    type_: "bool".to_string(),
                    control: "locked".to_string(),
                    apply: "always".to_string(),
                    render: "xml_property".to_string(),
                    default: None,
                    min: None,
                    max: None,
                    enum_values: None,
                    secret: false,
                    label_key: None,
                    description_key: None,
                    group_key: None,
                },
            ],
            i18n: serde_json::json!({"fallback": "en"}),
        }
    }

    #[test]
    fn validate_schema_ok() {
        assert!(validate_adapter_schema(&sample_schema()).is_ok());
    }

    #[test]
    fn validate_schema_duplicate_key_rejected() {
        let mut s = sample_schema();
        s.settings.push(s.settings[0].clone());
        assert!(validate_adapter_schema(&s).unwrap_err().contains("重复"));
    }

    #[test]
    fn validate_schema_bad_control_rejected() {
        let mut s = sample_schema();
        s.settings[0].control = "owner".to_string();
        assert!(validate_adapter_schema(&s).unwrap_err().contains("control"));
    }

    #[test]
    fn validate_schema_enum_without_values_rejected() {
        let mut s = sample_schema();
        s.settings[2].enum_values = None;
        assert!(validate_adapter_schema(&s).unwrap_err().contains("enum_values"));
    }

    #[test]
    fn validate_schema_relative_render_file_rejected() {
        let mut s = sample_schema();
        s.render_file = Some("data/serverconfig.xml".to_string());
        assert!(validate_adapter_schema(&s).unwrap_err().contains("绝对路径"));
    }

    #[test]
    fn validate_config_ok() {
        let mut cfg = std::collections::HashMap::new();
        cfg.insert("ServerName".to_string(), "我的服".to_string());
        cfg.insert("ServerMaxPlayerCount".to_string(), "16".to_string());
        assert!(validate_instance_config(&sample_schema(), &cfg).is_ok());
    }

    #[test]
    fn validate_config_unknown_key_rejected() {
        let mut cfg = std::collections::HashMap::new();
        cfg.insert("NoSuchKey".to_string(), "x".to_string());
        assert!(validate_instance_config(&sample_schema(), &cfg).unwrap_err().contains("未知"));
    }

    #[test]
    fn validate_config_locked_key_rejected() {
        let mut cfg = std::collections::HashMap::new();
        cfg.insert("TelnetEnabled".to_string(), "false".to_string());
        assert!(validate_instance_config(&sample_schema(), &cfg).unwrap_err().contains("锁定"));
    }

    #[test]
    fn validate_config_int_out_of_range_rejected() {
        let mut cfg = std::collections::HashMap::new();
        cfg.insert("ServerMaxPlayerCount".to_string(), "128".to_string());
        assert!(validate_instance_config(&sample_schema(), &cfg).unwrap_err().contains("最大值"));
    }

    #[test]
    fn validate_config_enum_invalid_rejected() {
        let mut cfg = std::collections::HashMap::new();
        cfg.insert("ServerVisibility".to_string(), "9".to_string());
        assert!(validate_instance_config(&sample_schema(), &cfg).unwrap_err().contains("值非法"));
    }
}
