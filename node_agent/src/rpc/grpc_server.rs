use std::sync::Arc;

use tonic::{Request, Response, Status};

use crate::{
    domain::{
        BuildPreparation, BuildPreparationResult, DesiredRuntimeState, Endpoint, FailureInfo,
        GameBuild, InstanceAssignment, InstanceId, InstanceRuntimeRecord,
        InstanceRuntimeSpec, InstanceSpec, NodeId, NodeOperation, OperationId, OperationKind,
        OperationStatus, ResourceRequirements, RuntimeState, SnapshotArtifact, SnapshotCaptureRequest,
        SnapshotReference, SnapshotRestoreRequest, SnapshotRestoreResult,
    },
    error::NodeAgentError,
    ports::{
        AssetServiceFace, BuildRuntime, ImageClient, InstanceRuntime, OperationRepository,
        SnapshotRuntime, SystemInfoProvider,
    },
    proto::node_agent::{
        self,
        node_agent_service_server::NodeAgentService as NodeAgentRpc,
        BuildPreparationResult as ProtoBuildPreparationResult,
        CreateSnapshotRequest, CreateSnapshotResponse, FailureInfo as ProtoFailureInfo,
        GameBuild as ProtoGameBuild, GetHeartbeatRequest, GetHeartbeatResponse,
        GetOperationRequest, GetOperationResponse, InspectInstanceRequest,
        InspectInstanceResponse, InstanceRuntimeRecord as ProtoInstanceRuntimeRecord,
        InstanceRuntimeSpec as ProtoInstanceRuntimeSpec, InstanceSpec as ProtoInstanceSpec,
        NodeHeartbeat as ProtoNodeHeartbeat, NodeOperation as ProtoNodeOperation,
        PrepareGameBuildRequest, PrepareGameBuildResponse, SnapshotArtifact as ProtoSnapshotArtifact,
        SnapshotReference as ProtoSnapshotReference, SnapshotRestoreResult as ProtoSnapshotRestoreResult,
        StartInstanceRequest, StartInstanceResponse, StopInstanceRequest, StopInstanceResponse,
        RestoreSnapshotRequest as ProtoRestoreSnapshotRequest, RestoreSnapshotResponse,
    },
    service::NodeAgentService,
};

pub struct GrpcNodeAgentServer<I, P, O, S, A, IMC>
where
    I: InstanceRuntime,
    P: SnapshotRuntime,
    O: OperationRepository,
    S: SystemInfoProvider,
    A: AssetServiceFace,
    IMC: ImageClient,
{
    service: Arc<NodeAgentService<I, P, O, S, A, IMC>>,
}

impl<I, P, O, S, A, IMC> GrpcNodeAgentServer<I, P, O, S, A, IMC>
where
    I: InstanceRuntime,
    P: SnapshotRuntime,
    O: OperationRepository,
    S: SystemInfoProvider,
    A: AssetServiceFace,
    IMC: ImageClient,
{
    pub fn new(service: Arc<NodeAgentService<I, P, O, S, A, IMC>>) -> Self {
        Self { service }
    }
}

