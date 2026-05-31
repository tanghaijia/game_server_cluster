use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::{BuildId, ModManifestId, SnapshotId};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum GameKind {
    Dst,
    Minecraft,
    Custom(String),
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum VersionSelector {
    Channel { channel: String },
    BuildId { build_id: String },
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum BuildStatus {
    Discovered,
    Resolving,
    Available,
    Deprecated,
    Unavailable,
    Deleted,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct GameBuild {
    pub build_id: BuildId,
    pub game: GameKind,
    pub channel: Option<String>,
    pub adapter_version: Option<String>,
    pub upstream_version: Option<String>,
    pub artifact_uri: Option<String>,
    pub checksum: Option<String>,
    pub status: BuildStatus,
    pub pinned: bool,
    pub resolved_at: DateTime<Utc>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum SnapshotType {
    Manual,
    Scheduled,
    PreUpgrade,
    FinalStop,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum SnapshotStatus {
    Pending,
    Running,
    Uploading,
    Completed,
    Failed,
    Expired,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SnapshotRecord {
    pub snapshot_id: SnapshotId,
    pub instance_id: String,
    pub build_id: Option<BuildId>,
    pub snapshot_type: SnapshotType,
    pub instance_data_path: String,
    pub storage_uri: Option<String>,
    pub manifest_uri: Option<String>,
    pub checksum: Option<String>,
    pub status: SnapshotStatus,
    pub source_node: Option<String>,
    pub created_at: DateTime<Utc>,
    pub completed_at: Option<DateTime<Utc>>,
    pub failure_message: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ModEntry {
    pub mod_id: String,
    pub version: String,
    pub required: bool,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ModManifest {
    pub manifest_id: ModManifestId,
    pub game: GameKind,
    pub mods: Vec<ModEntry>,
    pub config_hash: String,
    pub compatibility_note: Option<String>,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct BuildCompatibility {
    pub compatible: bool,
    pub reason: Option<String>,
}


#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SnapshotRestorePlan {
    pub snapshot_id: SnapshotId,
    pub build_id: Option<BuildId>,
    pub storage_uri: String,
    pub manifest_uri: Option<String>,
    pub checksum: Option<String>,
    pub instance_data_path: String,
}


pub fn instance_data_path(instance_id: &str) -> String {
    format!("/data/game-instances/{instance_id}")
}
