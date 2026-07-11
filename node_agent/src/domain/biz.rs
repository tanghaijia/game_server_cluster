use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use crate::domain::HostSnapShotDataPath;

/**
 * GameInstanceStatus
 */
#[derive(Clone, Serialize, Deserialize)]
pub enum GameInstanceStatus {
    Pedding,
    Preparing,
    Running,
    Stopping,
    Stopped,
    Failed,
}

/**
* NodeAgent视角上的业务的游戏服务
**/
#[derive(Clone, Serialize, Deserialize)]
pub struct GameInstance {
    pub id: String,
    pub status: GameInstanceStatus,
    pub container_id: Option<String>,
    pub game_build_id: String,
    pub host_data_path: HostSnapShotDataPath,
    pub create_time: DateTime<Utc>,
    pub update_time: DateTime<Utc>,
}

impl GameInstance {
    pub fn new(
        id: String,
        status: GameInstanceStatus,
        container_id: Option<String>,
        game_build_id: String,
    ) -> Self {
        Self {
            id: id.clone(),
            status,
            container_id,
            game_build_id,
            host_data_path: HostSnapShotDataPath::new(id),
            create_time: Utc::now(),
            update_time: Utc::now(),
        }
    }
}

/**
* Controller视角上的业务的游戏服务
**/
pub struct GameInstanceBiz {
    pub id: String,
    pub status: String,
}
