use std::format;
use std::path::PathBuf;
use std::sync::Arc;

use chrono::Utc;
use lmrc_docker::DockerClient;

use crate::common::{CONTAINER_DATA_PATH, GAME_CACHE_SERVER_ROOT_PATH};
use crate::domain::{
    ConatinerType, ContainerFilePath, ContainerFilePathMappingHost, GameCache as DomainGameCache,
    GameCacheStatus as DomainGameCacheStatus, GameContainer, GameInstance, HostFilePath,
    HostSnapShotDataPath, LocalGameBuildManager, NodeId, RemoteImage, RuntimeState,
};
use crate::ports::{ContainerClient, GameCacheRepository, GameInstanceRepository};
use crate::service::{DirectoryUploadDownloadService, SteamService, freeze_copy, manifest_key};
use crate::{
    domain::{
        BuildPreparation, BuildPreparationResult, FailureInfo, InstanceId, InstanceRuntimeRecord,
        OperationId, SnapshotCaptureRequest, SnapshotRestoreRequest, SnapshotRestoreResult,
        StartInstanceArgument,
    },
    error::NodeAgentError,
    ports::{AssetServiceFace, SystemInfoProvider},
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
        spec: StartInstanceArgument,
    ) -> Result<InstanceRuntimeRecord, NodeAgentError>;

    async fn restore_snapshot(
        &self,
        request: SnapshotRestoreRequest,
    ) -> Result<SnapshotRestoreResult, NodeAgentError>;

    async fn stop_instance(&self, instance_id: InstanceId) -> Result<(), NodeAgentError>;

    async fn clean_instance(&self, instance_id: InstanceId) -> Result<(), NodeAgentError>;
}

pub struct NodeAgentService<I, S, A, IMC>
where
    I: GameInstanceRepository,
    S: SystemInfoProvider,
    A: AssetServiceFace,
    IMC: ContainerClient,
{
    game_instance_repos: Arc<I>,
    system_info: Arc<S>,
    asset_service: Arc<A>,
    container_client: Arc<IMC>,
    local_game_build_manager: LocalGameBuildManager,
    directory_service: Arc<DirectoryUploadDownloadService>,
    game_cache_repos: Arc<dyn GameCacheRepository>,
    steam_service: Arc<dyn SteamService>,
}

