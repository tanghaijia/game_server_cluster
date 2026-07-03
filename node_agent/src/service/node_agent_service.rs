use std::sync::Arc;

use chrono::Utc;

use crate::domain::{HostSnapShotDataPath, LocalGameBuildManager, RemoteImage};
use crate::ports::ImageClient;
use crate::proto::node_agent::SnapshotArtifact;
use crate::{
    domain::{
        BuildPreparation, BuildPreparationResult, FailureInfo, InstanceId, InstanceRuntimeRecord,
        InstanceRuntimeSpec, NodeOperation, OperationId, OperationKind, OperationStatus,
        RuntimeState, SnapshotCaptureRequest, SnapshotRestoreRequest, SnapshotRestoreResult,
    },
    error::NodeAgentError,
    ports::{
        AssetServiceFace, InstanceRuntime, OperationRepository, SnapshotRuntime, SystemInfoProvider,
    },
};

// ============================================================
// BackgroundWorker — 给后台任务 handler 复用的业务接口
// ============================================================

#[async_trait::async_trait]
pub trait BackgroundWorker: Send + Sync {
    /// 执行 prepare_build，更新已有的 operation（由 gRPC handler 创建 Pending）
    async fn prepare_game_build(
        &self,
        request: BuildPreparation,
        operation_id: &OperationId,
    ) -> Result<BuildPreparationResult, NodeAgentError>;

    /// 执行 start_instance，更新已有的 operation
    async fn start_instance(
        &self,
        spec: InstanceRuntimeSpec,
        operation_id: &OperationId,
    ) -> Result<InstanceRuntimeRecord, NodeAgentError>;
}

pub struct NodeAgentService<I, P, O, S, A, IMC>
where
    I: InstanceRuntime,
    P: SnapshotRuntime,
    O: OperationRepository,
    S: SystemInfoProvider,
    A: AssetServiceFace,
    IMC: ImageClient,
{
    instance_runtime: Arc<I>,
    snapshot_runtime: Arc<P>,
    operations: Arc<O>,
    system_info: Arc<S>,
    asset_service: Arc<A>,
    image_client: Arc<IMC>,
    local_game_build_manager: LocalGameBuildManager,
}

impl<I, P, O, S, A, IMC> NodeAgentService<I, P, O, S, A, IMC>
where
    I: InstanceRuntime,
    P: SnapshotRuntime,
    O: OperationRepository,
    S: SystemInfoProvider,
    A: AssetServiceFace,
    IMC: ImageClient,
{
}

