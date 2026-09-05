use std::{
    collections::HashMap,
    sync::{Arc, Mutex},
};

use async_trait::async_trait;

use crate::{
    domain::{
        BuildId, Game, GameBuild, ModManifest, ModManifestId, Node, NodeAgent, SnapshotId,
        SnapshotRecord,
    },
    error::AssetServiceError,
    ports::{
        AgentReleaseStore, BuildRepository, GameRepository, ModManifestRepository,
        NodeAgentRepository, NodeRepository, SnapshotRepository, SteamBranch,
        SteamBranchRepository,
    },
};

#[derive(Default)]
pub struct InMemoryBuildRepository {
    builds: Arc<Mutex<HashMap<String, GameBuild>>>,
}

#[async_trait]
impl BuildRepository for InMemoryBuildRepository {
    async fn save(&self, build: &GameBuild) -> Result<(), AssetServiceError> {
        let mut builds = self.builds.lock().map_err(|_| AssetServiceError::Internal {
            message: "build repository lock poisoned".to_string(),
        })?;
        builds.insert(build.build_id.0.clone(), build.clone());
        Ok(())
    }

    async fn get(&self, build_id: &BuildId) -> Result<Option<GameBuild>, AssetServiceError> {
        let builds = self.builds.lock().map_err(|_| AssetServiceError::Internal {
            message: "build repository lock poisoned".to_string(),
        })?;
        Ok(builds.get(&build_id.0).cloned())
    }

    async fn list_by_game(&self, game_id: &str) -> Result<Vec<GameBuild>, AssetServiceError> {
        let builds = self.builds.lock().map_err(|_| AssetServiceError::Internal {
            message: "build repository lock poisoned".to_string(),
        })?;
        Ok(builds
            .values()
            .filter(|build| &build.game_id == game_id)
            .cloned()
            .collect())
    }
}

#[derive(Default)]
pub struct InMemorySnapshotRepository {
    snapshots: Arc<Mutex<HashMap<String, SnapshotRecord>>>,
    latest_by_instance: Arc<Mutex<HashMap<String, String>>>,
}

#[async_trait]
impl SnapshotRepository for InMemorySnapshotRepository {
    async fn save(&self, snapshot: &SnapshotRecord) -> Result<(), AssetServiceError> {
        let mut snapshots = self.snapshots.lock().map_err(|_| AssetServiceError::Internal {
            message: "snapshot repository lock poisoned".to_string(),
        })?;
        snapshots.insert(snapshot.snapshot_id.0.clone(), snapshot.clone());
        Ok(())
    }

    async fn get(
        &self,
        snapshot_id: &SnapshotId,
    ) -> Result<Option<SnapshotRecord>, AssetServiceError> {
        let snapshots = self.snapshots.lock().map_err(|_| AssetServiceError::Internal {
            message: "snapshot repository lock poisoned".to_string(),
        })?;
        Ok(snapshots.get(&snapshot_id.0).cloned())
    }

    async fn list_by_instance(
        &self,
        instance_id: &str,
    ) -> Result<Vec<SnapshotRecord>, AssetServiceError> {
        let snapshots = self.snapshots.lock().map_err(|_| AssetServiceError::Internal {
            message: "snapshot repository lock poisoned".to_string(),
        })?;
        Ok(snapshots
            .values()
            .filter(|snapshot| snapshot.instance_id == instance_id)
            .cloned()
            .collect())
    }

    async fn set_latest(
        &self,
        instance_id: &str,
        snapshot_id: &SnapshotId,
    ) -> Result<(), AssetServiceError> {
        {
            let snapshots = self.snapshots.lock().map_err(|_| AssetServiceError::Internal {
                message: "snapshot repository lock poisoned".to_string(),
            })?;
            if !snapshots.contains_key(&snapshot_id.0) {
                return Err(AssetServiceError::SnapshotNotFound {
                    snapshot_id: snapshot_id.0.clone(),
                });
            }
        }

        let mut latest = self.latest_by_instance.lock().map_err(|_| AssetServiceError::Internal {
            message: "snapshot latest map lock poisoned".to_string(),
        })?;
        latest.insert(instance_id.to_string(), snapshot_id.0.clone());
        Ok(())
    }

    async fn get_latest(
        &self,
        instance_id: &str,
    ) -> Result<Option<SnapshotRecord>, AssetServiceError> {
        let latest_id = {
            let latest = self.latest_by_instance.lock().map_err(|_| AssetServiceError::Internal {
                message: "snapshot latest map lock poisoned".to_string(),
            })?;
            latest.get(instance_id).cloned()
        };

        let Some(snapshot_id) = latest_id else {
            return Ok(None);
        };

        let snapshots = self.snapshots.lock().map_err(|_| AssetServiceError::Internal {
            message: "snapshot repository lock poisoned".to_string(),
        })?;
        Ok(snapshots.get(&snapshot_id).cloned())
    }
}

