use std::collections::HashMap;
use std::process::Command;
use async_trait::async_trait;
use serde::Deserialize;
use anyhow::{anyhow, Result};
use keyvalues_serde::from_str;

use crate::ports::{DepotManifest, GameData, SteamBranch, SteamService};

#[derive(Debug, Deserialize)]
pub struct Depots {
    pub branches: HashMap<String, Branch>,

    #[serde(flatten)]
    pub depots: HashMap<String, serde_json::Value>,
}

#[derive(Debug, Deserialize)]
pub struct Branch {
    pub buildid: String,

    #[serde(default)]
    pub description: Option<String>,

    #[serde(default)]
    pub timeupdated: Option<String>,

    #[serde(default)]
    pub timebuildupdated: Option<String>,
}

#[derive(Debug, Deserialize)]
pub struct Depot {
    #[serde(default)]
    pub manifests: Option<HashMap<String, Manifest>>,
}

#[derive(Debug, Deserialize)]
pub struct Manifest {
    pub gid: String,

    #[serde(default)]
    pub size: Option<String>,

    #[serde(default)]
    pub download: Option<String>,
}

/// Steam Store API 响应中的单条游戏数据（SteamResponse 的内部字段）。
#[derive(Deserialize, Debug)]
struct SteamResponse {
    success: bool,
    data: Option<GameData>,
}

pub struct SteamServiceHttp {
    client: reqwest::Client,
    base_url: String,
}

impl SteamServiceHttp {
    pub fn new() -> Self {
        Self {
            client: reqwest::Client::new(),
            base_url: "https://store.steampowered.com".to_string(),
        }
    }

    /// 测试用：注入自定义 HTTP client 和 base URL。
    #[cfg(test)]
    fn with_client(client: reqwest::Client, base_url: String) -> Self {
        Self { client, base_url }
    }
}

impl Default for SteamServiceHttp {
    fn default() -> Self {
        Self::new()
    }
}

impl SteamServiceHttp {
    pub fn get_app_info(appid: u32) -> Result<String> {
        let output = Command::new("steamcmd")
            .args([
                "+login",
                "anonymous",
                "+app_info_print",
                &appid.to_string(),
                "+quit",
            ])
            .output()?;

        if !output.status.success() {
            return Err(anyhow!("steamcmd failed"));
        }

        let text = String::from_utf8(output.stdout)?;

        let start = text
            .find(&format!("\"{}\"", appid))
            .ok_or_else(|| anyhow!("app root not found"))?;

        Ok(text[start..].to_string())
    }

    pub fn parse_appinfo(vdf: &str) -> Result<Vec<SteamBranch>> {
        // 从完整 VDF 中截取 "depots" 片段，避免 common/config/extended 等字段干扰解析
        let depots_key = vdf
            .find("\"depots\"")
            .ok_or_else(|| anyhow!("depots not found"))?;
        let brace = vdf[depots_key..]
            .find('{')
            .ok_or_else(|| anyhow!("depots block not found"))?;
        let block_start = depots_key + brace;

        let mut depth = 1;
        let mut pos = block_start + 1;
        while depth > 0 && pos < vdf.len() {
            match vdf.as_bytes()[pos] {
                b'{' => depth += 1,
                b'}' => depth -= 1,
                _ => {}
            }
            pos += 1;
        }

        let depots_text = &vdf[depots_key..pos];
        let depots: Depots = from_str(depots_text)?;

        let mut result = Vec::new();

        for (branch_name, branch_info) in &depots.branches {
            let mut manifests = Vec::new();

            for (depot_id, depot_val) in &depots.depots {
                let depot_id_num = match depot_id.parse::<u32>() {
                    Ok(v) => v,
                    Err(_) => continue,
                };

                // depots 中可能混有字符串字段（如 baselanguages），跳过非 object 的值
                let depot: Depot = match serde_json::from_value(depot_val.clone()) {
                    Ok(d) => d,
                    Err(_) => continue,
                };

                let Some(depot_manifests) = &depot.manifests else {
                    continue;
                };

                if let Some(manifest) = depot_manifests.get(branch_name) {
                    manifests.push(DepotManifest {
                        depot_id: depot_id_num,
                        manifest_gid: manifest.gid.parse()?,
                    });
                }
            }

            result.push(SteamBranch {
                name: branch_name.clone(),
                build_id: branch_info.buildid.parse()?,
                description: branch_info.description.clone(),
                manifests,
            });
        }

        Ok(result)
    }
}

