use std::{collections::{HashMap, HashSet}, sync::{Arc, Mutex}};

use async_trait::async_trait;
use chrono::Utc;

use crate::{
    domain::{
        instance_data_path, BuildPreparation, BuildPreparationResult, Endpoint, InstanceId,
        InstanceRuntimeRecord, InstanceRuntimeSpec, NodeId, NodeOperation, OperationId,
        RuntimeState, SnapshotArtifact, SnapshotCaptureRequest, SnapshotRestoreRequest,
        SnapshotRestoreResult, SnapshotRestorePlan,
    },
    error::NodeAgentError,
    ports::{
        AssetServiceFace, BuildRuntime, InstanceRuntime, NodeHeartbeat, OperationRepository,
        SnapshotRuntime, StartInstanceResult, SystemInfoProvider,
    },
};

#[derive(Default, Clone)]
pub struct FakeBuildRuntime {
    prepared: Arc<Mutex<HashSet<(String, String)>>>,
}

#[async_trait]
impl BuildRuntime for FakeBuildRuntime {
    async fn prepare_build(&self, request: BuildPreparation) -> Result<BuildPreparationResult, NodeAgentError> {
        let mut prepared = self.prepared.lock().map_err(|_| NodeAgentError::Internal {
            message: "fake build runtime lock poisoned".to_string(),
        })?;
        prepared.insert((request.node_id.0, request.build.build_id.clone()));
        Ok(BuildPreparationResult {
            build_root: format!("/srv/game-cache/{}/{}", request.build.game.id, request.build.build_id),
            prepared_at: Utc::now(),
        })
    }
}

#[derive(Default, Clone)]
pub struct FakeInstanceRuntime {
    runtimes: Arc<Mutex<HashMap<String, InstanceRuntimeRecord>>>,
}

#[async_trait]
impl InstanceRuntime for FakeInstanceRuntime {
    async fn start_instance(&self, spec: InstanceRuntimeSpec) -> Result<StartInstanceResult, NodeAgentError> {
        let endpoint = Endpoint {
            host: spec.assignment.node_id.0.clone(),
            game_port: 23000,
            query_port: Some(23001),
        };
        let record = InstanceRuntimeRecord {
            instance_id: spec.instance_id.clone(),
            node_id: spec.assignment.node_id.clone(),
            state: RuntimeState::Running,
            endpoint: Some(endpoint.clone()),
            failure: None,
            updated_at: Utc::now(),
        };
        let mut runtimes = self.runtimes.lock().map_err(|_| NodeAgentError::Internal {
            message: "fake instance runtime lock poisoned".to_string(),
        })?;
        runtimes.insert(spec.instance_id.0, record);
        Ok(StartInstanceResult { endpoint: Some(endpoint) })
    }

    async fn stop_instance(&self, instance_id: &InstanceId) -> Result<(), NodeAgentError> {
        let mut runtimes = self.runtimes.lock().map_err(|_| NodeAgentError::Internal {
            message: "fake instance runtime lock poisoned".to_string(),
        })?;
        if let Some(record) = runtimes.get_mut(&instance_id.0) {
            record.state = RuntimeState::Stopped;
            record.endpoint = None;
            record.updated_at = Utc::now();
        }
        Ok(())
    }

    async fn inspect_instance(&self, instance_id: &InstanceId) -> Result<Option<InstanceRuntimeRecord>, NodeAgentError> {
        let runtimes = self.runtimes.lock().map_err(|_| NodeAgentError::Internal {
            message: "fake instance runtime lock poisoned".to_string(),
        })?;
        Ok(runtimes.get(&instance_id.0).cloned())
    }
}

#[derive(Default, Clone)]
pub struct FakeSnapshotRuntime;

#[async_trait]
impl SnapshotRuntime for FakeSnapshotRuntime {
    async fn create_snapshot(&self, request: SnapshotCaptureRequest) -> Result<SnapshotArtifact, NodeAgentError> {
        Ok(SnapshotArtifact {
            snapshot_id: request.snapshot_id.clone(),
            instance_data_path: instance_data_path(&request.instance_id),
            storage_uri: format!("memory://snapshots/{}.tar.zst", request.snapshot_id),
            manifest_uri: Some(format!("memory://snapshots/{}.manifest.json", request.snapshot_id)),
            checksum: Some(format!("sha256:{}", request.snapshot_id)),
            captured_at: Utc::now(),
        })
    }

