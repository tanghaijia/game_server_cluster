use async_trait::async_trait;
use serde::{Deserialize, Serialize};

/// Steam 游戏详情（来自 Steam Store API）。
#[derive(Deserialize, Debug, Clone)]
pub struct GameData {
    pub name: String,
    pub header_image: String,
    pub short_description: String,
    #[serde(default)]
    pub price_overview: Option<PriceOverview>,
}

/// Steam 游戏价格信息。
#[derive(Deserialize, Debug, Clone)]
pub struct PriceOverview {
    pub final_formatted: String,
}

#[derive(Serialize)]
struct VersionResponse {
    success: bool,
    app_id: u32,
    #[serde(skip_serializing_if = "Option::is_none")]
    build_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    latest_patch_title: Option<String>,
    message: String,
}

#[derive(Debug)]
pub struct SteamBranch {
    pub name: String,
    pub build_id: u64,
    pub description: Option<String>,
    pub manifests: Vec<DepotManifest>,
}

#[derive(Debug)]
pub struct DepotManifest {
    pub depot_id: u32,
    pub manifest_gid: u64,
}

/// Steam 游戏查询接口。
///
/// 在 controller 中将由 `BuildResolver` 使用，根据上游版本（如 Steam app id）
/// 查询游戏元信息。
#[async_trait]
pub trait SteamService: Send + Sync {
    async fn fetch_game_from_steam(&self, app_id: u32) -> Result<GameData, String>;

    async fn get_steam_branchs(&self, app_id: u32) -> Result<Vec<SteamBranch>, String>;
}
