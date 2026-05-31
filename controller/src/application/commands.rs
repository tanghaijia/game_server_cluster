use serde::{Deserialize, Serialize};

use crate::domain::{
    Endpoint, GameBuild, GameInstance, GameKind, InstanceId, InstanceSpec, NodeAssignment,
    NodeId, ResourceRequirements, RuntimeState, SnapshotReference, SnapshotType, VersionSelector,
};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CreateInstanceRequest {
    pub game: GameKind,
    pub version_selector: VersionSelector,
    pub spec: InstanceSpec,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CreateInstanceResponse {
    pub instance: GameInstance,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RequestStopInstance {
    pub instance_id: InstanceId,
    pub reason: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CreateSnapshotRequest {
    pub instance_id: InstanceId,
    pub snapshot_type: SnapshotType,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CreateSnapshotResponse {
    pub instance: GameInstance,
    pub snapshot: SnapshotReference,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RestoreSnapshotRequest {
    pub instance_id: InstanceId,
    pub snapshot_id: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RestoreSnapshotResponse {
    pub instance: GameInstance,
    pub snapshot: SnapshotReference,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReconcileInstanceRequest {
    pub instance_id: InstanceId,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReconcileInstanceResponse {
    pub instance: GameInstance,
    pub last_action: ReconcileAction,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum ReconcileAction {
    NoOp,
    ResolvedBuild { build: GameBuild },
    AssignedNode { assignment: NodeAssignment },
    BuildPreparationRequested { node_id: NodeId, build: GameBuild },
    StartRequested { node_id: NodeId, endpoint: Option<Endpoint> },
    MarkedRunning { endpoint: Option<Endpoint> },
    StopRequested,
    MarkedStopped,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScheduleRequest {
    pub game: GameKind,
    pub build: GameBuild,
    pub resources: ResourceRequirements,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScheduleResponse {
    pub assignment: NodeAssignment,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RuntimeStatusReport {
    pub instance_id: InstanceId,
    pub state: RuntimeState,
    pub endpoint: Option<Endpoint>,
    pub message: Option<String>,
}
