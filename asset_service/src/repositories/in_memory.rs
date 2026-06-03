use std::{
    collections::HashMap,
    sync::{Arc, Mutex},
};

use async_trait::async_trait;

use crate::{
    domain::{BuildId, GameBuild, ModManifest, ModManifestId, SnapshotId, SnapshotRecord},
    error::AssetServiceError,
    ports::{BuildRepository, ModManifestRepository, SnapshotRepository},
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