    async fn restore_snapshot(&self, request: SnapshotRestoreRequest) -> Result<SnapshotRestoreResult, NodeAgentError> {
        Ok(SnapshotRestoreResult {
            snapshot_id: request.snapshot_id,
            restore_path: instance_data_path(&request.instance_id),
            restored_at: Utc::now(),
        })
    }
}

#[derive(Default, Clone)]
pub struct InMemoryOperationRepository {
    operations: Arc<Mutex<HashMap<String, NodeOperation>>>,
}

#[async_trait]
impl OperationRepository for InMemoryOperationRepository {
    async fn save(&self, operation: &NodeOperation) -> Result<(), NodeAgentError> {
        let mut operations = self.operations.lock().map_err(|_| NodeAgentError::Internal {
            message: "operation repository lock poisoned".to_string(),
        })?;
        operations.insert(operation.operation_id.0.clone(), operation.clone());
        Ok(())
    }

    async fn get(&self, operation_id: &OperationId) -> Result<Option<NodeOperation>, NodeAgentError> {
        let operations = self.operations.lock().map_err(|_| NodeAgentError::Internal {
            message: "operation repository lock poisoned".to_string(),
        })?;
        Ok(operations.get(&operation_id.0).cloned())
    }
}

#[derive(Clone)]
pub struct FakeSystemInfoProvider {
    node_id: NodeId,
}

impl Default for FakeSystemInfoProvider {
    fn default() -> Self {
        Self { node_id: NodeId("node-dev-1".to_string()) }
    }
}

impl FakeSystemInfoProvider {
    pub fn new(node_id: impl Into<String>) -> Self {
        Self { node_id: NodeId(node_id.into()) }
    }
}

#[async_trait]
impl SystemInfoProvider for FakeSystemInfoProvider {
    async fn heartbeat(&self) -> Result<NodeHeartbeat, NodeAgentError> {
        Ok(NodeHeartbeat {
            node_id: self.node_id.clone(),
            cpu_usage_pct: 12.5,
            memory_usage_pct: 33.0,
            disk_usage_pct: 48.0,
            running_instances: 0,
        })
    }
}

#[derive(Default, Clone)]
pub struct FakeAssetServiceFace;

#[async_trait]
impl AssetServiceFace for FakeAssetServiceFace {
    async fn create_snapshot_record(
        &self,
        _instance_id: &str,
        _build_id: Option<String>,
        _snapshot_type: i32,
        _source_node: Option<String>,
    ) -> Result<String, NodeAgentError> {
        Ok("fake-snapshot-id".to_string())
    }

    async fn complete_snapshot_record(
        &self,
        _snapshot_id: &str,
        _storage_uri: &str,
        _manifest_uri: Option<String>,
        _checksum: Option<String>,
    ) -> Result<(), NodeAgentError> {
        Ok(())
    }

    async fn fail_snapshot_record(
        &self,
        _snapshot_id: &str,
        _failure_message: &str,
    ) -> Result<(), NodeAgentError> {
        Ok(())
    }

    async fn get_snapshot_restore_plan(
        &self,
        snapshot_id: &str,
    ) -> Result<SnapshotRestorePlan, NodeAgentError> {
        Ok(SnapshotRestorePlan {
            snapshot_id: snapshot_id.to_string(),
            build_id: None,
            storage_uri: format!("memory://snapshots/{snapshot_id}.tar.zst"),
            manifest_uri: None,
            checksum: None,
            instance_data_path: "/data/game-instances/fake".to_string(),
        })
    }

    async fn register_node_agent(
        &self,
        _node_id: &str,
        _endpoint: &str,
    ) -> Result<(), NodeAgentError> {
        Ok(())
    }

    async fn update_node_agent(
        &self,
        _node_id: &str,
        _endpoint: &str,
        _status: &str,
        _last_heartbeat_at: i64,
    ) -> Result<(), NodeAgentError> {
        Ok(())
    }
}
