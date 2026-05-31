use std::sync::Arc;

use tonic::{Request, Response, Status};

use crate::{
    application::commands::{
        CreateInstanceRequest, CreateSnapshotRequest, ReconcileInstanceRequest,
        RequestStopInstance, RestoreSnapshotRequest, RuntimeStatusReport,
    },
    domain::{
        Endpoint, GameBuild, GameInstance, GameKind, InstanceId, InstanceSpec, NodeAssignment,
        ResourceRequirements, RuntimeState, SnapshotReference, SnapshotType,
        VersionSelector,
    },
    ports::{BuildResolver, Clock, InstanceRepository, NodeAgentClient, Scheduler, SnapshotService},
    proto::controller::{
        self,
        controller_service_server::ControllerService as ControllerRpc,
        CreateInstanceRequest as ProtoCreateInstanceRequest,
        CreateInstanceResponse as ProtoCreateInstanceResponse,
        CreateSnapshotRequest as ProtoCreateSnapshotRequest,
        CreateSnapshotResponse as ProtoCreateSnapshotResponse,
        GetInstanceRequest, GetInstanceResponse, ListInstancesRequest, ListInstancesResponse,
        ReconcileAction as ProtoReconcileAction, ReconcileInstanceRequest as ProtoReconcileInstanceRequest,
        ReconcileInstanceResponse as ProtoReconcileInstanceResponse,
        ReportRuntimeStatusRequest, ReportRuntimeStatusResponse,
        RequestStopInstanceRequest as ProtoRequestStopInstanceRequest,
        RequestStopInstanceResponse as ProtoRequestStopInstanceResponse,
        RestoreSnapshotRequest as ProtoRestoreSnapshotRequest,
        RestoreSnapshotResponse as ProtoRestoreSnapshotResponse,
    },
    service::ControllerService,
};

pub struct GrpcControllerServer<R, B, S, N, P, C>
where
    R: InstanceRepository,
    B: BuildResolver,
    S: Scheduler,
    N: NodeAgentClient,
    P: SnapshotService,
    C: Clock,
{
    service: Arc<ControllerService<R, B, S, N, P, C>>,
}

impl<R, B, S, N, P, C> GrpcControllerServer<R, B, S, N, P, C>
where
    R: InstanceRepository,
    B: BuildResolver,
    S: Scheduler,
    N: NodeAgentClient,
    P: SnapshotService,
    C: Clock,
{
    pub fn new(service: Arc<ControllerService<R, B, S, N, P, C>>) -> Self {
        Self { service }
    }
}

