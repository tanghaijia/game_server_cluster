use std::sync::Arc;

use chrono::Utc;

use crate::{
    domain::{
        BuildPreparation, BuildPreparationResult, FailureInfo, InstanceId, InstanceRuntimeRecord,
        InstanceRuntimeSpec, NodeOperation, OperationId, OperationKind, OperationStatus,
        RuntimeState, SnapshotArtifact, SnapshotCaptureRequest, SnapshotRestoreRequest,
        SnapshotRestoreResult,
    },
    error::NodeAgentError,
    ports::{
        BuildRuntime, InstanceRuntime, OperationRepository, SnapshotRuntime, SystemInfoProvider,
    },
};

pub struct NodeAgentService<B, I, P, O, S>
where
    B: BuildRuntime,
    I: InstanceRuntime,
    P: SnapshotRuntime,
    O: OperationRepository,
    S: SystemInfoProvider,
{
    build_runtime: Arc<B>,
    instance_runtime: Arc<I>,
    snapshot_runtime: Arc<P>,
    operations: Arc<O>,
    system_info: Arc<S>,
}

impl<B, I, P, O, S> NodeAgentService<B, I, P, O, S>
where
    B: BuildRuntime,
    I: InstanceRuntime,
    P: SnapshotRuntime,
    O: OperationRepository,
    S: SystemInfoProvider,
{
    pub fn new(
        build_runtime: Arc<B>,
        instance_runtime: Arc<I>,
        snapshot_runtime: Arc<P>,
        operations: Arc<O>,
        system_info: Arc<S>,
    ) -> Self {
        Self {
            build_runtime,
            instance_runtime,
            snapshot_runtime,
            operations,
            system_info,
        }
    }

    pub async fn prepare_game_build(
        &self,
        request: BuildPreparation,
    ) -> Result<(NodeOperation, BuildPreparationResult), NodeAgentError> {
        let operation_id = OperationId::new();
        let mut operation = NodeOperation {
            operation_id,
            kind: OperationKind::PrepareBuild,
            status: OperationStatus::Running,
            instance_id: None,
            build_id: Some(request.build.build_id.clone()),
            message: Some("Preparing game build".to_string()),
            started_at: Utc::now(),
            finished_at: None,
        };
        self.operations.save(&operation).await?;

        let result = self.build_runtime.prepare_build(request).await;
        match result {
            Ok(prepared) => {
                operation.status = OperationStatus::Succeeded;
                operation.finished_at = Some(Utc::now());
                operation.message = Some("Build prepared".to_string());
                self.operations.save(&operation).await?;
                Ok((operation, prepared))
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

    pub async fn start_instance(
        &self,
        spec: InstanceRuntimeSpec,
    ) -> Result<(NodeOperation, InstanceRuntimeRecord), NodeAgentError> {
        let instance_id = spec.instance_id.clone();
        let node_id = spec.assignment.node_id.clone();
        let operation_id = OperationId::new();
        let mut operation = NodeOperation {
            operation_id,
            kind: OperationKind::StartInstance,
            status: OperationStatus::Running,
            instance_id: Some(instance_id.clone()),
            build_id: Some(spec.build.build_id.clone()),
            message: Some("Starting instance".to_string()),
            started_at: Utc::now(),
            finished_at: None,
        };
        self.operations.save(&operation).await?;

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
                Ok((operation, record))
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
