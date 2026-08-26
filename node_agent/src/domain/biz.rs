use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use crate::domain::HostSnapShotDataPath;

/**
 * GameInstanceStatus
 */
#[derive(Clone, PartialEq, Serialize, Deserialize)]
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
    /// 失败原因（容器退出/游戏进程崩溃等；status == Failed 时填充，
    /// 供 controller 展示给用户——启动失败用户可见性闭环）
    /// #[serde(default)]：兼容库里已存在的旧记录（无该字段）
    #[serde(default)]
    pub fail_reason: String,
    /// B-04/P1-3：运行时探针模式（"script" | "a2s" | "none"；旧记录缺省 script）
    #[serde(default)]
    pub probe_mode: String,
    /// B-04/P1-3：A2S 查询宿主端口（a2s 模式；旧记录缺省 None）
    #[serde(default)]
    pub query_host_port: Option<u16>,
    /// P2-A：实例所属游戏（start_instance 时落库，remove_cache/refcount 释放用）
    #[serde(default)]
    pub game_id: String,
    /// P2-A：实例解析到的 Steam 分支名（start_instance 时落库）
    #[serde(default)]
    pub branch_name: String,
    /// P2-A：实例实际挂载的缓存版本 buildid（start_instance 时落库；
    /// refcount 释放与"重启沿用原 buildid"用）
    #[serde(default)]
    pub cache_build_id: String,
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
            fail_reason: String::new(),
            probe_mode: String::new(), // B-04/P1-3：start_instance 时按声明填充
            query_host_port: None,
            game_id: String::new(),     // P2-A：start_instance 时落库
            branch_name: String::new(), // P2-A：start_instance 时落库
            cache_build_id: String::new(), // P2-A：start_instance 时落库
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