#[tonic::async_trait]
impl<I, P, O, S, A, IMC> NodeAgentRpc for GrpcNodeAgentServer<I, P, O, S, A, IMC>
where
    I: InstanceRuntime + 'static,
    P: SnapshotRuntime + 'static,
    O: OperationRepository + 'static,
    S: SystemInfoProvider + 'static,
    A: AssetServiceFace + 'static,
    IMC: ImageClient + 'static,
{
    async fn prepare_game_build(
        &self,
        request: Request<PrepareGameBuildRequest>,
    ) -> Result<Response<PrepareGameBuildResponse>, Status> {
        let request = request.into_inner();
        let build = request.build.ok_or_else(|| Status::invalid_argument("build is required"))?;
        let domain = BuildPreparation {
            node_id: NodeId(request.node_id),
            build: map_game_build(build)?,
        };
        let (operation, result) = self
            .service
            .prepare_game_build(domain)
            .await
            .map_err(map_error)?;
        Ok(Response::new(PrepareGameBuildResponse {
            operation: Some(map_operation(operation)),
            result: Some(map_build_preparation_result(result)),
        }))
    }

    async fn start_instance(
        &self,
        request: Request<StartInstanceRequest>,
    ) -> Result<Response<StartInstanceResponse>, Status> {
        let request = request.into_inner();
        let spec = request
            .instance
            .ok_or_else(|| Status::invalid_argument("instance is required"))?;
        let (operation, runtime) = self
            .service
            .start_instance(map_instance_runtime_spec(spec)?)
            .await
            .map_err(map_error)?;
        Ok(Response::new(StartInstanceResponse {
            operation: Some(map_operation(operation)),
            runtime: Some(map_runtime_record(runtime)),
        }))
    }

    async fn stop_instance(
        &self,
        request: Request<StopInstanceRequest>,
    ) -> Result<Response<StopInstanceResponse>, Status> {
        let request = request.into_inner();
        let operation = self
            .service
            .stop_instance(InstanceId(request.instance_id))
            .await
            .map_err(map_error)?;
        Ok(Response::new(StopInstanceResponse {
            operation: Some(map_operation(operation)),
        }))
    }

    async fn create_snapshot(
        &self,
        request: Request<CreateSnapshotRequest>,
    ) -> Result<Response<CreateSnapshotResponse>, Status> {
        let request = request.into_inner();
        let (operation, snapshot) = self
            .service
            .create_snapshot(SnapshotCaptureRequest {
                instance_id: InstanceId(request.instance_id),
                snapshot_id: request.snapshot_id,
            })
            .await
            .map_err(map_error)?;
        Ok(Response::new(CreateSnapshotResponse {
            operation: Some(map_operation(operation)),
            snapshot: Some(map_snapshot_artifact(snapshot)),
        }))
    }

    async fn restore_snapshot(
        &self,
        request: Request<ProtoRestoreSnapshotRequest>,
    ) -> Result<Response<RestoreSnapshotResponse>, Status> {
        let request = request.into_inner();
        let (operation, result) = self
            .service
            .restore_snapshot(SnapshotRestoreRequest {
                instance_id: InstanceId(request.instance_id),
                snapshot_id: request.snapshot_id,
                storage_uri: request.storage_uri,
                manifest_uri: request.manifest_uri,
                checksum: request.checksum,
            })
            .await
            .map_err(map_error)?;
        Ok(Response::new(RestoreSnapshotResponse {
            operation: Some(map_operation(operation)),
            result: Some(map_snapshot_restore_result(result)),
        }))
    }

    async fn get_operation(
        &self,
        request: Request<GetOperationRequest>,
    ) -> Result<Response<GetOperationResponse>, Status> {
        let request = request.into_inner();
        let operation = self
            .service
            .get_operation(&OperationId(request.operation_id))
            .await
            .map_err(map_error)?;
        Ok(Response::new(GetOperationResponse {
            operation: Some(map_operation(operation)),
        }))
    }

    async fn inspect_instance(
        &self,
        request: Request<InspectInstanceRequest>,
    ) -> Result<Response<InspectInstanceResponse>, Status> {
        let request = request.into_inner();
        let runtime = self
            .service
            .inspect_instance(&InstanceId(request.instance_id))
            .await
            .map_err(map_error)?;
        Ok(Response::new(InspectInstanceResponse {
            runtime: Some(map_runtime_record(runtime)),
        }))
    }

    async fn get_heartbeat(
        &self,
        _request: Request<GetHeartbeatRequest>,
    ) -> Result<Response<GetHeartbeatResponse>, Status> {
        let heartbeat = self.service.heartbeat().await.map_err(map_error)?;
        Ok(Response::new(GetHeartbeatResponse {
            heartbeat: Some(ProtoNodeHeartbeat {
                node_id: heartbeat.node_id.0,
                cpu_usage_pct: heartbeat.cpu_usage_pct,
                memory_usage_pct: heartbeat.memory_usage_pct,
                disk_usage_pct: heartbeat.disk_usage_pct,
                running_instances: heartbeat.running_instances,
            }),
        }))
    }
}

fn map_error(error: NodeAgentError) -> Status {
    match error {
        NodeAgentError::InvalidRequest { message } => Status::invalid_argument(message),
        NodeAgentError::InstanceNotFound { instance_id } => Status::not_found(instance_id),
        NodeAgentError::BuildPreparationFailed { message }
        | NodeAgentError::InstanceRuntimeFailed { message }
        | NodeAgentError::Internal { message }
        | NodeAgentError::ImageRepositoryRequestFail { message } => Status::internal(message),
        | NodeAgentError::DBOperationFail { message } => Status::internal(message),
    }
}

fn map_game_build(value: ProtoGameBuild) -> Result<GameBuild, Status> {
    let game = value.game.ok_or_else(|| Status::invalid_argument("game is required"))?;
    Ok(GameBuild {
        build_id: value.build_id,
        game: map_game(game)?,
        channel: value.channel,
        adapter_version: value.adapter_version,
    })
}

fn map_instance_runtime_spec(value: ProtoInstanceRuntimeSpec) -> Result<InstanceRuntimeSpec, Status> {
    let build = map_game_build(value.build.ok_or_else(|| Status::invalid_argument("build is required"))?)?;
    Ok(InstanceRuntimeSpec {
        instance_id: InstanceId(value.instance_id),
        game: build.game.clone(),
        build,
        desired_state: match node_agent::DesiredRuntimeState::try_from(value.desired_state)
            .unwrap_or(node_agent::DesiredRuntimeState::Unspecified)
        {
            node_agent::DesiredRuntimeState::Running => DesiredRuntimeState::Running,
            node_agent::DesiredRuntimeState::Stopped => DesiredRuntimeState::Stopped,
            node_agent::DesiredRuntimeState::Unspecified => {
                return Err(Status::invalid_argument("desired_state is required"))
            }
        },
        spec: map_instance_spec(value.spec.ok_or_else(|| Status::invalid_argument("spec is required"))?)?,
        assignment: InstanceAssignment {
            node_id: NodeId(
                value
                    .assignment
                    .ok_or_else(|| Status::invalid_argument("assignment is required"))?
                    .node_id,
            ),
        },
        latest_snapshot: value.latest_snapshot.map(map_snapshot_reference),
    })
}

