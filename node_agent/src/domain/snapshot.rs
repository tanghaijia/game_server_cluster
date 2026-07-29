use std::path::Path;

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
    pub manifest: Option<Manifest>,
    pub checksum: Option<String>,
    pub captured_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Manifest {
    pub snapshot_id: String,
    pub instance_id: String,
    pub captured_at: String,
    pub checksum: String,
    pub file_count: usize,
    pub total_size_bytes: u64,
    pub entries: Vec<Entry>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Entry {
    pub path: String,
    pub size: u64,
    pub mode: String,
}

/**
* 宿主机数据文件根目录
**/
pub const HOST_DATA_PATH: &str = "/data/game_instances";

/**
* 宿主机保存snapshot的数据文件根目录
**/
pub const HOST_LOCAL_SNAP_SHOTS_DATA_PATH: &str = "/data";

#[derive(Clone, Serialize, Deserialize)]
pub struct HostSnapShotDataPath {
    path: String,
}

impl HostSnapShotDataPath {
    pub fn new(path: String) -> Self {
        let host = Path::new(HOST_DATA_PATH);
        let mut new_path = host.to_path_buf();
        new_path.push(path);
        Self {
            path: new_path.to_string_lossy().to_string(),
        }
    }

    pub fn to_string(&self) -> anyhow::Result<String> {
        Ok(self.path.clone())
    }
}

impl AsRef<Path> for HostSnapShotDataPath {
    fn as_ref(&self) -> &Path {
        Path::new(&self.path)
    }
}