#[tonic::async_trait]
impl<R, B, S, N, P, C> ControllerRpc for GrpcControllerServer<R, B, S, N, P, C>
where
    R: InstanceRepository + 'static,
    B: BuildResolver + 'static,
    S: Scheduler + 'static,
    N: NodeAgentClient + 'static,
    P: SnapshotService + 'static,
    C: Clock + 'static,
{
    async fn create_instance(&self, request: Request<ProtoCreateInstanceRequest>) -> Result<Response<ProtoCreateInstanceResponse>, Status> {
        let request = request.into_inner();
        let response = self.service.create_instance(CreateInstanceRequest {
            game: map_game_kind(request.game, request.spec.as_ref().and_then(|_| request.custom_game())),
            version_selector: map_version_selector(request.version_selector.ok_or_else(|| Status::invalid_argument("version_selector is required"))?)?,
            spec: map_instance_spec(request.spec.ok_or_else(|| Status::invalid_argument("spec is required"))?)?,
        }).await.map_err(map_error)?;
        Ok(Response::new(ProtoCreateInstanceResponse {
            instance: Some(map_instance(response.instance)),
        }))
    }

    async fn get_instance(&self, request: Request<GetInstanceRequest>) -> Result<Response<GetInstanceResponse>, Status> {
        let response = self.service.get_instance(&request.into_inner().instance_id).await.map_err(map_error)?;
        Ok(Response::new(GetInstanceResponse {
            instance: Some(map_instance(response)),
        }))
    }

    async fn list_instances(&self, request: Request<ListInstancesRequest>) -> Result<Response<ListInstancesResponse>, Status> {
        let request = request.into_inner();
        let mut instances = self.service.list_unfinished_instances().await.map_err(map_error)?;
        if !request.runtime_states.is_empty() {
            let allowed: Vec<RuntimeState> = request.runtime_states.into_iter().filter_map(map_runtime_state_from_i32).collect();
            instances.retain(|instance| allowed.iter().any(|state| state == &instance.runtime_state));
        }
        Ok(Response::new(ListInstancesResponse {
            instances: instances.into_iter().take(request.page_size.max(100) as usize).map(map_instance).collect(),
            next_page_token: String::new(),
        }))
    }

    async fn request_stop_instance(&self, request: Request<ProtoRequestStopInstanceRequest>) -> Result<Response<ProtoRequestStopInstanceResponse>, Status> {
        let request = request.into_inner();
        let response = self.service.request_stop(RequestStopInstance {
            instance_id: InstanceId(request.instance_id),
            reason: request.reason,
        }).await.map_err(map_error)?;
        Ok(Response::new(ProtoRequestStopInstanceResponse {
            instance: Some(map_instance(response)),
        }))
    }

    async fn create_snapshot(&self, request: Request<ProtoCreateSnapshotRequest>) -> Result<Response<ProtoCreateSnapshotResponse>, Status> {
        let request = request.into_inner();
        let response = self.service.create_snapshot(CreateSnapshotRequest {
            instance_id: InstanceId(request.instance_id),
            snapshot_type: map_snapshot_type(request.snapshot_type)?,
        }).await.map_err(map_error)?;
        Ok(Response::new(ProtoCreateSnapshotResponse {
            instance: Some(map_instance(response.instance)),
            snapshot: Some(map_snapshot_reference(response.snapshot)),
        }))
    }

    async fn restore_snapshot(&self, request: Request<ProtoRestoreSnapshotRequest>) -> Result<Response<ProtoRestoreSnapshotResponse>, Status> {
        let request = request.into_inner();
        let response = self.service.restore_snapshot(RestoreSnapshotRequest {
            instance_id: InstanceId(request.instance_id),
            snapshot_id: request.snapshot_id,
        }).await.map_err(map_error)?;
        Ok(Response::new(ProtoRestoreSnapshotResponse {
            instance: Some(map_instance(response.instance)),
            snapshot: Some(map_snapshot_reference(response.snapshot)),
        }))
    }

    async fn reconcile_instance(&self, request: Request<ProtoReconcileInstanceRequest>) -> Result<Response<ProtoReconcileInstanceResponse>, Status> {
        let request = request.into_inner();
        let response = self.service.reconcile_instance(ReconcileInstanceRequest {
            instance_id: InstanceId(request.instance_id),
        }).await.map_err(map_error)?;
        Ok(Response::new(ProtoReconcileInstanceResponse {
            instance: Some(map_instance(response.instance)),
            last_action: Some(map_reconcile_action(response.last_action)),
        }))
    }

    async fn report_runtime_status(&self, request: Request<ReportRuntimeStatusRequest>) -> Result<Response<ReportRuntimeStatusResponse>, Status> {
        let request = request.into_inner();
        let response = self.service.report_runtime_status(RuntimeStatusReport {
            instance_id: InstanceId(request.instance_id),
            state: map_runtime_state(request.runtime_state)?,
            endpoint: request.endpoint.map(map_endpoint_from_proto),
            message: request.message,
        }).await.map_err(map_error)?;
        Ok(Response::new(ReportRuntimeStatusResponse {
            instance: Some(map_instance(response)),
        }))
    }
}

fn map_error(error: crate::error::ControllerError) -> Status {
    match error {
        crate::error::ControllerError::InstanceNotFound { instance_id } => Status::not_found(instance_id),
        crate::error::ControllerError::InvalidStateTransition { action, state, .. } => Status::failed_precondition(format!("invalid state transition for {action}: {state}")),
        crate::error::ControllerError::DependencyFailure { message } => Status::internal(message),
        crate::error::ControllerError::Conflict { message } => Status::failed_precondition(message),
    }
}