#[derive(Default)]
pub struct InMemoryModManifestRepository {
    manifests: Arc<Mutex<HashMap<String, ModManifest>>>,
}

#[async_trait]
impl ModManifestRepository for InMemoryModManifestRepository {
    async fn save(&self, manifest: &ModManifest) -> Result<(), AssetServiceError> {
        let mut manifests = self.manifests.lock().map_err(|_| AssetServiceError::Internal {
            message: "mod manifest repository lock poisoned".to_string(),
        })?;
        manifests.insert(manifest.manifest_id.0.clone(), manifest.clone());
        Ok(())
    }

    async fn get(
        &self,
        manifest_id: &ModManifestId,
    ) -> Result<Option<ModManifest>, AssetServiceError> {
        let manifests = self.manifests.lock().map_err(|_| AssetServiceError::Internal {
            message: "mod manifest repository lock poisoned".to_string(),
        })?;
        Ok(manifests.get(&manifest_id.0).cloned())
    }
}

pub struct InMemorySteamBranchRepository {
    /// game_id → Vec<SteamBranch>
    branches: Arc<Mutex<HashMap<String, Vec<SteamBranch>>>>,
}

impl Default for InMemorySteamBranchRepository {
    fn default() -> Self {
        Self {
            branches: Arc::new(Mutex::new(HashMap::new())),
        }
    }
}

#[async_trait]
impl SteamBranchRepository for InMemorySteamBranchRepository {
    async fn save_branches(
        &self,
        game_id: &str,
        branches: &[SteamBranch],
    ) -> Result<(), AssetServiceError> {
        let mut store = self.branches.lock().map_err(|e| AssetServiceError::Internal {
            message: format!("steam branch repository lock poisoned: {e}"),
        })?;
        // 按 build_id 去重，后出现的覆盖先出现的
        let mut seen = std::collections::HashSet::new();
        let deduped: Vec<SteamBranch> = branches
            .iter()
            .rev()
            .filter(|b| seen.insert(b.build_id))
            .collect::<Vec<_>>()
            .into_iter()
            .rev()
            .cloned()
            .collect();
        store.insert(game_id.to_string(), deduped);
        Ok(())
    }

    async fn get_branches(
        &self,
        game_id: &str,
    ) -> Result<Vec<SteamBranch>, AssetServiceError> {
        let store = self.branches.lock().map_err(|e| AssetServiceError::Internal {
            message: format!("steam branch repository lock poisoned: {e}"),
        })?;
        Ok(store.get(game_id).cloned().unwrap_or_default())
    }

    async fn get_branch(
        &self,
        game_id: &str,
        branch_name: &str,
    ) -> Result<Option<SteamBranch>, AssetServiceError> {
        let store = self.branches.lock().map_err(|e| AssetServiceError::Internal {
            message: format!("steam branch repository lock poisoned: {e}"),
        })?;
        Ok(store
            .get(game_id)
            .and_then(|branches: &Vec<SteamBranch>| {
                branches.iter().find(|b| b.name == branch_name).cloned()
            }))
    }
}

#[derive(Default)]
pub struct InMemoryGameRepository {
    games: Arc<Mutex<HashMap<String, Game>>>,
}

#[async_trait]
impl GameRepository for InMemoryGameRepository {
    async fn save(&self, game: &Game) -> Result<(), AssetServiceError> {
        let mut store = self.games.lock().map_err(|e| AssetServiceError::Internal {
            message: format!("game repository lock poisoned: {e}"),
        })?;
        store.insert(game.id.clone(), game.clone());
        Ok(())
    }

    async fn get(&self, game_id: &str) -> Result<Option<Game>, AssetServiceError> {
        let store = self.games.lock().map_err(|e| AssetServiceError::Internal {
            message: format!("game repository lock poisoned: {e}"),
        })?;
        Ok(store.get(game_id).cloned())
    }

    async fn list(&self) -> Result<Vec<Game>, AssetServiceError> {
        let store = self.games.lock().map_err(|e| AssetServiceError::Internal {
            message: format!("game repository lock poisoned: {e}"),
        })?;
        Ok(store.values().cloned().collect())
    }

    async fn delete(&self, game_id: &str) -> Result<(), AssetServiceError> {
        let mut store = self.games.lock().map_err(|e| AssetServiceError::Internal {
            message: format!("game repository lock poisoned: {e}"),
        })?;
        store.remove(game_id);
        Ok(())
    }
}

