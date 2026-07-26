use std::{
    collections::{HashMap, HashSet},
    sync::{Arc, Mutex},
};

use async_trait::async_trait;
use chrono::Utc;

use crate::proto::node_agent::SnapshotArtifact;
use crate::{
    domain::{
        BuildCompatibility, BuildPreparation, BuildPreparationResult, ConatinerType,
        ContainerFilePathMappingHost, ContainerPortMapping, ContainerResourceLimitation,
        ContainerStatus, Endpoint, Game, GameBuild, GameCache, GameCacheStatus, GameContainer,
        GameInstance, GameInstanceStatus, Image, InstanceId, InstanceRuntimeRecord, LocalGameBuild,
        ModManifest, NodeAgentInfo, NodeId, NodeOperation, OperationId, RemoteImage, RuntimeState,
        SnapshotCaptureRequest, SnapshotRecord, SnapshotRestorePlan, SnapshotRestoreRequest,
        SnapshotRestoreResult, StartInstanceArgument, instance_data_path,
    },
    error::NodeAgentError,
    ports::{
        AssetServiceFace, ContainerClient, ContainerError, DockerInstanceRepository,
        GameCacheRepository, GameInstanceRepository, NodeHeartbeat, OperationRepository,
        Snapshot_manager, SystemInfoProvider,
    },
    service::{SteamService, SteamServiceError},
};

#[derive(Default, Clone)]
pub struct FakeInstanceRuntime {
    runtimes: Arc<Mutex<HashMap<String, InstanceRuntimeRecord>>>,
    game_instances: Arc<Mutex<HashMap<String, GameInstance>>>,
}

#[async_trait]
impl GameInstanceRepository for FakeInstanceRuntime {
    async fn save(&self, game_instance: &GameInstance) -> Result<(), NodeAgentError> {
        let mut instances = self
            .game_instances
            .lock()
            .map_err(|_| NodeAgentError::Internal {
                message: "fake game instance repository lock poisoned".to_string(),
            })?;
        instances.insert(game_instance.id.clone(), game_instance.clone());
        Ok(())
    }

    async fn get(&self, game_instance_id: String) -> Result<GameInstance, NodeAgentError> {
        let instances = self
            .game_instances
            .lock()
            .map_err(|_| NodeAgentError::Internal {
                message: "fake game instance repository lock poisoned".to_string(),
            })?;
        instances
            .get(&game_instance_id)
            .cloned()
            .ok_or_else(|| NodeAgentError::InstanceNotFound {
                instance_id: game_instance_id,
            })
    }

    async fn get_all(&self) -> Result<Vec<GameInstance>, NodeAgentError> {
        let instances = self
            .game_instances
            .lock()
            .map_err(|_| NodeAgentError::Internal {
                message: "fake game instance repository lock poisoned".to_string(),
            })?;
        Ok(instances.values().cloned().collect())
    }
}

#[derive(Default, Clone)]
pub struct FakeSnapshotRuntime;

#[async_trait]
impl Snapshot_manager for FakeSnapshotRuntime {
    async fn create_snapshot(
        &self,
        request: SnapshotCaptureRequest,
    ) -> Result<SnapshotArtifact, NodeAgentError> {
        Ok(SnapshotArtifact {
            snapshot_id: request.snapshot_id.clone(),
            instance_data_path: instance_data_path(&request.instance_id),
            storage_uri: format!("memory://snapshots/{}.tar.zst", request.snapshot_id),
            manifest_uri: Some(format!(
                "memory://snapshots/{}.manifest.json",
                request.snapshot_id
            )),
            checksum: Some(format!("sha256:{}", request.snapshot_id)),
            captured_at: Utc::now().to_string(),
        })
    }

    async fn restore_snapshot(
        &self,
        request: SnapshotRestoreRequest,
    ) -> Result<SnapshotRestoreResult, NodeAgentError> {
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
        let mut operations = self
            .operations
            .lock()
            .map_err(|_| NodeAgentError::Internal {
                message: "operation repository lock poisoned".to_string(),
            })?;
        operations.insert(operation.operation_id.0.clone(), operation.clone());
        Ok(())
    }

