use std::sync::Arc;

use crate::{
    domain::{
        BuildCompatibility, BuildId, BuildStatus, GameBuild, ModManifest, ModManifestId,
        SnapshotId, SnapshotRecord, SnapshotRestorePlan, SnapshotStatus, SnapshotType,
        VersionSelector, instance_data_path,
    },
    error::AssetServiceError,
    ports::{BuildRepository, Clock, GameRepository, ModManifestRepository, SnapshotRepository},
};

#[derive(Debug, Clone)]
pub struct RegisterBuildRequest {
    pub build: GameBuild,
}

#[derive(Debug, Clone)]
pub struct CreateSnapshotRequest {
    pub instance_id: String,
    pub build_id: Option<String>,
    pub snapshot_type: SnapshotType,
    pub source_node: Option<String>,
}

#[derive(Debug, Clone)]
pub struct CompleteSnapshotRequest {
    pub snapshot_id: String,
    pub storage_uri: String,
    pub manifest_uri: Option<String>,
    pub checksum: Option<String>,
}

#[derive(Debug, Clone)]
pub struct FailSnapshotRequest {
    pub snapshot_id: String,
    pub failure_message: String,
}

pub struct AssetService<B, S, M, C, G>
where
    B: BuildRepository,
    S: SnapshotRepository,
    M: ModManifestRepository,
    C: Clock,
    G: GameRepository,
{
    builds: Arc<B>,
    snapshots: Arc<S>,
    manifests: Arc<M>,
    clock: Arc<C>,
    game_repository: Arc<G>,
}

