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
    pub status: GameCacheStatus,
    pub path: Option<String>,
    pub download_progress: Option<f32>,
    pub create_time: DateTime<Utc>,
    pub update_time: DateTime<Utc>,
}
