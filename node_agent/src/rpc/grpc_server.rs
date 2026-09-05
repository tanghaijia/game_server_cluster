use std::collections::HashMap;
use std::pin::Pin;
use std::sync::Arc;
use std::time::Duration;

use apalis_sqlite::SqlitePool;
use chrono::Utc;
use log::error;
use prost::Message;
use tokio_stream::{Stream, wrappers::ReceiverStream};
use tonic::{Request, Response, Status};

use prost_types::Timestamp;

use crate::{
    domain::{
        BuildPreparation, BuildPreparationResult, ContainerPortMapping, ContainerPortMappingMod,
        Endpoint, FailureInfo, GameBuild, GameInstance, GameInstanceStatus,
        InstanceId, InstanceRuntimeRecord, InstanceSpec, LocalGameBuild,
        MappingPortType, NodeOperation, OperationError, OperationId, OperationKind,
        OperationStatus, PortMap,
        ResourceRequirements, RuntimeState, SnapshotCaptureRequest,
        SnapshotRestoreResult, StartInstanceArgument,
        GameCache as DomainGameCache, GameCacheStatus as DomainGameCacheStatus,
    },
    error::NodeAgentError,
    ports::{
        AssetServiceFace, ContainerClient, GameInstanceRepository, ObjectStore,
        OperationRepository, SystemInfoProvider,
    },
    proto::{
        asset_service::Node,
        node_agent::{
            self, BusinessErrorCode, BuildPreparationResult as ProtoBuildPreparationResult,
            CacheGameRequest, CacheGameResponse, CleanInstanceRequest, CleanInstanceResponse,
            CreateSnapshotRequest, CreateSnapshotResponse, ErrorCategory,
            ErrorDetail as ProtoErrorDetail, FailureInfo as ProtoFailureInfo,
            GameBuild as ProtoGameBuild, GetCacheGameRequest, GetHeartbeatRequest,
            GetHeartbeatResponse, GetInstancesRequest, GetInstancesResponse,
            GetOperationRequest, GetOperationResponse, InspectInstanceRequest,
            InspectInstanceResponse,
            InstanceRuntimeRecord as ProtoInstanceRuntimeRecord,
            InstanceRuntimeSpec as ProtoInstanceRuntimeSpec, InstanceSpec as ProtoInstanceSpec,
            MappingPortProtocol, NodeAgentGameInstance, NodeAgentGameInstanceStatus,
            NodeHeartbeat as ProtoNodeHeartbeat, NodeOperation as ProtoNodeOperation,
            PortMapEntry, PortMapping, PortMappingMod,
            PrepareGameBuildRequest, PrepareGameBuildResponse,
            RemoveCacheRequest, RemoveCacheResponse,
            RestoreSnapshotRequest as ProtoRestoreSnapshotRequest, RestoreSnapshotResponse,
            SnapshotRestoreResult as ProtoSnapshotRestoreResult, StartInstanceRequest,
            StartInstanceResponse, StopInstanceRequest, StopInstanceResponse,
            RestartInstanceRequest, RestartInstanceResponse,
            UpdateNodeAgentRequest, UpdateNodeAgentResponse,
            node_agent_service_server::NodeAgentService as NodeAgentRpc,
        },
    },
    service::{
        NodeAgentService, RuntimeProbeService, enqueue_clean_instance, enqueue_prepare_build,
        enqueue_restart_instance, enqueue_restore_snapshot, enqueue_start_instance,
        enqueue_stop_instance,
    },
    update::{self, run_update_and_restart},
};
pub struct GrpcNodeAgentServer<I, S, A, IMC>
where
    I: GameInstanceRepository,
    S: SystemInfoProvider,
    A: AssetServiceFace,
    IMC: ContainerClient,
{
    service: Arc<NodeAgentService<I, S, A, IMC>>,
    pool: SqlitePool,
    operations: Arc<dyn OperationRepository>,
    game_instance_repository: Arc<dyn GameInstanceRepository>,
    // B-04/P1-1：运行时探针缓存（健康 + 在线人数），心跳读取
    runtime_probe: Option<Arc<RuntimeProbeService>>,
    // P3（agent-release-asset-service-redesign）：更新下载走对象存储（与快照同构）
    object_store: Arc<dyn ObjectStore>,
}