impl<B, S, M, C, G> AssetService<B, S, M, C, G>
where
    B: BuildRepository,
    S: SnapshotRepository,
    M: ModManifestRepository,
    C: Clock,
    G: GameRepository,
{
    pub fn new(
        builds: Arc<B>,
        snapshots: Arc<S>,
        manifests: Arc<M>,
        clock: Arc<C>,
        game_repository: Arc<G>,
    ) -> Self {
        Self {
            builds,
            snapshots,
            manifests,
            clock,
            game_repository,
        }
    }

    pub async fn resolve_game_build(
        &self,
        game_id: &str,
        selector: VersionSelector,
    ) -> Result<GameBuild, AssetServiceError> {
        match selector {
            VersionSelector::BuildId { build_id } => self
                .builds
                .get(&BuildId(build_id.clone()))
                .await?
                .ok_or(AssetServiceError::BuildNotFound { build_id }),
            VersionSelector::Channel { channel } => {
                let candidates = self.builds.list_by_game(game_id).await?;
                candidates
                    .into_iter()
                    .find(|build| {
                        build.channel.as_deref() == Some(channel.as_str())
                            && matches!(
                                build.status,
                                BuildStatus::Available | BuildStatus::Deprecated
                            )
                    })
                    .ok_or_else(|| AssetServiceError::BuildNotFound {
                        build_id: format!("{game_id}:{channel}"),
                    })
            }
        }
    }

    pub async fn register_game_build(
        &self,
        request: RegisterBuildRequest,
    ) -> Result<GameBuild, AssetServiceError> {
        self.builds.save(&request.build).await?;
        Ok(request.build)
    }

    pub async fn get_game_build(&self, build_id: &str) -> Result<GameBuild, AssetServiceError> {
        self.builds
            .get(&BuildId(build_id.to_string()))
            .await?
            .ok_or_else(|| AssetServiceError::BuildNotFound {
                build_id: build_id.to_string(),
            })
    }

    pub async fn create_snapshot(
        &self,
        request: CreateSnapshotRequest,
    ) -> Result<SnapshotRecord, AssetServiceError> {
        let now = self.clock.now();
        let instance_data_path = instance_data_path(&request.instance_id);
        let snapshot = SnapshotRecord {
            snapshot_id: SnapshotId::new(),
            instance_id: request.instance_id,
            build_id: request.build_id.map(BuildId),
            snapshot_type: request.snapshot_type,
            instance_data_path,
            storage_uri: None,
            manifest_uri: None,
            checksum: None,
            status: SnapshotStatus::Pending,
            source_node: request.source_node,
            created_at: now,
            completed_at: None,
            failure_message: None,
            bucket: String::new(),
            key: String::new(),
            host: String::new(),
            host_port: 0,
        };
        self.snapshots.save(&snapshot).await?;
        Ok(snapshot)
    }

    pub async fn complete_snapshot(
        &self,
        request: CompleteSnapshotRequest,
    ) -> Result<SnapshotRecord, AssetServiceError> {
        let id = SnapshotId(request.snapshot_id.clone());
        let mut snapshot =
            self.snapshots
                .get(&id)
                .await?
                .ok_or(AssetServiceError::SnapshotNotFound {
                    snapshot_id: request.snapshot_id,
                })?;
        snapshot.storage_uri = Some(request.storage_uri);
        snapshot.manifest_uri = request.manifest_uri;
        snapshot.checksum = request.checksum;
        snapshot.status = SnapshotStatus::Completed;
        snapshot.completed_at = Some(self.clock.now());
        snapshot.failure_message = None;
        self.snapshots.save(&snapshot).await?;
        self.snapshots
            .set_latest(&snapshot.instance_id, &snapshot.snapshot_id)
            .await?;
        Ok(snapshot)
    }

    pub async fn fail_snapshot(
        &self,
        request: FailSnapshotRequest,
    ) -> Result<SnapshotRecord, AssetServiceError> {
        let id = SnapshotId(request.snapshot_id.clone());
        let mut snapshot =
            self.snapshots
                .get(&id)
                .await?
                .ok_or(AssetServiceError::SnapshotNotFound {
                    snapshot_id: request.snapshot_id,
                })?;
        snapshot.status = SnapshotStatus::Failed;
        snapshot.failure_message = Some(request.failure_message);
        snapshot.completed_at = Some(self.clock.now());
        self.snapshots.save(&snapshot).await?;
        Ok(snapshot)
    }

    pub async fn get_snapshot(
        &self,
        snapshot_id: &str,
    ) -> Result<SnapshotRecord, AssetServiceError> {
        self.snapshots
            .get(&SnapshotId(snapshot_id.to_string()))
            .await?
            .ok_or_else(|| AssetServiceError::SnapshotNotFound {
                snapshot_id: snapshot_id.to_string(),
            })
    }

    pub async fn get_snapshot_restore_plan(
        &self,
        snapshot_id: &str,
    ) -> Result<SnapshotRestorePlan, AssetServiceError> {
        let snapshot = self.get_snapshot(snapshot_id).await?;
        if snapshot.status != SnapshotStatus::Completed {
            return Err(AssetServiceError::Conflict {
                message: format!(
                    "snapshot {} is not restorable until it reaches Completed",
                    snapshot_id
                ),
            });
        }

        let storage_uri =
            snapshot
                .storage_uri
                .clone()
                .ok_or_else(|| AssetServiceError::Conflict {
                    message: format!("snapshot {} is missing storage_uri", snapshot_id),
                })?;

        Ok(SnapshotRestorePlan {
            snapshot_id: snapshot.snapshot_id,
            build_id: snapshot.build_id,
            storage_uri,
            manifest_uri: snapshot.manifest_uri,
            checksum: snapshot.checksum,
            instance_data_path: snapshot.instance_data_path,
        })
    }

    pub async fn list_snapshots(
        &self,
        instance_id: &str,
    ) -> Result<Vec<SnapshotRecord>, AssetServiceError> {
        self.snapshots.list_by_instance(instance_id).await
    }

    pub async fn get_latest_snapshot(
        &self,
        instance_id: &str,
    ) -> Result<Option<SnapshotRecord>, AssetServiceError> {
        self.snapshots.get_latest(instance_id).await
    }

    pub async fn set_latest_snapshot(
        &self,
        instance_id: &str,
        snapshot_id: &str,
    ) -> Result<SnapshotRecord, AssetServiceError> {
        let id = SnapshotId(snapshot_id.to_string());
        self.snapshots.set_latest(instance_id, &id).await?;
        self.snapshots
            .get(&id)
            .await?
            .ok_or_else(|| AssetServiceError::SnapshotNotFound {
                snapshot_id: snapshot_id.to_string(),
            })
    }

    pub async fn register_mod_manifest(
        &self,
        manifest: ModManifest,
    ) -> Result<ModManifest, AssetServiceError> {
        self.manifests.save(&manifest).await?;
        Ok(manifest)
    }

    pub async fn get_mod_manifest(
        &self,
        manifest_id: &str,
    ) -> Result<ModManifest, AssetServiceError> {
        self.manifests
            .get(&ModManifestId(manifest_id.to_string()))
            .await?
            .ok_or_else(|| AssetServiceError::ModManifestNotFound {
                manifest_id: manifest_id.to_string(),
            })
    }

    pub async fn check_build_mod_compatibility(
        &self,
        build_id: &str,
        manifest_id: &str,
    ) -> Result<BuildCompatibility, AssetServiceError> {
        let build = self.get_game_build(build_id).await?;
        let manifest = self.get_mod_manifest(manifest_id).await?;
        let compatible = build.game_id == manifest.game_id;

        Ok(BuildCompatibility {
            compatible,
            reason: if compatible {
                None
            } else {
                Some("build game kind does not match mod manifest game kind".to_string())
            },
        })
    }
}
