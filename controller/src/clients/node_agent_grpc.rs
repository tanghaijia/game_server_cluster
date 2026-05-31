use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::Mutex;
use tonic::transport::Channel;

use crate::{
    domain::{DesiredState, Endpoint, GameInstance, NodeId},
    error::ControllerError,
    ports::{
        CreateSnapshotRequest, CreateSnapshotResponse, NodeAgentClient, PrepareBuildRequest,
        RestoreSnapshotRequest, RestoreSnapshotResponse, StartInstanceRequest,
        StartInstanceResponse, StopInstanceRequest,
    },
    proto::node_agent::{
        self, node_agent_service_client::NodeAgentServiceClient,
    },
};

#[derive(Clone)]
pub struct NodeAgentGrpcClient {
    inner: Arc<Mutex<NodeAgentServiceClient<Channel>>>,
}

impl NodeAgentGrpcClient {
    pub async fn connect(endpoint: String) -> Result<Self, tonic::transport::Error> {
        let client = NodeAgentServiceClient::connect(endpoint).await?;
        Ok(Self { inner: Arc::new(Mutex::new(client)) })
    }
}

#[async_trait]
impl NodeAgentClient for NodeAgentGrpcClient {
    async fn prepare_game_build(&self, request: PrepareBuildRequest) -> Result<(), ControllerError> {
        let mut client = self.inner.lock().await;
        client.prepare_game_build(node_agent::PrepareGameBuildRequest {
            node_id: request.node_id.0,
            build: Some(map_build(request.build)),
        }).await.map_err(map_status)?;
        Ok(())
    }

    async fn start_instance(&self, request: StartInstanceRequest) -> Result<StartInstanceResponse, ControllerError> {
        let mut client = self.inner.lock().await;
        let response = client.start_instance(node_agent::StartInstanceRequest {
            instance: Some(map_instance_runtime_spec(request.instance, request.node_id)),
        }).await.map_err(map_status)?.into_inner();
        let endpoint = response.runtime.and_then(|runtime| runtime.endpoint).map(map_endpoint_from_proto);
        Ok(StartInstanceResponse { endpoint })
    }

    async fn stop_instance(&self, request: StopInstanceRequest) -> Result<(), ControllerError> {
        let mut client = self.inner.lock().await;
        client.stop_instance(node_agent::StopInstanceRequest {
            instance_id: request.instance.instance_id.0,
        }).await.map_err(map_status)?;
        Ok(())
    }

    async fn create_snapshot(&self, request: CreateSnapshotRequest) -> Result<CreateSnapshotResponse, ControllerError> {
        let mut client = self.inner.lock().await;
        let response = client.create_snapshot(node_agent::CreateSnapshotRequest {
            instance_id: request.instance.instance_id.0,
            snapshot_id: request.snapshot_id,
        }).await.map_err(map_status)?.into_inner();
        let snapshot = response.snapshot.ok_or_else(|| ControllerError::DependencyFailure {
            message: "node-agent returned empty snapshot artifact".to_string(),
        })?;
        Ok(CreateSnapshotResponse {
            storage_uri: snapshot.storage_uri,
            manifest_uri: snapshot.manifest_uri,
            checksum: snapshot.checksum,
        })
    }

    async fn restore_snapshot(&self, request: RestoreSnapshotRequest) -> Result<RestoreSnapshotResponse, ControllerError> {
        let mut client = self.inner.lock().await;
        let response = client.restore_snapshot(node_agent::RestoreSnapshotRequest {
            instance_id: request.instance_id.0,
            snapshot_id: request.snapshot.snapshot_id.0,
            storage_uri: request.snapshot.storage_uri,
            manifest_uri: request.snapshot.manifest_uri,
            checksum: request.snapshot.checksum,
        }).await.map_err(map_status)?.into_inner();
        let result = response.result.ok_or_else(|| ControllerError::DependencyFailure {
            message: "node-agent returned empty restore result".to_string(),
        })?;
        Ok(RestoreSnapshotResponse { restored_path: result.restore_path })
    }
}

fn map_status(status: tonic::Status) -> ControllerError {
    match status.code() {
        tonic::Code::NotFound => ControllerError::InstanceNotFound { instance_id: status.message().to_string() },
        tonic::Code::FailedPrecondition | tonic::Code::AlreadyExists => ControllerError::Conflict { message: status.message().to_string() },
        _ => ControllerError::DependencyFailure { message: status.to_string() },
    }
}

fn map_build(value: crate::domain::GameBuild) -> node_agent::GameBuild {
    node_agent::GameBuild {
        build_id: value.build_id,
        game: match value.game {
            crate::domain::GameKind::Dst => node_agent::GameKind::Dst as i32,
            crate::domain::GameKind::Minecraft => node_agent::GameKind::Minecraft as i32,
            crate::domain::GameKind::Custom(_) => node_agent::GameKind::Custom as i32,
        },
        channel: value.channel,
        adapter_version: value.adapter_version,
        custom_game: match value.game { crate::domain::GameKind::Custom(name) => Some(name), _ => None },
    }
}

fn map_instance_runtime_spec(value: GameInstance, node_id: NodeId) -> node_agent::InstanceRuntimeSpec {
    let build = value.resolved_build.clone().expect("resolved build required before start");
    node_agent::InstanceRuntimeSpec {
        instance_id: value.instance_id.0,
        game: match value.game {
            crate::domain::GameKind::Dst => node_agent::GameKind::Dst as i32,
            crate::domain::GameKind::Minecraft => node_agent::GameKind::Minecraft as i32,
            crate::domain::GameKind::Custom(_) => node_agent::GameKind::Custom as i32,
        },
        build: Some(map_build(build)),
        desired_state: match value.desired_state {
            DesiredState::Running => node_agent::DesiredRuntimeState::Running as i32,
            DesiredState::Stopped => node_agent::DesiredRuntimeState::Stopped as i32,
        },
        spec: Some(node_agent::InstanceSpec {
            cluster_name: value.spec.cluster_name,
            max_players: value.spec.max_players as u32,
            resources: Some(node_agent::ResourceRequirements {
                cpu_millis: value.spec.resources.cpu_millis,
                memory_mb: value.spec.resources.memory_mb,
                disk_mb: value.spec.resources.disk_mb,
            }),
            world_preset: value.spec.world_preset,
            mod_manifest_id: value.spec.mod_manifest_id,
        }),
        assignment: Some(node_agent::InstanceAssignment { node_id: node_id.0 }),
        latest_snapshot: value.latest_snapshot.map(|snapshot| node_agent::SnapshotReference {
            snapshot_id: snapshot.snapshot_id.0,
            storage_uri: snapshot.storage_uri,
            manifest_uri: snapshot.manifest_uri,
            checksum: snapshot.checksum,
        }),
    }
}

fn map_endpoint_from_proto(value: node_agent::Endpoint) -> Endpoint {
    Endpoint {
        host: value.host,
        game_port: value.game_port as u16,
        query_port: value.query_port.map(|v| v as u16),
    }
}