impl<I, P, O, S, A, IMC> NodeAgentService<I, P, O, S, A, IMC>
where
    I: InstanceRuntime,
    P: SnapshotRuntime,
    O: OperationRepository,
    S: SystemInfoProvider,
    A: AssetServiceFace,
    IMC: ImageClient,
{
    pub fn new(
        instance_runtime: Arc<I>,
        snapshot_runtime: Arc<P>,
        operations: Arc<O>,
        system_info: Arc<S>,
        asset_service: Arc<A>,
        image_client: Arc<IMC>,
    ) -> Self {
        Self {
            instance_runtime,
            snapshot_runtime,
            operations,
            system_info,
            asset_service,
            image_client,
            local_game_build_manager: LocalGameBuildManager::new(),
        }
    }

    pub async fn stop_instance(
        &self,
        instance_id: InstanceId,
    ) -> Result<NodeOperation, NodeAgentError> {
        let operation_id = OperationId::new();
        let mut operation = NodeOperation {
            operation_id,
            kind: OperationKind::StopInstance,
            status: OperationStatus::Running,
            instance_id: Some(instance_id.clone()),
            build_id: None,
            message: Some("Stopping instance".to_string()),
            started_at: Utc::now(),
            finished_at: None,
        };
        self.operations.save(&operation).await?;

        let stop_result = self.instance_runtime.stop_instance(&instance_id).await;
        match stop_result {
            Ok(()) => {
                operation.status = OperationStatus::Succeeded;
                operation.finished_at = Some(Utc::now());
                operation.message = Some("Instance stopped".to_string());
                self.operations.save(&operation).await?;
                Ok(operation)
            }
            Err(error) => {
                operation.status = OperationStatus::Failed;
                operation.finished_at = Some(Utc::now());
                operation.message = Some(error.to_string());
                self.operations.save(&operation).await?;
                Err(error)
            }
        }
    }

    pub async fn create_snapshot(
        &self,
        request: SnapshotCaptureRequest,
    ) -> Result<(NodeOperation, SnapshotArtifact), NodeAgentError> {
        let operation_id = OperationId::new();
        let mut operation = NodeOperation {
            operation_id,
            kind: OperationKind::CreateSnapshot,
            status: OperationStatus::Running,
            instance_id: Some(request.instance_id.clone()),
            build_id: None,
            message: Some("Creating instance snapshot".to_string()),
            started_at: Utc::now(),
            finished_at: None,
        };
        self.operations.save(&operation).await?;

        let result = self.snapshot_runtime.create_snapshot(request).await;
        match result {
            Ok(snapshot) => {
                operation.status = OperationStatus::Succeeded;
                operation.finished_at = Some(Utc::now());
                operation.message = Some("Snapshot created".to_string());
                self.operations.save(&operation).await?;
                Ok((operation, snapshot))
            }
            Err(error) => {
                operation.status = OperationStatus::Failed;
                operation.finished_at = Some(Utc::now());
                operation.message = Some(error.to_string());
                self.operations.save(&operation).await?;
                Err(error)
            }
        }
    }

    pub async fn restore_snapshot(
        &self,
        request: SnapshotRestoreRequest,
    ) -> Result<(NodeOperation, SnapshotRestoreResult), NodeAgentError> {
        let operation_id = OperationId::new();
        let mut operation = NodeOperation {
            operation_id,
            kind: OperationKind::RestoreSnapshot,
            status: OperationStatus::Running,
            instance_id: Some(request.instance_id.clone()),
            build_id: None,
            message: Some("Restoring instance snapshot".to_string()),
            started_at: Utc::now(),
            finished_at: None,
        };
        self.operations.save(&operation).await?;

        let data_path = HostSnapShotDataPath::new(request.instance_id.0.clone());

        let result = self.snapshot_runtime.restore_snapshot(request).await;
        match result {
            Ok(snapshot) => {
                operation.status = OperationStatus::Succeeded;
                operation.finished_at = Some(Utc::now());
                operation.message = Some("Snapshot restored".to_string());
                self.operations.save(&operation).await?;
                Ok((operation, snapshot))
            }
            Err(error) => {
                operation.status = OperationStatus::Failed;
                operation.finished_at = Some(Utc::now());
                operation.message = Some(error.to_string());
                self.operations.save(&operation).await?;
                Err(error)
            }
        }
    }

    pub async fn get_operation(
        &self,
        operation_id: &OperationId,
    ) -> Result<NodeOperation, NodeAgentError> {
        self.operations
            .get(operation_id)
            .await?
            .ok_or_else(|| NodeAgentError::InvalidRequest {
                message: format!("operation {} was not found", operation_id.0),
            })
    }

    pub async fn inspect_instance(
        &self,
        instance_id: &InstanceId,
    ) -> Result<InstanceRuntimeRecord, NodeAgentError> {
        self.instance_runtime
            .inspect_instance(instance_id)
            .await?
            .ok_or_else(|| NodeAgentError::InstanceNotFound {
                instance_id: instance_id.0.clone(),
            })
    }

    pub async fn heartbeat(&self) -> Result<crate::ports::NodeHeartbeat, NodeAgentError> {
        self.system_info.heartbeat().await
    }

    pub fn failure(message: impl Into<String>, retryable: bool) -> FailureInfo {
        FailureInfo {
            message: message.into(),
            retryable,
        }
    }
}

// ============================================================
// BackgroundWorker impl（委托给已有的 pub 方法）
// ============================================================