trait RequestCustomGame {
    fn custom_game(&self) -> Option<String>;
}

impl RequestCustomGame for ProtoCreateInstanceRequest {
    fn custom_game(&self) -> Option<String> {
        None
    }
}

fn map_game_kind(value: i32, custom: Option<String>) -> GameKind {
    match controller::GameKind::try_from(value).unwrap_or(controller::GameKind::Unspecified) {
        controller::GameKind::Dst => GameKind::Dst,
        controller::GameKind::Minecraft => GameKind::Minecraft,
        controller::GameKind::Custom => GameKind::Custom(custom.unwrap_or_else(|| "custom".to_string())),
        controller::GameKind::Unspecified => GameKind::Dst,
    }
}

fn map_version_selector(value: controller::VersionSelector) -> Result<VersionSelector, Status> {
    match value.selector {
        Some(controller::version_selector::Selector::Channel(channel)) => Ok(VersionSelector::Channel { channel }),
        Some(controller::version_selector::Selector::BuildId(build_id)) => Ok(VersionSelector::BuildId { build_id }),
        None => Err(Status::invalid_argument("version selector is required")),
    }
}

fn map_instance_spec(value: controller::InstanceSpec) -> Result<InstanceSpec, Status> {
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

fn map_runtime_state(value: i32) -> Result<RuntimeState, Status> {
    map_runtime_state_from_i32(value).ok_or_else(|| Status::invalid_argument("invalid runtime_state"))
}

fn map_runtime_state_from_i32(value: i32) -> Option<RuntimeState> {
    Some(match controller::RuntimeState::try_from(value).ok()? {
        controller::RuntimeState::Pending => RuntimeState::Pending,
        controller::RuntimeState::RelocationPending => RuntimeState::RelocationPending,
        controller::RuntimeState::ResolvingBuild => RuntimeState::ResolvingBuild,
        controller::RuntimeState::Scheduling => RuntimeState::Scheduling,
        controller::RuntimeState::PreparingBuild => RuntimeState::PreparingBuild,
        controller::RuntimeState::RestoringSnapshot => RuntimeState::RestoringSnapshot,
        controller::RuntimeState::Starting => RuntimeState::Starting,
        controller::RuntimeState::Running => RuntimeState::Running,
        controller::RuntimeState::StopRequested => RuntimeState::StopRequested,
        controller::RuntimeState::Stopping => RuntimeState::Stopping,
        controller::RuntimeState::Stopped => RuntimeState::Stopped,
        controller::RuntimeState::Failed => RuntimeState::Failed,
        controller::RuntimeState::Unspecified => return None,
    })
}

fn map_snapshot_type(value: i32) -> Result<SnapshotType, Status> {
    Ok(match controller::SnapshotType::try_from(value).unwrap_or(controller::SnapshotType::Unspecified) {
        controller::SnapshotType::Manual => SnapshotType::Manual,
        controller::SnapshotType::Scheduled => SnapshotType::Scheduled,
        controller::SnapshotType::PreUpgrade => SnapshotType::PreUpgrade,
        controller::SnapshotType::FinalStop => SnapshotType::FinalStop,
        controller::SnapshotType::Unspecified => return Err(Status::invalid_argument("snapshot_type is required")),
    })
}

fn map_instance(value: GameInstance) -> controller::GameInstance {
    controller::GameInstance {
        instance_id: value.instance_id.0,
        game: map_game_kind_to_proto(&value.game),
        desired_state: map_desired_state_to_proto(&value.desired_state),
        runtime_state: map_runtime_state_to_proto(&value.runtime_state),
        version_selector: Some(map_version_selector_to_proto(value.version_selector)),
        resolved_build: value.resolved_build.map(map_build),
        spec: Some(map_instance_spec_to_proto(value.spec)),
        assignment: value.assignment.map(map_assignment),
        endpoint: value.endpoint.map(map_endpoint),
        latest_snapshot: value.latest_snapshot.map(map_snapshot_reference),
        pending_restore_snapshot: value.pending_restore_snapshot.map(map_snapshot_reference),
        failure: value.failure.map(map_failure),
        created_at: value.created_at.to_rfc3339(),
        updated_at: value.updated_at.to_rfc3339(),
        generation: value.generation,
        custom_game: match value.game { GameKind::Custom(name) => Some(name), _ => None },
    }
}

fn map_build(value: GameBuild) -> controller::GameBuild {
    controller::GameBuild {
        build_id: value.build_id,
        game: map_game_kind_to_proto(&value.game),
        channel: value.channel,
        adapter_version: value.adapter_version,
        custom_game: match value.game { GameKind::Custom(name) => Some(name), _ => None },
    }
}

fn map_instance_spec_to_proto(value: InstanceSpec) -> controller::InstanceSpec {
    controller::InstanceSpec {
        cluster_name: value.cluster_name,
        max_players: value.max_players as u32,
        resources: Some(controller::ResourceRequirements {
            cpu_millis: value.resources.cpu_millis,
            memory_mb: value.resources.memory_mb,
            disk_mb: value.resources.disk_mb,
        }),
        world_preset: value.world_preset,
        mod_manifest_id: value.mod_manifest_id,
    }
}

fn map_assignment(value: NodeAssignment) -> controller::NodeAssignment {
    controller::NodeAssignment {
        node_id: value.node_id.0,
        reason: value.reason,
    }
}

fn map_endpoint(value: Endpoint) -> controller::Endpoint {
    controller::Endpoint {
        host: value.host,
        game_port: value.game_port as u32,
        query_port: value.query_port.map(|v| v as u32),
    }
}

fn map_endpoint_from_proto(value: controller::Endpoint) -> Endpoint {
    Endpoint {
        host: value.host,
        game_port: value.game_port as u16,
        query_port: value.query_port.map(|v| v as u16),
    }
}

fn map_snapshot_reference(value: SnapshotReference) -> controller::SnapshotReference {
    controller::SnapshotReference {
        snapshot_id: value.snapshot_id.0,
        storage_uri: value.storage_uri,
        manifest_uri: value.manifest_uri,
        checksum: value.checksum,
    }
}

fn map_failure(value: crate::domain::FailureInfo) -> controller::FailureInfo {
    controller::FailureInfo {
        step: value.step,
        reason: value.reason,
        retryable: value.retryable,
    }
}

fn map_reconcile_action(value: crate::application::commands::ReconcileAction) -> ProtoReconcileAction {
    use crate::application::commands::ReconcileAction::*;
    match value {
        NoOp => ProtoReconcileAction { action: Some(controller::reconcile_action::Action::NoOp(controller::NoOpAction {})) },
        ResolvedBuild { build } => ProtoReconcileAction { action: Some(controller::reconcile_action::Action::ResolvedBuild(controller::ResolvedBuildAction { build: Some(map_build(build)) })) },
        AssignedNode { assignment } => ProtoReconcileAction { action: Some(controller::reconcile_action::Action::AssignedNode(controller::AssignedNodeAction { assignment: Some(map_assignment(assignment)) })) },
        BuildPreparationRequested { node_id, build } => ProtoReconcileAction { action: Some(controller::reconcile_action::Action::BuildPreparationRequested(controller::BuildPreparationRequestedAction { node_id: node_id.0, build: Some(map_build(build)) })) },
        StartRequested { node_id, endpoint } => ProtoReconcileAction { action: Some(controller::reconcile_action::Action::StartRequested(controller::StartRequestedAction { node_id: node_id.0, endpoint: endpoint.map(map_endpoint) })) },
        MarkedRunning { endpoint } => ProtoReconcileAction { action: Some(controller::reconcile_action::Action::MarkedRunning(controller::MarkedRunningAction { endpoint: endpoint.map(map_endpoint) })) },
        StopRequested => ProtoReconcileAction { action: Some(controller::reconcile_action::Action::StopRequested(controller::StopRequestedAction {})) },
        MarkedStopped => ProtoReconcileAction { action: Some(controller::reconcile_action::Action::MarkedStopped(controller::MarkedStoppedAction {})) },
    }
}

fn map_game_kind_to_proto(value: &GameKind) -> i32 {
    match value {
        GameKind::Dst => controller::GameKind::Dst as i32,
        GameKind::Minecraft => controller::GameKind::Minecraft as i32,
        GameKind::Custom(_) => controller::GameKind::Custom as i32,
    }
}

fn map_desired_state_to_proto(value: &crate::domain::DesiredState) -> i32 {
    match value {
        crate::domain::DesiredState::Running => controller::DesiredState::Running as i32,
        crate::domain::DesiredState::Stopped => controller::DesiredState::Stopped as i32,
    }
}

fn map_runtime_state_to_proto(value: &RuntimeState) -> i32 {
    match value {
        RuntimeState::Pending => controller::RuntimeState::Pending as i32,
        RuntimeState::RelocationPending => controller::RuntimeState::RelocationPending as i32,
        RuntimeState::ResolvingBuild => controller::RuntimeState::ResolvingBuild as i32,
        RuntimeState::Scheduling => controller::RuntimeState::Scheduling as i32,
        RuntimeState::PreparingBuild => controller::RuntimeState::PreparingBuild as i32,
        RuntimeState::RestoringSnapshot => controller::RuntimeState::RestoringSnapshot as i32,
        RuntimeState::Starting => controller::RuntimeState::Starting as i32,
        RuntimeState::Running => controller::RuntimeState::Running as i32,
        RuntimeState::StopRequested => controller::RuntimeState::StopRequested as i32,
        RuntimeState::Stopping => controller::RuntimeState::Stopping as i32,
        RuntimeState::Stopped => controller::RuntimeState::Stopped as i32,
        RuntimeState::Failed => controller::RuntimeState::Failed as i32,
    }
}

fn map_version_selector_to_proto(value: VersionSelector) -> controller::VersionSelector {
    controller::VersionSelector {
        selector: Some(match value {
            VersionSelector::Channel { channel } => controller::version_selector::Selector::Channel(channel),
            VersionSelector::BuildId { build_id } => controller::version_selector::Selector::BuildId(build_id),
        }),
    }
}


#[cfg(test)]
mod tests {
    use std::sync::Arc;

    use tonic::Request;

    use crate::{
        implementations::{
            FakeBuildResolver, FakeNodeAgentClient, FakeScheduler, FakeSnapshotService,
            InMemoryInstanceRepository,
        },
        ports::{SystemClock},
        proto::controller::{
            self, controller_service_server::ControllerService as _, CreateInstanceRequest,
            CreateSnapshotRequest, GetInstanceRequest, ListInstancesRequest,
            ReconcileInstanceRequest, ReportRuntimeStatusRequest, RequestStopInstanceRequest,
            RestoreSnapshotRequest,
        },
        service::ControllerService,
    };

    type TestServer = super::GrpcControllerServer<
        InMemoryInstanceRepository,
        FakeBuildResolver,
        FakeScheduler,
        FakeNodeAgentClient,
        FakeSnapshotService,
        SystemClock,
    >;

    fn make_server() -> TestServer {
        let service = Arc::new(ControllerService::new(
            Arc::new(InMemoryInstanceRepository::default()),
            Arc::new(FakeBuildResolver),
            Arc::new(FakeScheduler::default()),
            Arc::new(FakeNodeAgentClient::default()),
            Arc::new(FakeSnapshotService::default()),
            Arc::new(SystemClock),
        ));
        super::GrpcControllerServer::new(service)
    }

    fn create_instance_request() -> CreateInstanceRequest {
        CreateInstanceRequest {
            game: controller::GameKind::Dst as i32,
            version_selector: Some(controller::VersionSelector {
                selector: Some(controller::version_selector::Selector::Channel(
                    "public".to_string(),
                )),
            }),
            spec: Some(controller::InstanceSpec {
                cluster_name: "cluster-alpha".to_string(),
                max_players: 6,
                resources: Some(controller::ResourceRequirements {
                    cpu_millis: 500,
                    memory_mb: 1024,
                    disk_mb: 2048,
                }),
                world_preset: Some("SURVIVAL_TOGETHER".to_string()),
                mod_manifest_id: None,
            }),
        }
    }

    async fn create_instance(server: &TestServer) -> controller::GameInstance {
        server
            .create_instance(Request::new(create_instance_request()))
            .await
            .unwrap()
            .into_inner()
            .instance
            .unwrap()
    }

    #[tokio::test]
    async fn create_instance_and_get_instance_round_trip() {
        let server = make_server();
        let created = create_instance(&server).await;

        assert_eq!(created.runtime_state, controller::RuntimeState::Pending as i32);
        assert_eq!(created.desired_state, controller::DesiredState::Running as i32);
        assert_eq!(created.spec.as_ref().unwrap().cluster_name, "cluster-alpha");

        let fetched = server
            .get_instance(Request::new(GetInstanceRequest {
                instance_id: created.instance_id.clone(),
            }))
            .await
            .unwrap()
            .into_inner()
            .instance
            .unwrap();

        assert_eq!(fetched.instance_id, created.instance_id);
        assert_eq!(fetched.runtime_state, controller::RuntimeState::Pending as i32);
    }

    #[tokio::test]
    async fn list_instances_filters_pending_instances() {
        let server = make_server();
        let created = create_instance(&server).await;

        let listed = server
            .list_instances(Request::new(ListInstancesRequest {
                runtime_states: vec![controller::RuntimeState::Pending as i32],
                desired_state: None,
                page_size: 10,
                page_token: String::new(),
            }))
            .await
            .unwrap()
            .into_inner();

        assert_eq!(listed.instances.len(), 1);
        assert_eq!(listed.instances[0].instance_id, created.instance_id);
    }

    #[tokio::test]
    async fn create_snapshot_returns_snapshot_metadata_after_assignment() {
        let server = make_server();
        let created = create_instance(&server).await;

        let _ = server
            .reconcile_instance(Request::new(ReconcileInstanceRequest {
                instance_id: created.instance_id.clone(),
            }))
            .await
            .unwrap();
        let _ = server
            .reconcile_instance(Request::new(ReconcileInstanceRequest {
                instance_id: created.instance_id.clone(),
            }))
            .await
            .unwrap();

        let response = server
            .create_snapshot(Request::new(CreateSnapshotRequest {
                instance_id: created.instance_id.clone(),
                snapshot_type: controller::SnapshotType::Manual as i32,
            }))
            .await
            .unwrap()
            .into_inner();

        let snapshot = response.snapshot.unwrap();
        assert!(snapshot.snapshot_id.starts_with("snap-"));
        assert!(snapshot.storage_uri.as_ref().unwrap().contains("memory://"));
        assert!(snapshot.manifest_uri.as_ref().unwrap().contains("manifest"));
        assert!(snapshot.checksum.as_ref().unwrap().starts_with("sha256:"));
        assert_eq!(
            response.instance.unwrap().latest_snapshot.unwrap().snapshot_id,
            snapshot.snapshot_id
        );
    }

    #[tokio::test]
    async fn restore_snapshot_flows_through_reconcile_before_running() {
        let server = make_server();
        let created = create_instance(&server).await;
        let id = created.instance_id.clone();

        for _ in 0..4 {
            let _ = server
                .reconcile_instance(Request::new(ReconcileInstanceRequest {
                    instance_id: id.clone(),
                }))
                .await
                .unwrap();
        }

        let _ = server
            .request_stop_instance(Request::new(RequestStopInstanceRequest {
                instance_id: id.clone(),
                reason: Some("test-stop".to_string()),
            }))
            .await
            .unwrap();
        let _ = server
            .reconcile_instance(Request::new(ReconcileInstanceRequest {
                instance_id: id.clone(),
            }))
            .await
            .unwrap();
        let _ = server
            .reconcile_instance(Request::new(ReconcileInstanceRequest {
                instance_id: id.clone(),
            }))
            .await
            .unwrap();

        let snapshot_response = server
            .create_snapshot(Request::new(CreateSnapshotRequest {
                instance_id: id.clone(),
                snapshot_type: controller::SnapshotType::FinalStop as i32,
            }))
            .await
            .unwrap()
            .into_inner();
        let snapshot_id = snapshot_response.snapshot.unwrap().snapshot_id;

        let restore_response = server
            .restore_snapshot(Request::new(RestoreSnapshotRequest {
                instance_id: id.clone(),
                snapshot_id: snapshot_id.clone(),
            }))
            .await
            .unwrap()
            .into_inner();
        let restored_instance = restore_response.instance.unwrap();
        assert_eq!(
            restored_instance.runtime_state,
            controller::RuntimeState::RelocationPending as i32
        );
        assert_eq!(
            restored_instance
                .pending_restore_snapshot
                .as_ref()
                .unwrap()
                .snapshot_id,
            snapshot_id
        );

        let step1 = server
            .reconcile_instance(Request::new(ReconcileInstanceRequest {
                instance_id: id.clone(),
            }))
            .await
            .unwrap()
            .into_inner()
            .instance
            .unwrap();
        assert_eq!(step1.runtime_state, controller::RuntimeState::Scheduling as i32);

        let step2 = server
            .reconcile_instance(Request::new(ReconcileInstanceRequest {
                instance_id: id.clone(),
            }))
            .await
            .unwrap()
            .into_inner()
            .instance
            .unwrap();
        assert_eq!(step2.runtime_state, controller::RuntimeState::PreparingBuild as i32);

        let step3 = server
            .reconcile_instance(Request::new(ReconcileInstanceRequest {
                instance_id: id.clone(),
            }))
            .await
            .unwrap()
            .into_inner()
            .instance
            .unwrap();
        assert_eq!(
            step3.runtime_state,
            controller::RuntimeState::RestoringSnapshot as i32
        );

        let step4 = server
            .reconcile_instance(Request::new(ReconcileInstanceRequest {
                instance_id: id.clone(),
            }))
            .await
            .unwrap()
            .into_inner()
            .instance
            .unwrap();
        assert_eq!(step4.runtime_state, controller::RuntimeState::Starting as i32);

        let step5 = server
            .reconcile_instance(Request::new(ReconcileInstanceRequest {
                instance_id: id,
            }))
            .await
            .unwrap()
            .into_inner()
            .instance
            .unwrap();
        assert_eq!(step5.runtime_state, controller::RuntimeState::Running as i32);
        assert!(step5.pending_restore_snapshot.is_none());
        assert_eq!(
            step5.latest_snapshot.as_ref().unwrap().snapshot_id,
            snapshot_id
        );
    }

    #[tokio::test]
    async fn report_runtime_status_updates_endpoint_and_failure_message() {
        let server = make_server();
        let created = create_instance(&server).await;

        let reported = server
            .report_runtime_status(Request::new(ReportRuntimeStatusRequest {
                instance_id: created.instance_id,
                runtime_state: controller::RuntimeState::Failed as i32,
                endpoint: Some(controller::Endpoint {
                    host: "node-dev-1".to_string(),
                    game_port: 2456,
                    query_port: Some(2457),
                }),
                message: Some("health check failed".to_string()),
            }))
            .await
            .unwrap()
            .into_inner()
            .instance
            .unwrap();

        assert_eq!(reported.runtime_state, controller::RuntimeState::Failed as i32);
        assert_eq!(reported.endpoint.unwrap().game_port, 2456);
        assert_eq!(reported.failure.unwrap().reason, "health check failed");
    }
}