impl<I, S, A, IMC> NodeAgentService<I, S, A, IMC>
where
    I: GameInstanceRepository,
    S: SystemInfoProvider,
    A: AssetServiceFace,
    IMC: ContainerClient,
{
    pub fn new(
        game_instance_repos: Arc<I>,
        system_info: Arc<S>,
        asset_service: Arc<A>,
        container_client: Arc<IMC>,
        directory_service: Arc<DirectoryUploadDownloadService>,
        game_cache_repos: Arc<dyn GameCacheRepository>,
        steam_service: Arc<dyn SteamService>,
    ) -> Self {
        Self {
            game_instance_repos,
            system_info,
            asset_service,
            container_client: container_client,
            local_game_build_manager: LocalGameBuildManager::new(),
            directory_service,
            game_cache_repos,
            steam_service,
        }
    }

    /// 创建内容寻址增量快照。
    ///
    /// 流程：取源（运行中先本地冻结拷贝）→ 创建快照记录 → 读上一份 manifest 增量对照
    /// → 增量上传 + 写 manifest 提交点 → 完成/失败记录。
    pub async fn create_snapshot(
        &self,
        request: SnapshotCaptureRequest,
    ) -> Result<(), NodeAgentError> {
        let instance_id = request.instance_id.0.clone();
        let game_instance = self.game_instance_repos.get(instance_id.clone()).await?;
        let node_id = self
            .system_info
            .get_node_id()
            .await
            .ok_or_else(|| NodeAgentError::Internal {
                message: "node_id not registered".to_string(),
            })?;

        // 取源：运行中 → 本地冻结拷贝（点时间一致）；否则直接用宿主数据目录
        let running = matches!(
            game_instance.status,
            crate::domain::GameInstanceStatus::Running
        );
        let freeze_temp: Option<PathBuf> = if running {
            let tmp = freeze_copy(game_instance.host_data_path.as_ref())
                .await
                .map_err(|err| NodeAgentError::S3UploadFail {
                    message: format!("freeze copy failed: {err}"),
                })?;
            Some(tmp)
        } else {
            None
        };

        let mut created_snapshot_id: Option<String> = None;
        let result: Result<(), NodeAgentError> = async {
            // 创建快照记录（拿 bucket + 权威 snapshot_id；snapshot_type 0 = 常规快照）
            let record = self
                .asset_service
                .create_snapshot_record(
                    &instance_id,
                    Some(game_instance.game_build_id.clone()),
                    0,
                    Some(node_id),
                )
                .await?;
            created_snapshot_id = Some(record.snapshot_id.clone());
            let bucket = record.bucket;

            // 取上一份 manifest 作增量对照（旧格式/下载失败则全量）
            let prev = match self.asset_service.get_latest_snapshot(&instance_id).await {
                Ok(Some(prev_record)) => self
                    .directory_service
                    .download_manifest(&prev_record.bucket, &prev_record.snapshot_id)
                    .await
                    .ok(),
                _ => None,
            };

            let src = if running {
                freeze_temp.as_ref().expect("freeze temp missing").clone()
            } else {
                game_instance.host_data_path.as_ref().to_path_buf()
            };

            let _manifest = self
                .directory_service
                .create_snapshot(
                    &bucket,
                    src,
                    &instance_id,
                    &record.snapshot_id,
                    prev.as_ref(),
                )
                .await
                .map_err(|err| NodeAgentError::S3UploadFail {
                    message: err.to_string(),
                })?;

            // 完成快照记录（新布局：storage_uri/manifest_uri 都指向 manifest，无单归档）
            let manifest_uri = manifest_key(&record.snapshot_id);
            self.asset_service
                .complete_snapshot_record(
                    &record.snapshot_id,
                    &manifest_uri,
                    Some(manifest_uri.clone()),
                    None,
                )
                .await?;
            self.asset_service
                .set_latest_snapshot(&instance_id, &record.snapshot_id)
                .await?;
            Ok(())
        }
        .await;

        // 清理冻结拷贝临时目录（无论成败）
        if let Some(tmp) = freeze_temp {
            let _ = tokio::fs::remove_dir_all(tmp).await;
        }

        if let Err(err) = &result {
            if let Some(snapshot_id) = created_snapshot_id {
                let _ = self
                    .asset_service
                    .fail_snapshot_record(&snapshot_id, &err.to_string())
                    .await;
            }
        }
        result
    }

    pub async fn inspect_instance(
        &self,
        instance_id: &InstanceId,
    ) -> Result<GameInstance, NodeAgentError> {
        let instance = self.game_instance_repos.get(instance_id.0.clone()).await?;
        Ok(instance)
    }

    pub async fn heartbeat(&self) -> Result<crate::ports::NodeHeartbeat, NodeAgentError> {
        self.system_info.heartbeat().await
    }

    pub async fn cache_game(
        &self,
        game_id: &str,
        branch_name: &str,
    ) -> Result<DomainGameCache, NodeAgentError> {
        let now = Utc::now();

        match self
            .game_cache_repos
            .get(&game_id.to_string(), &branch_name.to_string())
            .await
        {
            Ok(Some(c))
                if matches!(
                    c.status,
                    DomainGameCacheStatus::Available | DomainGameCacheStatus::Downloading
                ) =>
            {
                Ok(c)
            }
            _ => {
                let game = self.asset_service.get_game(game_id).await?;

                let mut path = PathBuf::from(GAME_CACHE_SERVER_ROOT_PATH);
                path.push(game.id);
                path.push(branch_name);
                let path_str = path.to_str().ok_or_else(|| {
                    let error = format!("{} {} path error.", game.name, branch_name);
                    log::error!("{}", error);
                    NodeAgentError::Internal { message: error }
                })?;

                let cache = DomainGameCache {
                    game_id: game_id.to_string(),
                    branch_name: branch_name.to_string(),
                    status: DomainGameCacheStatus::Downloading,
                    path: Some(path_str.to_string()),
                    download_progress: None,
                    create_time: now,
                    update_time: now,
                };
                self.game_cache_repos
                    .save(&cache)
                    .await
                    .map_err(|e| NodeAgentError::Internal {
                        message: format!("save game_cache failed: {e}"),
                    })?;
                self.steam_service.start_download(cache.clone()).await;
                Ok(cache)
            }
        }
    }

    pub async fn get_cache_game(
        &self,
        game_id: &str,
        branch_name: &str,
    ) -> Result<DomainGameCache, NodeAgentError> {
        self.game_cache_repos
            .get(&game_id.to_string(), &branch_name.to_string())
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("get game_cache failed: {e}"),
            })?
            .ok_or_else(|| NodeAgentError::Internal {
                message: format!("game cache not found: {}:{}", game_id, branch_name),
            })
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
impl<I, S, A, IMC> BackgroundWorker for NodeAgentService<I, S, A, IMC>
where
    I: GameInstanceRepository + Send + Sync,
    S: SystemInfoProvider + Send + Sync,
    A: AssetServiceFace + Send + Sync,
    IMC: ContainerClient + Send + Sync,
{
    async fn prepare_game_build(
        &self,
        request: BuildPreparation,
        _operation_id: &OperationId,
    ) -> Result<BuildPreparationResult, NodeAgentError> {
        let build_id = request.build_id;
        let build = self.asset_service.get_game_build(build_id.as_str()).await?;

        if build.artifact_uri.is_none()
            || build.artifact_image_name.is_none()
            || build.artifact_image_tag.is_none()
        {
            return Err(NodeAgentError::GameBuildError {
                message: "game build artifact is none".to_string(),
            });
        }

        // 1. 拉取镜像
        let remote_img = RemoteImage {
            id: "id".to_string(),
            name: build.artifact_image_name.clone().unwrap(),
            tag: build.artifact_image_tag.clone().unwrap(),
        };
        let image = self
            .container_client
            .pull_image(&remote_img)
            .await
            .map_err(|e| NodeAgentError::ImageRepositoryRequestFail {
                message: e.to_string(),
            })?;

        // 2. 注册本地构建
        self.local_game_build_manager
            .record_game_build_from_image(&build, &image)
            .map_err(|e| NodeAgentError::ImageRepositoryRequestFail {
                message: e.to_string(),
            })?;

        Ok(BuildPreparationResult {
            build_root: "asset_service".to_string(),
            prepared_at: Utc::now(),
            build_id: build_id,
        })
    }

    async fn start_instance(
        &self,
        argument: StartInstanceArgument,
    ) -> Result<InstanceRuntimeRecord, NodeAgentError> {
        let instance_id = argument.instance_id.clone();

        let mut game_instance = self.game_instance_repos.get(instance_id.0.clone()).await?;
        game_instance.status = crate::domain::GameInstanceStatus::Preparing;
        self.game_instance_repos.save(&game_instance).await?;

        let local_game_build = self
            .local_game_build_manager
            .get(argument.build.build_id)
            .await
            .map_err(|err| NodeAgentError::DBOperationFail {
                message: format!("get local game build fail: {}", err),
            })?;

        let game_cache = self
            .game_cache_repos
            .get(&argument.game.id, &argument.branch_name)
            .await
            .map_err(|err| NodeAgentError::DBOperationFail {
                message: format!("get game cache fail: {}", err),
            })?
            .ok_or_else(|| NodeAgentError::InvalidRequest {
                message: format!(
                    "game cache not found for game_id={}, branch_name={}",
                    argument.game.id, argument.branch_name
                ),
            })?;
        if game_cache.status != DomainGameCacheStatus::Available {
            return Err(NodeAgentError::InvalidRequest {
                message: format!(
                    "game cache is not available for game_id={}, branch_name={}",
                    argument.game.id, argument.branch_name
                ),
            });
        };

        // 游戏服务器目录映射
        let host_path = game_cache
            .path
            .ok_or_else(|| NodeAgentError::InvalidRequest {
                message: format!(
                    "game cache path is empty for game_id={}, branch_name={}",
                    argument.game.id, argument.branch_name
                ),
            })?;

        let mut v2: Vec<ContainerFilePathMappingHost> = Vec::new();
        let path_mapping = ContainerFilePathMappingHost {
            host_path: HostFilePath { path: host_path },
            container_file_path: ContainerFilePath {
                path: argument.container_server_path.clone(),
            },
            mapped_permission: "r".to_string(),
        };
        v2.push(path_mapping);

        // 数据目录/data映射
        let data_host_path =
            game_instance
                .host_data_path
                .to_string()
                .map_err(|e| NodeAgentError::PathError {
                    message: e.to_string(),
                })?;
        let path_mapping = ContainerFilePathMappingHost {
            host_path: HostFilePath {
                path: data_host_path,
            },
            container_file_path: ContainerFilePath {
                path: CONTAINER_DATA_PATH.to_string(),
            },
            mapped_permission: "r".to_string(),
        };
        v2.push(path_mapping);

        let container = self
            .container_client
            .create_container(
                game_instance.id.clone(),
                local_game_build,
                v2,
                argument.port_mapping,
                None,
            )
            .await?;

        game_instance.status = crate::domain::GameInstanceStatus::Running;
        game_instance.container_id = Some(container.id);
        self.game_instance_repos.save(&game_instance).await?;

        Ok(InstanceRuntimeRecord {
            instance_id: argument.instance_id,
            node_id: NodeId("".to_string()),
            state: RuntimeState::Running,
            endpoint: None,
            failure: None,
            updated_at: Utc::now(),
        })
    }

    async fn restore_snapshot(
        &self,
        request: SnapshotRestoreRequest,
    ) -> Result<SnapshotRestoreResult, NodeAgentError> {
        let instance_id = request.instance_id.0.clone();
        // 获取该快照的记录（拿 bucket）
        let snapshot_record = self
            .asset_service
            .get_snapshot(request.snapshot_id.as_str())
            .await?;

        // 读该快照的 manifest（新布局：snapshots/{snapshot_id}/manifest.json）
        let manifest = self
            .directory_service
            .download_manifest(&snapshot_record.bucket, &request.snapshot_id)
            .await
            .map_err(|err| NodeAgentError::S3DownloadFail {
                message: err.to_string(),
            })?;

        // 恢复到 /data/game-instances/{game_instance_id}
        let data_path = HostSnapShotDataPath::new(instance_id.clone());
        let restore_path_string = data_path.as_ref().display().to_string();
        self.directory_service
            .restore_snapshot(
                &snapshot_record.bucket,
                &manifest,
                data_path.as_ref(),
                None,
            )
            .await
            .map_err(|err| NodeAgentError::S3DownloadFail {
                message: err.to_string(),
            })?;

        Ok(SnapshotRestoreResult {
            snapshot_id: request.snapshot_id,
            restored_at: Utc::now(),
            restore_path: restore_path_string,
        })
    }

    async fn stop_instance(&self, instance_id: InstanceId) -> Result<(), NodeAgentError> {
        let instance_id = instance_id.0;
        let mut local_game_instance = self.game_instance_repos.get(instance_id.clone()).await?;
        let docker_id = local_game_instance.container_id.clone();
        if docker_id.is_none() {
            local_game_instance.status = crate::domain::GameInstanceStatus::Failed;
            self.game_instance_repos.save(&local_game_instance).await?;
            return Err(NodeAgentError::ConatinerFail {
                source: crate::ports::ContainerError::NotFound(format!(
                    "instance id {} 无docker_id",
                    instance_id
                )),
            });
        } else {
            let docker_id = docker_id.unwrap();
            local_game_instance.status = crate::domain::GameInstanceStatus::Stopping;
            self.game_instance_repos.save(&local_game_instance).await?;
            self.container_client.stop_container(docker_id).await?;
        }

        Ok(())
    }

    async fn clean_instance(&self, instance_id: InstanceId) -> Result<(), NodeAgentError> {
        // 复用完整快照生命周期：create_snapshot 内部会建记录（拿 bucket + 权威 snapshot_id）、
        // 增量上传 + 写 manifest 提交点、complete/set_latest、失败时 fail_snapshot_record。
        // 传入的 snapshot_id 会被 create_snapshot_record 的权威 snapshot_id 取代。
        self.create_snapshot(SnapshotCaptureRequest {
            instance_id: instance_id.clone(),
            snapshot_id: String::new(),
        })
        .await?;

        // 标记实例为 Stopped
        let mut game_instance = self.game_instance_repos.get(instance_id.0.clone()).await?;
        game_instance.status = crate::domain::GameInstanceStatus::Stopped;
        self.game_instance_repos.save(&game_instance).await?;

        Ok(())
    }
}