/// 空的 SteamService 实现，供 demo/开发阶段使用。
pub struct FakeSteamService;

#[async_trait]
impl crate::ports::SteamService for FakeSteamService {
    async fn fetch_game_from_steam(
        &self,
        _app_id: &str,
    ) -> Result<crate::ports::GameData, String> {
        Err("FakeSteamService: not implemented".to_string())
    }

    async fn get_steam_branchs(
        &self,
        _app_id: &str,
    ) -> Result<Vec<crate::ports::SteamBranch>, String> {
        Ok(Vec::new())
    }
}

#[derive(Default)]
pub struct InMemoryNodeRepository {
    nodes: Arc<Mutex<HashMap<String, Node>>>,
}

#[async_trait]
impl NodeRepository for InMemoryNodeRepository {
    async fn save(&self, node: &Node) -> Result<(), AssetServiceError> {
        let mut store = self.nodes.lock().map_err(|e| AssetServiceError::Internal {
            message: format!("node repository lock poisoned: {e}"),
        })?;
        store.insert(node.id.clone(), node.clone());
        Ok(())
    }

    async fn get(&self, node_id: &str) -> Result<Option<Node>, AssetServiceError> {
        let store = self.nodes.lock().map_err(|e| AssetServiceError::Internal {
            message: format!("node repository lock poisoned: {e}"),
        })?;
        Ok(store.get(node_id).cloned())
    }

    async fn list(&self) -> Result<Vec<Node>, AssetServiceError> {
        let store = self.nodes.lock().map_err(|e| AssetServiceError::Internal {
            message: format!("node repository lock poisoned: {e}"),
        })?;
        Ok(store.values().cloned().collect())
    }

    async fn delete(&self, node_id: &str) -> Result<(), AssetServiceError> {
        let mut store = self.nodes.lock().map_err(|e| AssetServiceError::Internal {
            message: format!("node repository lock poisoned: {e}"),
        })?;
        store.remove(node_id);
        Ok(())
    }
}

#[derive(Default)]
pub struct InMemoryNodeAgentRepository {
    agents: Arc<Mutex<HashMap<String, NodeAgent>>>,
}

#[async_trait]
impl NodeAgentRepository for InMemoryNodeAgentRepository {
    async fn save(&self, agent: &NodeAgent) -> Result<(), AssetServiceError> {
        let mut store = self.agents.lock().map_err(|e| AssetServiceError::Internal {
            message: format!("node agent repository lock poisoned: {e}"),
        })?;
        store.insert(agent.node_id.clone(), agent.clone());
        Ok(())
    }

    async fn get(&self, node_id: &str) -> Result<Option<NodeAgent>, AssetServiceError> {
        let store = self.agents.lock().map_err(|e| AssetServiceError::Internal {
            message: format!("node agent repository lock poisoned: {e}"),
        })?;
        Ok(store.get(node_id).cloned())
    }

    async fn list(&self) -> Result<Vec<NodeAgent>, AssetServiceError> {
        let store = self.agents.lock().map_err(|e| AssetServiceError::Internal {
            message: format!("node agent repository lock poisoned: {e}"),
        })?;
        Ok(store.values().cloned().collect())
    }

    async fn delete(&self, node_id: &str) -> Result<(), AssetServiceError> {
        let mut store = self.agents.lock().map_err(|e| AssetServiceError::Internal {
            message: format!("node agent repository lock poisoned: {e}"),
        })?;
        store.remove(node_id);
        Ok(())
    }
}

/// 内存 release 存储（开发/演示/测试用；数据不跨进程，重启即失）。
/// key 以 "{bucket}/{object_key}" 存 HashMap，便于测试断言写入内容。
#[derive(Default)]
pub struct InMemoryAgentReleaseStore {
    objects: Arc<Mutex<HashMap<String, Vec<u8>>>>,
}

impl InMemoryAgentReleaseStore {
    pub fn new() -> Self {
        Self::default()
    }

    /// 读取已写入的对象（测试断言用）
    pub fn get_object(&self, bucket: &str, key: &str) -> Option<Vec<u8>> {
        self.objects
            .lock()
            .ok()
            .and_then(|m| m.get(&format!("{bucket}/{key}")).cloned())
    }
}

#[async_trait]
impl AgentReleaseStore for InMemoryAgentReleaseStore {
    async fn put_object(
        &self,
        bucket: &str,
        key: &str,
        body: Vec<u8>,
    ) -> Result<(), AssetServiceError> {
        self.objects
            .lock()
            .map_err(|e| AssetServiceError::Internal {
                message: format!("in-memory release store lock poisoned: {e}"),
            })?
            .insert(format!("{bucket}/{key}"), body);
        Ok(())
    }
}