impl<I, S, A, IMC> GrpcNodeAgentServer<I, S, A, IMC>
where
    I: GameInstanceRepository,
    S: SystemInfoProvider,
    A: AssetServiceFace,
    IMC: ContainerClient,
{
    pub fn new(
        service: Arc<NodeAgentService<I, S, A, IMC>>,
        pool: SqlitePool,
        operations: Arc<dyn OperationRepository>,
        game_instance_repository: Arc<dyn GameInstanceRepository>,
        object_store: Arc<dyn ObjectStore>,
    ) -> Self {
        Self {
            service,
            pool,
            operations,
            game_instance_repository,
            runtime_probe: None,
            object_store,
        }
    }

    /// 附加运行时探针（B-04/P1-1）：GetHeartbeat 响应携带实例健康/在线人数。
    pub fn with_runtime_probe(mut self, probe: Arc<RuntimeProbeService>) -> Self {
        self.runtime_probe = Some(probe);
        self
    }
}

#[tonic::async_trait]
impl<I, S, A, IMC> NodeAgentRpc for GrpcNodeAgentServer<I, S, A, IMC>
where
    I: GameInstanceRepository + 'static,
    S: SystemInfoProvider + 'static,
    A: AssetServiceFace + 'static,
    IMC: ContainerClient + 'static,
{
    type InspectInstanceStreamStream =
        Pin<Box<dyn Stream<Item = Result<InspectInstanceResponse, Status>> + Send>>;

    async fn prepare_game_build(
        &self,
        request: Request<PrepareGameBuildRequest>,
    ) -> Result<Response<PrepareGameBuildResponse>, Status> {
        let request = request.into_inner();
        let build = request.build_id;
        let prep = BuildPreparation { build_id: build };
        let operation = enqueue_prepare_build(&self.pool, &self.operations, prep).await;
        Ok(Response::new(PrepareGameBuildResponse {
            operation: Some(map_operation(operation)),
            result: None,
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
        let instance = map_instance_runtime_spec(spec)?;

        let operation = enqueue_start_instance(
            &self.pool,
            &self.operations,
            &self.game_instance_repository,
            instance,
        )
        .await;

        Ok(Response::new(StartInstanceResponse {
            operation: Some(map_operation(operation)),
            runtime: None,
        }))
    }

    async fn stop_instance(
        &self,
        request: Request<StopInstanceRequest>,
    ) -> Result<Response<StopInstanceResponse>, Status> {
        let request = request.into_inner();
        let operation =
            enqueue_stop_instance(&self.pool, &self.operations, &request.instance_id).await;
        Ok(Response::new(StopInstanceResponse {
            operation: Some(map_operation(operation)),
        }))
    }

    async fn restart_instance(
        &self,
        request: Request<RestartInstanceRequest>,
    ) -> Result<Response<RestartInstanceResponse>, Status> {
        let request = request.into_inner();
        let operation =
            enqueue_restart_instance(&self.pool, &self.operations, &request.instance_id).await;
        Ok(Response::new(RestartInstanceResponse {
            operation: Some(map_operation(operation)),
        }))
    }

    async fn create_snapshot(
        &self,
        request: Request<CreateSnapshotRequest>,
    ) -> Result<Response<CreateSnapshotResponse>, Status> {
        let request = request.into_inner();
        let snapshot = self
            .service
            .create_snapshot(SnapshotCaptureRequest {
                instance_id: InstanceId(request.instance_id),
                snapshot_id: request.snapshot_id,
            })
            .await
            .map_err(map_error)?;
        Ok(Response::new(CreateSnapshotResponse {
            operation: None,
            snapshot: Some(snapshot),
        }))
    }

    async fn restore_snapshot(
        &self,
        request: Request<ProtoRestoreSnapshotRequest>,
    ) -> Result<Response<RestoreSnapshotResponse>, Status> {
        let request = request.into_inner();

        // 幂等:同实例已有进行中的 restore 操作则复用,不重复入队
        if let Some(existing) = self
            .operations
            .find_active(OperationKind::RestoreSnapshot, &request.instance_id)
            .await
            .map_err(|e| map_error(NodeAgentError::Internal { message: e.to_string() }))?
        {
            return Ok(Response::new(RestoreSnapshotResponse {
                operation: Some(map_operation(existing)),
                result: None,
            }));
        }

        // 生成operation
        let operation = NodeOperation {
            operation_id: OperationId::new(),
            kind: OperationKind::RestoreSnapshot,
            status: OperationStatus::Pending,
            instance_id: Some(InstanceId(request.instance_id.clone())),
            build_id: None,
            message: None,
            started_at: Utc::now(),
            finished_at: None,
            error: None,
        };

        enqueue_restore_snapshot(
            &self.pool,
            &self.operations,
            request.instance_id.as_str(),
            request.snapshot_id.as_str(),
            operation.clone(),
        )
        .await;

        Ok(Response::new(RestoreSnapshotResponse {
            operation: Some(map_operation(operation)),
            result: None,
        }))
    }

    async fn get_operation(
        &self,
        request: Request<GetOperationRequest>,
    ) -> Result<Response<GetOperationResponse>, Status> {
        let request = request.into_inner();
        let op_id = request.operation_id.clone();
        let operation = self
            .operations
            .get(&OperationId(request.operation_id))
            .await
            .map_err(|e| map_error(NodeAgentError::Internal { message: e.to_string() }))?
            .ok_or_else(|| {
                status_from_operation_error(
                    tonic::Code::NotFound,
                    OperationError {
                        code: BusinessErrorCode::OperationNotFound as i32,
                        category: ErrorCategory::NotFound as i32,
                        message: format!("operation {} was not found", op_id),
                        retryable: false,
                        params: HashMap::new(),
                    },
                )
            })?;
        Ok(Response::new(GetOperationResponse {
            operation: Some(map_operation(operation)),
        }))
    }

    async fn inspect_instance(
        &self,
        request: Request<InspectInstanceRequest>,
    ) -> Result<Response<InspectInstanceResponse>, Status> {
        let request = request.into_inner();
        let instance = self
            .service
            .inspect_instance(&InstanceId(request.instance_id))
            .await
            .map_err(map_error)?;

        Ok(Response::new(InspectInstanceResponse {
            instance: Some(map_game_instance_to_proto(instance)),
        }))
    }

    async fn get_heartbeat(
        &self,
        _request: Request<GetHeartbeatRequest>,
    ) -> Result<Response<GetHeartbeatResponse>, Status> {
        let heartbeat = self.service.heartbeat().await.map_err(map_error)?;
        let mut proto = ProtoNodeHeartbeat {
            node_id: heartbeat.node_id.0,
            cpu_usage_pct: heartbeat.cpu_usage_pct,
            memory_usage_pct: heartbeat.memory_usage_pct,
            disk_usage_pct: heartbeat.disk_usage_pct,
            running_instances: heartbeat.running_instances,
            net_rx_bps: heartbeat.net_rx_bps,
            net_tx_bps: heartbeat.net_tx_bps,
            instance_runtime: Vec::new(),
        };
        // B-04/P1-1：附加运行时探针缓存（读缓存，不阻塞心跳）
        if let Some(probe) = &self.runtime_probe {
            let stats = probe.snapshot().await;
            proto.instance_runtime = stats.into_iter().map(map_runtime_stat).collect();
        }
        Ok(Response::new(GetHeartbeatResponse {
            heartbeat: Some(proto),
            agent_version: update::current_version(),
        }))
    }

    /// P2：node_agent 一键更新（docs/node-agent-upgrade-design.md §3.2.4）。
    /// 校验（版本不同 + 无活跃实例）通过后 spawn 后台下载→校验→替换→exit(42)。
    async fn update_node_agent(
        &self,
        request: Request<UpdateNodeAgentRequest>,
    ) -> Result<Response<UpdateNodeAgentResponse>, Status> {
        let req = request.into_inner();

        // 1) 目标版本 ≠ 当前版本（防重复）
        let cur = update::current_version();
        if req.version.is_empty() {
            return Ok(Response::new(UpdateNodeAgentResponse {
                state: "failed".to_string(),
                message: "version 必填".to_string(),
            }));
        }
        if req.version == cur {
            return Ok(Response::new(UpdateNodeAgentResponse {
                state: "failed".to_string(),
                message: format!("已是最新版本 {cur}，无需更新"),
            }));
        }

        // 2) 无活跃实例（running/启动/停止中/准备中）才允许更新
        let instances = self
            .game_instance_repository
            .get_all()
            .await
            .map_err(map_error)?;
        let active: Vec<&GameInstance> = instances
            .iter()
            .filter(|i| {
                matches!(
                    i.status,
                    GameInstanceStatus::Running
                        | GameInstanceStatus::Stopping
                        | GameInstanceStatus::Preparing
                        | GameInstanceStatus::Pedding
                )
            })
            .collect();
        if !active.is_empty() {
            return Ok(Response::new(UpdateNodeAgentResponse {
                state: "failed".to_string(),
                message: format!(
                    "节点仍有 {} 个活跃实例（如 {}），拒绝更新",
                    active.len(),
                    active[0].id
                ),
            }));
        }

        // 3) 后台执行更新（下载/校验/替换在响应返回后异步完成，成功即 exit(42) 请求重启）
        let version = req.version.clone();
        let sha256 = req.sha256.clone();
        let size_bytes = req.size_bytes;
        let url = req.download_url.clone();
        let bucket = req.bucket.clone();
        let object_key = req.object_key.clone();
        let store = self.object_store.clone();
        tokio::spawn(async move {
            run_update_and_restart(&version, &sha256, size_bytes, &bucket, &object_key, &url, store)
                .await;
        });

        Ok(Response::new(UpdateNodeAgentResponse {
            state: "accepted".to_string(),
            message: format!(
                "更新已受理（{cur} → {}），完成后自动重启",
                req.version
            ),
        }))
    }

    async fn get_instances(
        &self,
        _request: Request<GetInstancesRequest>,
    ) -> Result<Response<GetInstancesResponse>, Status> {
        let instances = self
            .game_instance_repository
            .get_all()
            .await
            .map_err(map_error)?;

        let instances_list: Vec<NodeAgentGameInstance> = instances
            .into_iter()
            .map(map_game_instance_to_proto)
            .collect();

        Ok(Response::new(GetInstancesResponse {
            instances: instances_list,
        }))
    }

    async fn clean_instance(
        &self,
        request: Request<CleanInstanceRequest>,
    ) -> Result<Response<CleanInstanceResponse>, Status> {
        let request = request.into_inner();
        let operation = enqueue_clean_instance(
            &self.pool,
            &self.operations,
            &request.instance_id,
        )
        .await;
        Ok(Response::new(CleanInstanceResponse {
            operation: Some(map_operation(operation)),
        }))
    }

    async fn inspect_instance_stream(
        &self,
        request: Request<InspectInstanceRequest>,
    ) -> Result<Response<Self::InspectInstanceStreamStream>, Status> {
        let request = request.into_inner();
        let instance_id = request.instance_id;

        // Validate instance exists on first fetch
        self.service
            .inspect_instance(&InstanceId(instance_id.clone()))
            .await
            .map_err(map_error)?;

        let (tx, rx) = tokio::sync::mpsc::channel(4);
        let repo = self.game_instance_repository.clone();

        tokio::spawn(async move {
            let mut interval = tokio::time::interval(Duration::from_secs(1));
            // None = first tick (always send), then only send on status change
            let mut previous_status: Option<GameInstanceStatus> = None;

            loop {
                interval.tick().await;

                match repo.get(instance_id.clone()).await {
                    Ok(instance) => {
                        if previous_status.as_ref() != Some(&instance.status) {
                            previous_status = Some(instance.status.clone());
                            let instance_proto = map_game_instance_to_proto(instance);

                            if tx
                                .send(Ok(InspectInstanceResponse {
                                    instance: Some(instance_proto),
                                }))
                                .await
                                .is_err()
                            {
                                // Client disconnected
                                break;
                            }
                        }

                        // Stop polling when terminal state reached
                        if matches!(
                            previous_status.as_ref().unwrap(),
                            GameInstanceStatus::Stopped | GameInstanceStatus::Failed
                        ) {
                            break;
                        }
                    }
                    Err(e) => {
                        let _ = tx.send(Err(map_error(e))).await;
                        break;
                    }
                }
            }
        });

        Ok(Response::new(Box::pin(ReceiverStream::new(rx))))
    }

    async fn cache_game(
        &self,
        request: Request<CacheGameRequest>,
    ) -> Result<Response<CacheGameResponse>, Status> {
        let req = request.into_inner();
        let cache = self
            .service
            .cache_game(&req.game_id, &req.branch_name, &req.build_id)
            .await
            .map_err(map_error)?;

        Ok(Response::new(CacheGameResponse {
            game_cache: Some(map_domain_cache_to_proto(cache)),
        }))
    }

    async fn get_cache_game(
        &self,
        request: Request<GetCacheGameRequest>,
    ) -> Result<Response<CacheGameResponse>, Status> {
        let req = request.into_inner();
        let cache = self
            .service
            .get_cache_game(&req.game_id, &req.branch_name)
            .await
            .map_err(map_error)?;

        Ok(Response::new(CacheGameResponse {
            game_cache: Some(map_domain_cache_to_proto(cache)),
        }))
    }

    async fn remove_cache(
        &self,
        request: Request<RemoveCacheRequest>,
    ) -> Result<Response<RemoveCacheResponse>, Status> {
        let req = request.into_inner();
        let removed_path = self
            .service
            .remove_cache(&req.game_id, &req.branch_name)
            .await
            .map_err(map_error)?;

        Ok(Response::new(RemoveCacheResponse { removed_path }))
    }
}

fn map_error(error: NodeAgentError) -> Status {
    let code = match &error {
        NodeAgentError::InvalidRequest { .. } => tonic::Code::InvalidArgument,
        NodeAgentError::InstanceNotFound { .. } => tonic::Code::NotFound,
        NodeAgentError::GameCacheNotFound { .. } => tonic::Code::NotFound,
        _ => tonic::Code::Internal,
    };
    status_from_operation_error(code, error.to_operation_error())
}

/// 将业务错误详情打包进 gRPC Status 的 details(rich error model)。
/// 客户端可用 google.rpc.Status.details 反序列化出 nodeagent.v1.ErrorDetail。
fn status_from_operation_error(code: tonic::Code, detail: OperationError) -> Status {
    let proto_detail = map_operation_error(detail);
    let rpc_status = crate::proto::google::rpc::Status {
        code: code as i32,
        message: proto_detail.message.clone(),
        details: vec![prost_types::Any {
            type_url: "type.googleapis.com/nodeagent.v1.ErrorDetail".to_string(),
            value: proto_detail.encode_to_vec(),
        }],
    };
    Status::with_details(code, proto_detail.message, rpc_status.encode_to_vec().into())
}

/// 将 domain 层 OperationError 映射为 proto 的 ErrorDetail。
fn map_operation_error(value: OperationError) -> ProtoErrorDetail {
    ProtoErrorDetail {
        code: value.code,
        category: value.category,
        message: value.message,
        retryable: value.retryable,
        params: value.params,
    }
}

fn map_game_build(value: ProtoGameBuild) -> Result<GameBuild, Status> {
    let game = value
        .game
        .ok_or_else(|| Status::invalid_argument("game is required"))?;
    Ok(GameBuild {
        build_id: value.build_id,
        game: map_game(game)?,
        channel: value.channel,
        adapter_version: value.adapter_version,
        artifact_uri: value.artifact_uri,
        artifact_image_name: value.artifact_image_name,
        artifact_image_tag: value.artifact_image_tag,
    })
}

fn map_instance_runtime_spec(
    value: ProtoInstanceRuntimeSpec,
) -> Result<StartInstanceArgument, Status> {
    let build = map_game_build(
        value
            .build
            .ok_or_else(|| Status::invalid_argument("build is required"))?,
    )?;
    Ok(StartInstanceArgument {
        instance_id: InstanceId(value.instance_id),
        build,
        spec: map_instance_spec(
            value
                .spec
                .ok_or_else(|| Status::invalid_argument("spec is required"))?,
        )?,
        container_server_path: value.container_server_path,
        branch_name: value.branch_name.unwrap_or_else(|| "public".to_string()),
        port_mapping: value.port_mapping.map(map_port_mapping),
        env: value.env,
        config: value.config,
        credentials: value.credentials,
        // B-04/P1-3：运行时探针声明（缺省 script；query_host_port=0 视为未解析）
        probe_mode: value.probe_mode.unwrap_or_else(|| "script".to_string()),
        query_host_port: value.query_host_port.filter(|&p| p != 0).map(|p| p as u16),
    })
}

fn map_instance_spec(value: ProtoInstanceSpec) -> Result<InstanceSpec, Status> {
    let resources = value
        .resources
        .ok_or_else(|| Status::invalid_argument("resources are required"))?;
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

fn map_port_mapping(value: PortMapping) -> ContainerPortMapping {
    ContainerPortMapping {
        container_port_mapping_mod: match PortMappingMod::try_from(value.mode)
            .unwrap_or(PortMappingMod::Unspecified)
        {
            PortMappingMod::Host => ContainerPortMappingMod::HOST,
            PortMappingMod::Nat => ContainerPortMappingMod::NAT,
            PortMappingMod::Unspecified => ContainerPortMappingMod::NAT,
        },
        port_maps: value.entries.into_iter().map(map_port_map_entry).collect(),
    }
}

fn map_port_map_entry(value: PortMapEntry) -> PortMap {
    PortMap {
        host_port: value.host_port as u16,
        container_port: value.container_port as u16,
        mapping_port_type: match MappingPortProtocol::try_from(value.protocol)
            .unwrap_or(MappingPortProtocol::Unspecified)
        {
            MappingPortProtocol::Tcp => MappingPortType::TCP,
            MappingPortProtocol::Udp => MappingPortType::UDP,
            MappingPortProtocol::Unspecified => MappingPortType::TCP,
        },
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
            OperationKind::RestartInstance => node_agent::OperationKind::RestartInstance as i32,
            OperationKind::CreateSnapshot => node_agent::OperationKind::CreateSnapshot as i32,
            OperationKind::RestoreSnapshot => node_agent::OperationKind::RestoreSnapshot as i32,
            OperationKind::CleanInstance => node_agent::OperationKind::CleanInstance as i32,
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
        error: value.error.map(map_operation_error),
    }
}

fn map_build_preparation_result(value: BuildPreparationResult) -> ProtoBuildPreparationResult {
    ProtoBuildPreparationResult {
        build_root: value.build_root,
        prepared_at: value.prepared_at.to_rfc3339(),
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

fn map_node_agent_game_instance_status(status: GameInstanceStatus) -> i32 {
    match status {
        GameInstanceStatus::Pedding => NodeAgentGameInstanceStatus::Pedding as i32,
        GameInstanceStatus::Preparing => NodeAgentGameInstanceStatus::Preparing as i32,
        GameInstanceStatus::Running => NodeAgentGameInstanceStatus::Running as i32,
        GameInstanceStatus::Stopping => NodeAgentGameInstanceStatus::Stopping as i32,
        GameInstanceStatus::Stopped => NodeAgentGameInstanceStatus::Stopped as i32,
        GameInstanceStatus::Failed => NodeAgentGameInstanceStatus::Failed as i32,
    }
}

fn map_game_instance_to_proto(instance: GameInstance) -> NodeAgentGameInstance {
    NodeAgentGameInstance {
        instance_id: instance.id,
        status: map_node_agent_game_instance_status(instance.status),
        container_id: instance.container_id,
        game_build_id: instance.game_build_id,
        create_time: Some(Timestamp {
            seconds: instance.create_time.timestamp(),
            nanos: instance.create_time.timestamp_subsec_nanos() as i32,
        }),
        update_time: Some(Timestamp {
            seconds: instance.update_time.timestamp(),
            nanos: instance.update_time.timestamp_subsec_nanos() as i32,
        }),
        fail_reason: instance.fail_reason,
    }
}

/// B-04/P1-1：domain InstanceRuntimeStat → proto（随心跳上报）
fn map_runtime_stat(value: crate::service::InstanceRuntimeStat) -> node_agent::InstanceRuntimeStat {
    node_agent::InstanceRuntimeStat {
        instance_id: value.instance_id,
        player_count: value.player_count,
        max_players: value.max_players,
        healthy: value.healthy,
        probe_mode: value.probe_mode,
        probe_error: value.probe_error,
        sampled_at: value.sampled_at,
    }
}

fn map_domain_cache_status_to_proto(status: DomainGameCacheStatus) -> i32 {
    match status {
        DomainGameCacheStatus::Downloading => node_agent::GameCacheStatus::Downloading as i32,
        DomainGameCacheStatus::Available => node_agent::GameCacheStatus::Available as i32,
        DomainGameCacheStatus::Removed => node_agent::GameCacheStatus::Removed as i32,
        DomainGameCacheStatus::Unavailable => node_agent::GameCacheStatus::Unavailable as i32,
    }
}

fn map_domain_cache_to_proto(cache: DomainGameCache) -> node_agent::GameCache {
    node_agent::GameCache {
        game_id: cache.game_id,
        branch_name: cache.branch_name,
        build_id: cache.build_id,
        status: map_domain_cache_status_to_proto(cache.status),
        path: cache.path,
        download_progress: cache.download_progress,
        size_bytes: cache.size_bytes,
        // P4：失败原因透传（空串 = 无失败/成功）
        last_error: cache.last_error.unwrap_or_default(),
        create_time: Some(Timestamp {
            seconds: cache.create_time.timestamp(),
            nanos: cache.create_time.timestamp_subsec_nanos() as i32,
        }),
        update_time: Some(Timestamp {
            seconds: cache.update_time.timestamp(),
            nanos: cache.update_time.timestamp_subsec_nanos() as i32,
        }),
    }
}