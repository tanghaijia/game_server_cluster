/// 从 asset_service 返回的快照记录。
#[derive(Debug, Clone)]
pub struct SnapshotRecord {
    pub snapshot_id: String,
    pub instance_id: String,
    pub build_id: Option<String>,
    pub snapshot_type: i32,
    pub instance_data_path: String,
    pub storage_uri: Option<String>,
    pub manifest_uri: Option<String>,
    pub checksum: Option<String>,
    pub status: i32,
    pub source_node: Option<String>,
    pub created_at: String,
    pub completed_at: Option<String>,
    pub failure_message: Option<String>,
    pub bucket: String,
    pub key: String,
    pub host: String,
    pub host_port: i32,
}

/// 模组清单。
#[derive(Debug, Clone)]
pub struct ModManifest {
    pub manifest_id: String,
    pub game_id: String,
    pub mods: Vec<ModEntry>,
    pub config_hash: String,
    pub compatibility_note: Option<String>,
    pub created_at: String,
}

/// 模组清单中的单个条目。
#[derive(Debug, Clone)]
pub struct ModEntry {
    pub mod_id: String,
    pub version: String,
    pub required: bool,
}

/// 构建与模组兼容性检查结果。
#[derive(Debug, Clone)]
pub struct BuildCompatibility {
    pub compatible: bool,
    pub reason: Option<String>,
}

/// 节点代理信息。
#[derive(Debug, Clone)]
pub struct NodeAgentInfo {
    pub node_id: String,
    pub endpoint: String,
    pub status: String,
    pub last_heartbeat_at: i64,
}