    async fn get(
        &self,
        operation_id: &OperationId,
    ) -> Result<Option<NodeOperation>, NodeAgentError> {
        let operations = self
            .operations
            .lock()
            .map_err(|_| NodeAgentError::Internal {
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
        Self {
            node_id: NodeId("node-dev-1".to_string()),
        }
    }
}

impl FakeSystemInfoProvider {
    pub fn new(node_id: impl Into<String>) -> Self {
        Self {
            node_id: NodeId(node_id.into()),
        }
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

    async fn get_host_ip(&self) -> Result<std::net::IpAddr, crate::domain::SystemError> {
        Ok(std::net::IpAddr::V4(std::net::Ipv4Addr::new(127, 0, 0, 1)))
    }

    async fn set_node_id(&self, _node_id: String) {
        // no-op for fake
    }

    async fn get_node_id(&self) -> Option<String> {
        Some(self.node_id.0.clone())
    }
}

#[derive(Default, Clone)]
pub struct FakeAssetServiceFace;

#[async_trait]
impl AssetServiceFace for FakeAssetServiceFace {
    async fn resolve_game_build(
        &self,
        game_id: &str,
        _channel: &str,
    ) -> Result<GameBuild, NodeAgentError> {
        Ok(GameBuild {
            build_id: format!("{game_id}-fake-build"),
            game: Game {
                id: game_id.to_string(),
                name: game_id.to_string(),
                app_id: String::new(),
            },
            channel: None,
            adapter_version: Some("0.1.0".to_string()),
            artifact_uri: Some("test_docker_registry".to_string()),
            artifact_image_name: Some("test_img".to_string()),
            artifact_image_tag: Some("test-tag".to_string()),
        })
    }

    async fn get_game_build(&self, build_id: &str) -> Result<GameBuild, NodeAgentError> {
        Ok(GameBuild {
            build_id: build_id.to_string(),
            game: Game {
                id: "fake-game".to_string(),
                name: "fake-game".to_string(),
                app_id: String::new(),
            },
            channel: None,
            adapter_version: Some("0.1.0".to_string()),
            artifact_uri: Some("test_docker_registry".to_string()),
            artifact_image_name: Some("test_img".to_string()),
            artifact_image_tag: Some("test-tag".to_string()),
        })
    }

    async fn create_snapshot_record(
        &self,
        _instance_id: &str,
        _build_id: Option<String>,
        _snapshot_type: i32,
        _source_node: Option<String>,
    ) -> Result<SnapshotRecord, NodeAgentError> {
        Ok(SnapshotRecord {
            snapshot_id: "fake-snapshot-id".to_string(),
            instance_id: _instance_id.to_string(),
            build_id: _build_id,
            snapshot_type: _snapshot_type,
            instance_data_path: "/data/fake".to_string(),
            storage_uri: None,
            manifest_uri: None,
            checksum: None,
            status: 0,
            source_node: _source_node,
            created_at: Utc::now().to_rfc3339(),
            completed_at: None,
            failure_message: None,
            bucket: "fake-bucket".to_string(),
            key: "fake-key".to_string(),
            host: "127.0.0.1".to_string(),
            host_port: 8888,
        })
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

    async fn get_snapshot(&self, snapshot_id: &str) -> Result<SnapshotRecord, NodeAgentError> {
        Ok(SnapshotRecord {
            snapshot_id: snapshot_id.to_string(),
            instance_id: "fake-instance".to_string(),
            build_id: None,
            snapshot_type: 0,
            instance_data_path: "/data/game-instances/fake".to_string(),
            storage_uri: Some(format!("memory://snapshots/{snapshot_id}.tar.zst")),
            manifest_uri: None,
            checksum: None,
            status: 4,
            source_node: None,
            created_at: Utc::now().to_rfc3339(),
            completed_at: Some(Utc::now().to_rfc3339()),
            failure_message: None,
            bucket: "fake-bucket".to_string(),
            key: "fake-key".to_string(),
            host: "127.0.0.1".to_string(),
            host_port: 8888,
        })
    }

    async fn get_latest_snapshot(
        &self,
        instance_id: &str,
    ) -> Result<Option<SnapshotRecord>, NodeAgentError> {
        Ok(Some(SnapshotRecord {
            snapshot_id: format!("{instance_id}-latest-snapshot"),
            instance_id: instance_id.to_string(),
            build_id: None,
            snapshot_type: 0,
            instance_data_path: format!("/data/game-instances/{instance_id}"),
            storage_uri: None,
            manifest_uri: None,
            checksum: None,
            status: 4,
            source_node: None,
            created_at: Utc::now().to_rfc3339(),
            completed_at: None,
            failure_message: None,
            bucket: "fake-bucket".to_string(),
            key: "fake-key".to_string(),
            host: "127.0.0.1".to_string(),
            host_port: 8888,
        }))
    }

    async fn set_latest_snapshot(
        &self,
        _instance_id: &str,
        _snapshot_id: &str,
    ) -> Result<(), NodeAgentError> {
        Ok(())
    }

    async fn list_snapshots(
        &self,
        instance_id: &str,
    ) -> Result<Vec<SnapshotRecord>, NodeAgentError> {
        Ok(vec![SnapshotRecord {
            snapshot_id: format!("{instance_id}-snapshot-1"),
            instance_id: instance_id.to_string(),
            build_id: None,
            snapshot_type: 0,
            instance_data_path: format!("/data/game-instances/{instance_id}"),
            storage_uri: None,
            manifest_uri: None,
            checksum: None,
            status: 4,
            source_node: None,
            created_at: Utc::now().to_rfc3339(),
            completed_at: None,
            failure_message: None,
            bucket: "fake-bucket".to_string(),
            key: "fake-key".to_string(),
            host: "127.0.0.1".to_string(),
            host_port: 8888,
        }])
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

    async fn get_mod_manifest(&self, manifest_id: &str) -> Result<ModManifest, NodeAgentError> {
        Ok(ModManifest {
            manifest_id: manifest_id.to_string(),
            game_id: "fake-game".to_string(),
            mods: vec![],
            config_hash: "fake-hash".to_string(),
            compatibility_note: None,
            created_at: Utc::now().to_rfc3339(),
        })
    }

    async fn check_build_mod_compatibility(
        &self,
        _build_id: &str,
        _manifest_id: &str,
    ) -> Result<BuildCompatibility, NodeAgentError> {
        Ok(BuildCompatibility {
            compatible: true,
            reason: None,
        })
    }

    async fn get_game(&self, game_id: &str) -> Result<Game, NodeAgentError> {
        Ok(Game {
            id: game_id.to_string(),
            name: game_id.to_string(),
            app_id: String::new(),
        })
    }

    async fn list_games(&self) -> Result<Vec<Game>, NodeAgentError> {
        Ok(vec![Game {
            id: "fake-game".to_string(),
            name: "fake-game".to_string(),
            app_id: String::new(),
        }])
    }

    async fn register_node_agent(
        &self,
        _node_id: &str,
        _endpoint: &str,
    ) -> Result<(), NodeAgentError> {
        Ok(())
    }

    async fn get_node_agent(&self, node_id: &str) -> Result<NodeAgentInfo, NodeAgentError> {
        Ok(NodeAgentInfo {
            node_id: node_id.to_string(),
            endpoint: "http://127.0.0.1:50052".to_string(),
            status: "online".to_string(),
            last_heartbeat_at: Utc::now().timestamp(),
        })
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

    async fn unregister_node_agent(&self, _node_id: &str) -> Result<(), NodeAgentError> {
        Ok(())
    }

    async fn list_node_agents(&self) -> Result<Vec<NodeAgentInfo>, NodeAgentError> {
        Ok(vec![NodeAgentInfo {
            node_id: "fake-node".to_string(),
            endpoint: "http://127.0.0.1:50052".to_string(),
            status: "online".to_string(),
            last_heartbeat_at: Utc::now().timestamp(),
        }])
    }
}

#[derive(Default, Clone)]
pub struct FakeImageClient {
    containers: Arc<Mutex<HashMap<String, GameContainer>>>,
}

#[async_trait]
impl ContainerClient for FakeImageClient {
    async fn pull_image(&self, image: &RemoteImage) -> anyhow::Result<Image> {
        Ok(Image {
            id: image.id.to_string(),
            name: image.name.to_string(),
            tag: image.tag.to_string(),
            size: Some(1024),
            created_at: Utc::now(),
            status: crate::domain::ImageStatus::Runnable,
        })
    }

    async fn check_image(&self, _image: &RemoteImage) -> anyhow::Result<bool> {
        Ok(true)
    }

    async fn last_version(&self, _image: &RemoteImage) -> anyhow::Result<String> {
        Ok("0.1.0".to_string())
    }

    async fn get_container(&self, id: String) -> Result<GameContainer, ContainerError> {
        let containers = self
            .containers
            .lock()
            .map_err(|_| ContainerError::Unknown)?;
        containers.get(&id).cloned().ok_or(ContainerError::Unknown)
    }

    async fn create_container(
        &self,
        _container_name: String,
        game_build: LocalGameBuild,
        path_mapping: Option<ContainerFilePathMappingHost>,
        port_mapping: Option<ContainerPortMapping>,
        resource_limitation: Option<ContainerResourceLimitation>,
    ) -> Result<GameContainer, ContainerError> {
        let container = GameContainer {
            id: format!("fake-container-{}", game_build.build_id),
            game_build,
            container: ConatinerType::DockerContainer,
            container_file_path_mapping: path_mapping,
            container_port_mapping: port_mapping,
            resource_limitation,
            status: ContainerStatus::Created,
        };
        let mut containers = self
            .containers
            .lock()
            .map_err(|_| ContainerError::Unknown)?;
        let id = container.id.clone();
        containers.insert(id, container.clone());
        Ok(container)
    }

    async fn stop_container(&self, id: String) -> Result<GameContainer, ContainerError> {
        let mut containers = self
            .containers
            .lock()
            .map_err(|_| ContainerError::Unknown)?;
        containers.remove(&id).ok_or(ContainerError::Unknown)
    }

    async fn remove_container(&self, id: String) -> Result<GameContainer, ContainerError> {
        let mut containers = self
            .containers
            .lock()
            .map_err(|_| ContainerError::Unknown)?;
        containers.remove(&id).ok_or(ContainerError::Unknown)
    }

    async fn update_container_status(&self) -> Result<i32, ContainerError> {
        let containers = self
            .containers
            .lock()
            .map_err(|_| ContainerError::Unknown)?;
        Ok(containers.len() as i32)
    }
}

// ============================================================
// InMemoryDockerInstanceRepository — 用于测试
// ============================================================

#[derive(Default, Clone)]
pub struct InMemoryDockerInstanceRepository {
    store: Arc<std::sync::Mutex<HashMap<String, GameContainer>>>,
}

#[async_trait]
impl DockerInstanceRepository for InMemoryDockerInstanceRepository {
    async fn save(&self, container: &GameContainer) -> Result<(), NodeAgentError> {
        let mut store = self.store.lock().map_err(|e| NodeAgentError::Internal {
            message: format!("docker instance repo lock poisoned: {e}"),
        })?;
        store.insert(container.id.clone(), container.clone());
        Ok(())
    }

    async fn get(&self, container_id: &str) -> Result<Option<GameContainer>, NodeAgentError> {
        let store = self.store.lock().map_err(|e| NodeAgentError::Internal {
            message: format!("docker instance repo lock poisoned: {e}"),
        })?;
        Ok(store.get(container_id).cloned())
    }

    async fn delete(&self, container_id: &str) -> Result<(), NodeAgentError> {
        let mut store = self.store.lock().map_err(|e| NodeAgentError::Internal {
            message: format!("docker instance repo lock poisoned: {e}"),
        })?;
        store.remove(container_id);
        Ok(())
    }
}

// ============================================================
// InMemoryGameCacheRepository — 用于测试
// ============================================================

#[derive(Default, Clone)]
pub struct InMemoryGameCacheRepository {
    store: Arc<Mutex<HashMap<String, GameCache>>>,
}

#[async_trait]
impl GameCacheRepository for InMemoryGameCacheRepository {
    async fn save(&self, game_cache: &GameCache) -> anyhow::Result<()> {
        let key = format!("{}:{}", game_cache.game_id, game_cache.branch_name);
        let mut store = self
            .store
            .lock()
            .map_err(|e| anyhow::anyhow!("lock: {e}"))?;
        store.insert(key, game_cache.clone());
        Ok(())
    }

    async fn get(
        &self,
        game_id: &String,
        branch_name: &String,
    ) -> anyhow::Result<Option<GameCache>> {
        let key = format!("{}:{}", game_id, branch_name);
        let store = self
            .store
            .lock()
            .map_err(|e| anyhow::anyhow!("lock: {e}"))?;
        Ok(store.get(&key).cloned())
    }
}

// ============================================================
// FakeSteamService — 用于测试
// ============================================================

#[derive(Default, Clone)]
pub struct FakeSteamService;

#[async_trait]
impl SteamService for FakeSteamService {
    async fn start_download(
        &self,
        _game_cache: GameCache,
    ) -> tokio::task::JoinHandle<Result<(), SteamServiceError>> {
        tokio::spawn(async { Ok(()) })
    }

    async fn uninstall(&self, _game_cache: GameCache) -> Result<String, SteamServiceError> {
        Ok("/fake/install/path".to_string())
    }

    async fn get_download_progress(
        &self,
        _game_id: &String,
        _branch_name: &String,
    ) -> anyhow::Result<Option<f32>> {
        Ok(Some(100.0))
    }
}
