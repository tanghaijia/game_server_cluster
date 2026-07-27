use super::{InstanceId, NodeId, OperationId};
use crate::domain::game::Game;
use crate::domain::game_build::GameBuild;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/// 从 asset_service 获取的快照恢复计划。
#[derive(Debug, Clone)]
pub struct SnapshotRestorePlan {
    pub snapshot_id: String,
    pub build_id: Option<String>,
    pub storage_uri: String,
    pub manifest_uri: Option<String>,
    pub checksum: Option<String>,
    pub instance_data_path: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ResourceRequirements {
    pub cpu_millis: u32,
    pub memory_mb: u32,
    pub disk_mb: u32,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct InstanceSpec {
    pub cluster_name: String,
    pub max_players: u16,
    pub resources: ResourceRequirements,
    pub world_preset: Option<String>,
    pub mod_manifest_id: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SnapshotReference {
    pub snapshot_id: String,
    pub storage_uri: Option<String>,
    pub manifest_uri: Option<String>,
    pub checksum: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct InstanceAssignment {
    pub node_id: NodeId,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Endpoint {
    pub host: String,
    pub game_port: u16,
    pub query_port: Option<u16>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum DesiredRuntimeState {
    Running,
    Stopped,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct StartInstanceArgument {
    pub instance_id: InstanceId,
    pub game: Game,
    pub build: GameBuild,
    pub desired_state: DesiredRuntimeState,
    pub spec: InstanceSpec,
    pub assignment: InstanceAssignment,
    pub latest_snapshot: Option<SnapshotReference>,
    pub container_server_path: String,
    pub branch_name: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum RuntimeState {
    Pending,
    BuildPrepared,
    Starting,
    Running,
    Stopping,
    Stopped,
    Failed,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum OperationKind {
    PrepareBuild,
    StartInstance,
    StopInstance,
    CreateSnapshot,
    RestoreSnapshot,
    CleanInstance,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum OperationStatus {
    Pending,
    Running,
    Succeeded,
    Failed,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct FailureInfo {
    pub message: String,
    pub retryable: bool,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct BuildPreparation {
    pub build_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct BuildPreparationResult {
    pub build_root: String,
    pub prepared_at: DateTime<Utc>,
    pub build_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SnapshotCaptureRequest {
    pub instance_id: InstanceId,
    pub snapshot_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SnapshotRestoreRequest {
    pub instance_id: InstanceId,
    pub snapshot_id: String,
    pub storage_uri: String,
    pub manifest_uri: Option<String>,
    pub checksum: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SnapshotRestoreResult {
    pub snapshot_id: String,
    pub restore_path: String,
    pub restored_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct InstanceRuntimeRecord {
    pub instance_id: InstanceId,
    pub node_id: NodeId,
    pub state: RuntimeState,
    pub endpoint: Option<Endpoint>,
    pub failure: Option<FailureInfo>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct NodeOperation {
    pub operation_id: OperationId,
    pub kind: OperationKind,
    pub status: OperationStatus,
    pub instance_id: Option<InstanceId>,
    pub build_id: Option<String>,
    pub message: Option<String>,
    pub started_at: DateTime<Utc>,
    pub finished_at: Option<DateTime<Utc>>,
}

pub fn instance_data_path(instance_id: &InstanceId) -> String {
    format!("/data/game-instances/{}", instance_id.0)
}
