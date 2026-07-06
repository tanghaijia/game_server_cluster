use chrono::{DateTime, Utc};

/**
 * GameInstanceStatus
 */
#[derive(Clone)]
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
#[derive(Clone)]
pub struct GameInstance {
    pub id: String,
    pub status: GameInstanceStatus,
    pub container_id: Option<String>,
    pub game_build_id: String,
    pub create_time: DateTime<Utc>,
    pub update_time: DateTime<Utc>,
}

/**
* Controller视角上的业务的游戏服务
**/
pub struct GameInstanceBiz {
    pub id: String,
    pub status: String,
}
