use serde::{Deserialize, Serialize};

use crate::error::ControllerError;

use super::{NodeId, SnapshotId};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum GameKind {
    Dst,
    Minecraft,
    Custom(String),
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum DesiredState {
    Running,
    Stopped,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum RuntimeState {
    Pending,
    RelocationPending,
    ResolvingBuild,
    Scheduling,
    PreparingBuild,
    RestoringSnapshot,
    Starting,
    Running,
    StopRequested,
    Stopping,
    Stopped,
    Failed,
}

impl RuntimeState {
    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Pending => "Pending",
            Self::RelocationPending => "RelocationPending",
            Self::ResolvingBuild => "ResolvingBuild",
            Self::Scheduling => "Scheduling",
            Self::PreparingBuild => "PreparingBuild",
            Self::RestoringSnapshot => "RestoringSnapshot",
            Self::Starting => "Starting",
            Self::Running => "Running",
            Self::StopRequested => "StopRequested",
            Self::Stopping => "Stopping",
            Self::Stopped => "Stopped",
            Self::Failed => "Failed",
        }
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum VersionSelector {
    Channel { channel: String },
    BuildId { build_id: String },
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct GameBuild {
    pub build_id: String,
    pub game: GameKind,
    pub channel: Option<String>,
    pub adapter_version: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ResourceRequirements {
    pub cpu_millis: u32,
    pub memory_mb: u32,
    pub disk_mb: u32,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct NodeAssignment {
    pub node_id: NodeId,
    pub reason: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Endpoint {
    pub host: String,
    pub game_port: u16,
    pub query_port: Option<u16>,
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
pub struct FailureInfo {
    pub step: String,
    pub reason: String,
    pub retryable: bool,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SnapshotReference {
    pub snapshot_id: SnapshotId,
    pub storage_uri: Option<String>,
    pub manifest_uri: Option<String>,
    pub checksum: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum SnapshotType {
    Manual,
    Scheduled,
    PreUpgrade,
    FinalStop,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct StartInstanceSpec {
    pub build: GameBuild,
    pub data_snapshot: Option<SnapshotReference>,
    pub desired_state: DesiredState,
}

pub fn ensure_state(
    instance_id: &str,
    current: &RuntimeState,
    allowed: &[RuntimeState],
    action: &'static str,
) -> Result<(), ControllerError> {
    if allowed.contains(current) {
        return Ok(());
    }

    Err(ControllerError::InvalidStateTransition {
        instance_id: instance_id.to_string(),
        action,
        state: current.as_str(),
    })
}


pub fn instance_data_path(instance_id: &str) -> String {
    format!("/data/game-instances/{instance_id}")
}