fn map_instance_spec(value: ProtoInstanceSpec) -> Result<InstanceSpec, Status> {
    let resources = value.resources.ok_or_else(|| Status::invalid_argument("resources are required"))?;
    Ok(InstanceSpec {
        cluster_name: value.cluster_name,
        max_players: value.max_players as u16,
        resources: ResourceRequirements {
            cpu_millis: resources.cpu_millis,
            memory_mb: resources.memory_mb,
            disk_mb: resources.disk_mb,
        },
        world_preset: value.world_preset,
        mod_manifest_id: value.mod_manifest_id,
    })
}

fn map_snapshot_reference(value: ProtoSnapshotReference) -> SnapshotReference {
    SnapshotReference {
        snapshot_id: value.snapshot_id,
        storage_uri: value.storage_uri,
        manifest_uri: value.manifest_uri,
        checksum: value.checksum,
    }
}

fn map_game(value: node_agent::Game) -> Result<crate::domain::Game, Status> {
    Ok(crate::domain::Game {
        id: value.id,
        name: value.name,
        app_id: value.app_id,
    })
}

fn map_operation(value: NodeOperation) -> ProtoNodeOperation {
    ProtoNodeOperation {
        operation_id: value.operation_id.0,
        kind: match value.kind {
            OperationKind::PrepareBuild => node_agent::OperationKind::PrepareBuild as i32,
            OperationKind::StartInstance => node_agent::OperationKind::StartInstance as i32,
            OperationKind::StopInstance => node_agent::OperationKind::StopInstance as i32,
            OperationKind::CreateSnapshot => node_agent::OperationKind::CreateSnapshot as i32,
            OperationKind::RestoreSnapshot => node_agent::OperationKind::RestoreSnapshot as i32,
        },
        status: match value.status {
            OperationStatus::Pending => node_agent::OperationStatus::Pending as i32,
            OperationStatus::Running => node_agent::OperationStatus::Running as i32,
            OperationStatus::Succeeded => node_agent::OperationStatus::Succeeded as i32,
            OperationStatus::Failed => node_agent::OperationStatus::Failed as i32,
        },
        instance_id: value.instance_id.map(|id| id.0),
        build_id: value.build_id,
        message: value.message,
        started_at: value.started_at.to_rfc3339(),
        finished_at: value.finished_at.map(|v| v.to_rfc3339()),
    }
}

fn map_build_preparation_result(value: BuildPreparationResult) -> ProtoBuildPreparationResult {
    ProtoBuildPreparationResult {
        build_root: value.build_root,
        prepared_at: value.prepared_at.to_rfc3339(),
    }
}

fn map_snapshot_artifact(value: SnapshotArtifact) -> ProtoSnapshotArtifact {
    ProtoSnapshotArtifact {
        snapshot_id: value.snapshot_id,
        instance_data_path: value.instance_data_path,
        storage_uri: value.storage_uri,
        manifest_uri: value.manifest_uri,
        checksum: value.checksum,
        captured_at: value.captured_at.to_rfc3339(),
    }
}

fn map_snapshot_restore_result(value: SnapshotRestoreResult) -> ProtoSnapshotRestoreResult {
    ProtoSnapshotRestoreResult {
        snapshot_id: value.snapshot_id,
        restore_path: value.restore_path,
        restored_at: value.restored_at.to_rfc3339(),
    }
}

fn map_runtime_record(value: InstanceRuntimeRecord) -> ProtoInstanceRuntimeRecord {
    ProtoInstanceRuntimeRecord {
        instance_id: value.instance_id.0,
        node_id: value.node_id.0,
        state: match value.state {
            RuntimeState::Pending => node_agent::RuntimeState::Pending as i32,
            RuntimeState::BuildPrepared => node_agent::RuntimeState::BuildPrepared as i32,
            RuntimeState::Starting => node_agent::RuntimeState::Starting as i32,
            RuntimeState::Running => node_agent::RuntimeState::Running as i32,
            RuntimeState::Stopping => node_agent::RuntimeState::Stopping as i32,
            RuntimeState::Stopped => node_agent::RuntimeState::Stopped as i32,
            RuntimeState::Failed => node_agent::RuntimeState::Failed as i32,
        },
        endpoint: value.endpoint.map(map_endpoint),
        failure: value.failure.map(map_failure),
        updated_at: value.updated_at.to_rfc3339(),
    }
}

fn map_endpoint(value: Endpoint) -> node_agent::Endpoint {
    node_agent::Endpoint {
        host: value.host,
        game_port: value.game_port as u32,
        query_port: value.query_port.map(|v| v as u32),
    }
}

fn map_failure(value: FailureInfo) -> ProtoFailureInfo {
    ProtoFailureInfo {
        message: value.message,
        retryable: value.retryable,
    }
}
