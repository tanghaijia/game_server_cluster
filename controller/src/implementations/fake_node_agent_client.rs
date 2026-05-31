use std::{collections::{HashMap, HashSet}, hash::{Hash, Hasher}, sync::{Arc, Mutex}};

use async_trait::async_trait;

use crate::{
    domain::{Endpoint, GameInstance, NodeId},
    error::ControllerError,
    ports::{
        CreateSnapshotRequest, CreateSnapshotResponse, NodeAgentClient, PrepareBuildRequest,
        RestoreSnapshotRequest, RestoreSnapshotResponse, StartInstanceRequest,
        StartInstanceResponse, StopInstanceRequest,
    },
};

#[derive(Default, Clone)]
pub struct FakeNodeAgentClient {
    prepared_builds: Arc<Mutex<HashSet<(String, String)>>>,
    running: Arc<Mutex<HashMap<String, Endpoint>>>,
}

#[async_trait]
impl NodeAgentClient for FakeNodeAgentClient {
    async fn prepare_game_build(&self, request: PrepareBuildRequest) -> Result<(), ControllerError> {
        let mut prepared = self.prepared_builds.lock().map_err(|_| ControllerError::DependencyFailure {
            message: "fake node-agent prepare lock poisoned".to_string(),
        })?;
        prepared.insert((request.node_id.0, request.build.build_id));
        Ok(())
    }

    async fn start_instance(&self, request: StartInstanceRequest) -> Result<StartInstanceResponse, ControllerError> {
        let endpoint = deterministic_endpoint(&request.node_id, &request.instance);
        let mut running = self.running.lock().map_err(|_| ControllerError::DependencyFailure {
            message: "fake node-agent runtime lock poisoned".to_string(),
        })?;
        running.insert(request.instance.instance_id.0.clone(), endpoint.clone());
        Ok(StartInstanceResponse { endpoint: Some(endpoint) })
    }

    async fn stop_instance(&self, request: StopInstanceRequest) -> Result<(), ControllerError> {
        let mut running = self.running.lock().map_err(|_| ControllerError::DependencyFailure {
            message: "fake node-agent runtime lock poisoned".to_string(),
        })?;
        running.remove(&request.instance.instance_id.0);
        Ok(())
    }

    async fn create_snapshot(&self, request: CreateSnapshotRequest) -> Result<CreateSnapshotResponse, ControllerError> {
        Ok(CreateSnapshotResponse {
            storage_uri: format!("memory://{}/{}.tar.zst", request.node_id.0, request.snapshot_id),
            manifest_uri: Some(format!("memory://{}/{}.manifest.json", request.node_id.0, request.snapshot_id)),
            checksum: Some(format!("sha256:{}", request.snapshot_id)),
        })
    }

    async fn restore_snapshot(&self, request: RestoreSnapshotRequest) -> Result<RestoreSnapshotResponse, ControllerError> {
        let _ = request;
        Ok(RestoreSnapshotResponse { restored_path: request.snapshot.instance_data_path })
    }
}

fn deterministic_endpoint(node_id: &NodeId, instance: &GameInstance) -> Endpoint {
    let mut hasher = std::collections::hash_map::DefaultHasher::new();
    node_id.0.hash(&mut hasher);
    instance.instance_id.0.hash(&mut hasher);
    let port = 20000 + (hasher.finish() % 10000) as u16;
    Endpoint {
        host: node_id.0.clone(),
        game_port: port,
        query_port: Some(port + 1),
    }
}
