use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

#[derive(Clone, PartialEq, Serialize, Deserialize)]
pub enum GameCacheStatus {
    Downloading,
    Available,
    Removed,
    Unavailable,
}

#[derive(Clone, PartialEq, Serialize, Deserialize)]
pub struct GameCache {
    pub game_id: String,
    pub branch_name: String,
    /// 游戏版本号。与 game_id+branch_name 共同决定是否需要下载/更新。
    /// #[serde(default)] 兼容旧数据库记录(无该字段时反序列化为空串)。
    #[serde(default)]
    pub build_id: String,
    pub status: GameCacheStatus,
    pub path: Option<String>,
    pub download_progress: Option<f32>,
    pub create_time: DateTime<Utc>,
    pub update_time: DateTime<Utc>,
}
