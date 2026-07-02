use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/**
* 业务上的snapshot
**/
struct SnapShot {
    pub snap_shop_id: String,
    pub user_id: String,
    pub instance_id: String, // 业务上的game_instance id
}

/**
* 本地被管理的snapshot
**/
struct LocalSnapShot {
    pub snap_shop_id: String,
    pub instance_id: String,
}

/**
* 快照artifact，快照的物理表示
**/
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SnapshotArtifact {
    pub snapshot_id: String,
    pub instance_data_path: String,
    pub storage_uri: String,
    pub manifest_uri: Option<String>,
    pub checksum: Option<String>,
    pub captured_at: DateTime<Utc>,
}

/**
* 宿主机数据文件根目录
**/
const HOST_DATA_PATH: &str = "/data/game_instances";


/**
* 宿主机保存snapshot的数据文件根目录
**/
const HOST_LOCAL_SNAP_SHOTS_DATA_PATH: &str = "/data";





