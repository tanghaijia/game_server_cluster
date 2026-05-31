use async_trait::async_trait;

use crate::{
    domain::{Endpoint, GameBuild, GameInstance, InstanceId, NodeId},
    error::ControllerError,
    ports::SnapshotRestorePlan,
};

#[derive(Debug, Clone)]
pub struct PrepareBuildRequest {
    pub node_id: NodeId,
    pub build: GameBuild,
}

#[derive(Debug, Clone)]
pub struct StartInstanceRequest {
    pub node_id: NodeId,
    pub instance: GameInstance,
}

#[derive(Debug, Clone)]
pub struct StartInstanceResponse {
    pub endpoint: Option<Endpoint>,
}

#[derive(Debug, Clone)]
pub struct StopInstanceRequest {
    pub node_id: NodeId,
    pub instance: GameInstance,
}

#[derive(Debug, Clone)]
pub struct CreateSnapshotRequest {
    pub node_id: NodeId,
    pub instance: GameInstance,
    pub snapshot_id: String,
}

#[derive(Debug, Clone)]
pub struct CreateSnapshotResponse {
    pub storage_uri: String,
    pub manifest_uri: Option<String>,
    pub checksum: Option<String>,
}

#[derive(Debug, Clone)]
pub struct RestoreSnapshotRequest {
    pub node_id: NodeId,
    pub instance_id: InstanceId,
    pub snapshot: SnapshotRestorePlan,
}

#[derive(Debug, Clone)]
pub struct RestoreSnapshotResponse {
    pub restored_path: String,
}

#[async_trait]
pub trait NodeAgentClient: Send + Sync {
    async fn prepare_game_build(
        &self,
        request: PrepareBuildRequest,
    ) -> Result<(), ControllerError>;

    async fn start_instance(
        &self,
        request: StartInstanceRequest,
    ) -> Result<StartInstanceResponse, ControllerError>;

    async fn stop_instance(&self, request: StopInstanceRequest) -> Result<(), ControllerError>;

    async fn create_snapshot(
        &self,
        request: CreateSnapshotRequest,
    ) -> Result<CreateSnapshotResponse, ControllerError>;

    async fn restore_snapshot(
        &self,
        request: RestoreSnapshotRequest,
    ) -> Result<RestoreSnapshotResponse, ControllerError>;
}