#[async_trait::async_trait]
impl<I, P, O, S, A, IMC> BackgroundWorker for NodeAgentService<I, P, O, S, A, IMC>
where
    I: InstanceRuntime + Send + Sync,
    P: SnapshotRuntime + Send + Sync,
    O: OperationRepository + Send + Sync,
    S: SystemInfoProvider + Send + Sync,
    A: AssetServiceFace + Send + Sync,
    IMC: ImageClient + Send + Sync,
{
    async fn prepare_game_build(
        &self,
        request: BuildPreparation,
        operation_id: &OperationId,
    ) -> Result<BuildPreparationResult, NodeAgentError> {
        // 1. 查找已有的 Pending 操作，设为 Running
        let mut operation = self.operations.get(operation_id).await?.ok_or_else(|| {
            NodeAgentError::InvalidRequest {
                message: format!("operation {} not found", operation_id.0),
            }
        })?;
        operation.status = OperationStatus::Running;
        operation.message = Some("Preparing build...".to_string());
        self.operations
            .save(&operation)
            .await
            .map_err(|e| NodeAgentError::DBOperationFail {
                message: e.to_string(),
            })?;

        // 2. 拉取镜像
        let remote_img = RemoteImage {
            id: "id".to_string(),
            name: "name".to_string(),
            tag: "tag".to_string(),
        };
        let image = match self.image_client.pull_image(&remote_img).await {
            Ok(img) => img,
            Err(e) => {
                operation.status = OperationStatus::Failed;
                operation.finished_at = Some(Utc::now());
                operation.message = Some(format!("pull image fail: {}", e));
                let _ = self.operations.save(&operation).await;
                return Err(NodeAgentError::ImageRepositoryRequestFail {
                    message: e.to_string(),
                });
            }
        };

        // 3. 注册本地构建
        if let Err(e) = self
            .local_game_build_manager
            .record_game_build_from_image(&request.build, &image)
        {
            operation.status = OperationStatus::Failed;
            operation.finished_at = Some(Utc::now());
            operation.message = Some(format!("record build fail: {}", e));
            let _ = self.operations.save(&operation).await;
            return Err(NodeAgentError::ImageRepositoryRequestFail {
                message: e.to_string(),
            });
        }

        // 4. 成功
        operation.status = OperationStatus::Succeeded;
        operation.finished_at = Some(Utc::now());
        operation.message = Some("Build prepared".to_string());
        let _ = self.operations.save(&operation).await;

        Ok(BuildPreparationResult {
            build_root: "asset_service".to_string(),
            prepared_at: Utc::now(),
            build_id: request.build.build_id,
        })
    }

    async fn start_instance(
        &self,
        spec: InstanceRuntimeSpec,
        operation_id: &OperationId,
    ) -> Result<InstanceRuntimeRecord, NodeAgentError> {
        let instance_id = spec.instance_id.clone();
        let node_id = spec.assignment.node_id.clone();

        // 1. 查找已有的 Pending 操作，设为 Running
        let mut operation = self.operations.get(operation_id).await?.ok_or_else(|| {
            NodeAgentError::InvalidRequest {
                message: format!("operation {} not found", operation_id.0),
            }
        })?;
        operation.status = OperationStatus::Running;
        operation.message = Some("Starting instance...".to_string());
        self.operations
            .save(&operation)
            .await
            .map_err(|e| NodeAgentError::DBOperationFail {
                message: e.to_string(),
            })?;

        // 2. 执行
        let start_result = self.instance_runtime.start_instance(spec).await;
        match start_result {
            Ok(result) => {
                let record = InstanceRuntimeRecord {
                    instance_id,
                    node_id,
                    state: RuntimeState::Running,
                    endpoint: result.endpoint,
                    failure: None,
                    updated_at: Utc::now(),
                };
                operation.status = OperationStatus::Succeeded;
                operation.finished_at = Some(Utc::now());
                operation.message = Some("Instance started".to_string());
                self.operations.save(&operation).await?;
                Ok(record)
            }
            Err(error) => {
                operation.status = OperationStatus::Failed;
                operation.finished_at = Some(Utc::now());
                operation.message = Some(error.to_string());
                self.operations.save(&operation).await?;
                Err(error)
            }
        }
    }
}