#[async_trait]
impl SteamService for SteamServiceHttp {
    async fn fetch_game_from_steam(&self, app_id: u32) -> Result<GameData, String> {
        let url = format!("{}/api/appdetails?appids={}", self.base_url, app_id);

        let response: HashMap<String, SteamResponse> = self
            .client
            .get(&url)
            .send()
            .await
            .map_err(|e| format!("网络请求失败: {}", e))?
            .json()
            .await
            .map_err(|e| format!("JSON 解析失败: {}", e))?;

        if let Some(game_info) = response.get(&app_id.to_string()) {
            if game_info.success {
                if let Some(data) = &game_info.data {
                    return Ok(data.clone());
                }
            }
        }

        Err("Steam 找不到该游戏或请求失败".to_string())
    }


    async fn get_steam_branchs(&self, app_id: u32) -> Result<Vec<SteamBranch>, String> {
        let vdf = Self::get_app_info(app_id).map_err(|e| e.to_string())?;

        let branches = Self::parse_appinfo(&vdf).map_err(|e| e.to_string())?;

        Ok(branches)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// 用真实的 steamcmd 输出文件测试 7 Days to Die（251570）的解析。
    #[test]
    fn test_parse_appinfo_251570() {
        let raw = include_str!("../../test/data/steacmd_appinfo_output_251570");
        let start = raw.find(r#""251570""#).expect("app root not found");
        // steamcmd 输出末尾有 "Unloading Steam API...OK"，在解析前去掉
        let end = raw[start..].find("\nUnloading").unwrap_or(raw.len() - start);
        let vdf = raw[start..start + end].trim_end();

        let branches = SteamServiceHttp::parse_appinfo(vdf).unwrap();

        // public branch
        let public = branches.iter().find(|b| b.name == "public").unwrap();
        assert_eq!(public.build_id, 22422060);
        assert_eq!(public.description, None);
        // public 应覆盖所有游戏 depot
        assert!(public.manifests.len() >= 3);
        assert!(public.manifests.iter().any(|m| m.depot_id == 251576));
        assert!(public.manifests.iter().any(|m| m.depot_id == 251577));
        assert!(public.manifests.iter().any(|m| m.depot_id == 251578));

        // alpha21.2 branch
        let alpha21 = branches.iter().find(|b| b.name == "alpha21.2").unwrap();
        assert_eq!(alpha21.build_id, 12966449);
        assert_eq!(
            alpha21.description.as_deref(),
            Some("Alpha 21.2 Stable")
        );
        assert!(alpha21.manifests.len() >= 3);

        // v2.6 branch（和 public 同 buildid，但作为独立 branch）
        let v26 = branches.iter().find(|b| b.name == "v2.6").unwrap();
        assert_eq!(v26.build_id, 22422060);
        assert_eq!(v26.description.as_deref(), Some("Version 2.6 Stable"));
        assert!(v26.manifests.len() >= 3);

        // latest_experimental
        let latest_exp = branches
            .iter()
            .find(|b| b.name == "latest_experimental")
            .unwrap();
        assert_eq!(latest_exp.build_id, 22422060);
        assert_eq!(
            latest_exp.description.as_deref(),
            Some("Unstable build")
        );

        // 总共应解析出约 18 个 branch
        assert_eq!(branches.len(), 22);
    }

    /// 用真实的 steamcmd 输出文件测试 DST Dedicated Server（343050）的解析。
    #[test]
    fn test_parse_appinfo_343050() {
        let raw = include_str!("../../test/data/steacmd_appinfo_output_343050");
        let start = raw.find(r#""343050""#).expect("app root not found");
        let end = raw[start..].find("\nUnloading").unwrap_or(raw.len() - start);
        let vdf = raw[start..start + end].trim_end();

        let branches = SteamServiceHttp::parse_appinfo(vdf).unwrap();

        // public branch
        let public = branches.iter().find(|b| b.name == "public").unwrap();
        assert_eq!(public.build_id, 23206748);
        assert!(public.manifests.len() >= 3);
        assert!(public.manifests.iter().any(|m| m.depot_id == 343051));
        assert!(public.manifests.iter().any(|m| m.depot_id == 343052));
        assert!(public.manifests.iter().any(|m| m.depot_id == 343053));

        // beforemacoschanges
        let before = branches
            .iter()
            .find(|b| b.name == "beforemacoschanges")
            .unwrap();
        assert_eq!(before.build_id, 12576213);
        assert_eq!(before.description.as_deref(), Some("r579121"));
        assert!(before.manifests.len() >= 3);

        // updatebeta branch
        let updatebeta = branches.iter().find(|b| b.name == "updatebeta").unwrap();
        assert_eq!(updatebeta.build_id, 23482006);
        assert_eq!(updatebeta.description.as_deref(), Some("Public Beta"));
        assert!(updatebeta.manifests.len() >= 3);

        assert_eq!(branches.len(), 3);
    }
}
