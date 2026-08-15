use std::format;
use std::path::PathBuf;
use std::sync::Arc;

use chrono::Utc;
use lmrc_docker::DockerClient;
use tokio::fs;

use crate::common::{CONTAINER_DATA_PATH, GAME_CACHE_SERVER_ROOT_PATH};
use crate::domain::{
    ConatinerType, ContainerFilePath, ContainerFilePathMappingHost, GameCache as DomainGameCache,
    GameCacheStatus as DomainGameCacheStatus, GameContainer, GameInstance, HostFilePath,
    HostSnapShotDataPath, LocalGameBuild, NodeId, RemoteImage, RuntimeState,
};
use crate::ports::{
    ContainerClient, ContainerError, GameCacheRepository, GameInstanceRepository,
    LocalGameBuildRepository,
};
use crate::service::{DirectoryUploadDownloadService, SteamService, freeze_copy, manifest_key};
use crate::{
    domain::{
        BuildPreparation, BuildPreparationResult, FailureInfo, InstanceId, InstanceRuntimeRecord,
        OperationId, SnapshotCaptureRequest, SnapshotRestoreRequest, SnapshotRestoreResult,
        StartInstanceArgument, instance_data_path,
    },
    error::NodeAgentError,
    ports::{AssetServiceFace, SystemInfoProvider},
    proto::{asset_service::SnapshotType, node_agent::SnapshotArtifact},
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

    async fn restart_instance(
        &self,
        instance_id: InstanceId,
    ) -> Result<InstanceRuntimeRecord, NodeAgentError>;

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
    local_game_build_repos: Arc<dyn LocalGameBuildRepository>,
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
        local_game_build_repos: Arc<dyn LocalGameBuildRepository>,
        directory_service: Arc<DirectoryUploadDownloadService>,
        game_cache_repos: Arc<dyn GameCacheRepository>,
        steam_service: Arc<dyn SteamService>,
    ) -> Self {
        Self {
            game_instance_repos,
            system_info,
            asset_service,
            container_client: container_client,
            local_game_build_repos,
            directory_service,
            game_cache_repos,
            steam_service,
        }
    }

    /// 执行内容寻址增量快照上传。
    ///
    /// 快照记录由 controller 预先创建并通过 `snapshot_id` 传入，这里只做：
    /// 取源（运行中冻结拷贝）→ 读上一份已完成快照 manifest 增量对照 → 增量上传 + 写 manifest 提交点。
    /// 完成/失败记录由 controller 负责，因此不调 create/complete/set_latest。
    pub async fn create_snapshot(
        &self,
        request: SnapshotCaptureRequest,
    ) -> Result<SnapshotArtifact, NodeAgentError> {
        let instance_id = request.instance_id.0.clone();
        let game_instance = self.game_instance_repos.get(instance_id.clone()).await?;

        // 从预创建的快照记录拿 bucket
        let snapshot_record = self
            .asset_service
            .get_snapshot(request.snapshot_id.as_str())
            .await?;
        let bucket = snapshot_record.bucket;

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

        let upload_result = async {
            // 上一份已完成快照的 manifest 作增量对照（get_latest 返回 set_latest 的记录）
            let prev = match self.asset_service.get_latest_snapshot(&instance_id).await {
                Ok(Some(prev_record)) if prev_record.snapshot_id != request.snapshot_id => self
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

            self.directory_service
                .create_snapshot(
                    &bucket,
                    src,
                    &instance_id,
                    &request.snapshot_id,
                    prev.as_ref(),
                )
                .await
                .map_err(|err| NodeAgentError::S3UploadFail {
                    message: err.to_string(),
                })
        }
        .await;

        // 清理冻结拷贝临时目录（无论成败）
        if let Some(tmp) = freeze_temp {
            let _ = tokio::fs::remove_dir_all(tmp).await;
        }

        let manifest = upload_result?;

        // 返回 artifact（storage_uri/manifest_uri 指向 manifest，无单归档）
        let manifest_uri = manifest_key(&request.snapshot_id);
        Ok(SnapshotArtifact {
            snapshot_id: request.snapshot_id,
            instance_data_path: instance_data_path(&request.instance_id),
            storage_uri: manifest_uri.clone(),
            manifest_uri: Some(manifest_uri),
            checksum: None,
            captured_at: manifest.captured_at,
        })
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
        build_id: &str,
    ) -> Result<DomainGameCache, NodeAgentError> {
        let now = Utc::now();

        // 1) 查询现有缓存记录
        let existing = self
            .game_cache_repos
            .get(&game_id.to_string(), &branch_name.to_string())
            .await
            .ok()
            .flatten();

        // 1.1) 已存在且 build_id 相同,并且处于可用/下载中 → 幂等返回,无需重新下载
        if let Some(c) = &existing {
            if c.build_id == build_id
                && matches!(
                    c.status,
                    DomainGameCacheStatus::Available | DomainGameCacheStatus::Downloading
                )
            {
                return Ok(c.clone());
            }
        }

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
            build_id: build_id.to_string(),
            status: DomainGameCacheStatus::Downloading,
            path: Some(path_str.to_string()),
            download_progress: None,
            create_time: now,
            update_time: now,
        };

        // 2) 原子 get-or-create:仅"首个"插入者负责启动下载,防并发双下载
        match self.game_cache_repos.insert_if_absent(&cache).await {
            Ok(true) => {
                self.steam_service.start_download(cache.clone()).await;
                Ok(cache)
            }
            Ok(false) => {
                // 已存在:可能是并发刚插入同一 build,或残留旧版本/清理态
                if let Ok(Some(existing)) = self
                    .game_cache_repos
                    .get(&game_id.to_string(), &branch_name.to_string())
                    .await
                {
                    // 并发方已插入相同 build 且可用/下载中 → 幂等返回
                    if existing.build_id == build_id
                        && matches!(
                            existing.status,
                            DomainGameCacheStatus::Available
                                | DomainGameCacheStatus::Downloading
                        )
                    {
                        return Ok(existing);
                    }
                    // build_id 不同(新版本)或残留清理态:覆盖为 Downloading 重新下载
                    // steamcmd 会以 force_install_dir + validate 覆盖旧文件,无需手动清理
                    let _ = self.game_cache_repos.save(&cache).await;
                    self.steam_service.start_download(cache.clone()).await;
                    return Ok(cache);
                }
                Ok(cache)
            }
            Err(e) => Err(NodeAgentError::Internal {
                message: format!("save game_cache failed: {e}"),
            }),
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
            .ok_or_else(|| NodeAgentError::GameCacheNotFound {
                game_id: game_id.to_string(),
                branch_name: branch_name.to_string(),
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

        // 2. 注册本地构建（持久化到表）
        let local_game_build = LocalGameBuild {
            build_id: build.build_id.clone(),
            game: build.game.clone(),
            image: image.clone(),
        };
        self.local_game_build_repos
            .save(&local_game_build)
            .await
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
            .local_game_build_repos
            .get(argument.build.build_id)
            .await
            .map_err(|err| NodeAgentError::DBOperationFail {
                message: format!("get local game build fail: {}", err),
            })?;

        let game_cache = self
            .game_cache_repos
            .get(&argument.build.game.id, &argument.branch_name)
            .await
            .map_err(|err| NodeAgentError::DBOperationFail {
                message: format!("get game cache fail: {}", err),
            })?
            .ok_or_else(|| NodeAgentError::InvalidRequest {
                message: format!(
                    "game cache not found for game_id={}, branch_name={}",
                    argument.build.game.id, argument.branch_name
                ),
            })?;
        if game_cache.status != DomainGameCacheStatus::Available {
            return Err(NodeAgentError::InvalidRequest {
                message: format!(
                    "game cache is not available for game_id={}, branch_name={}",
                    argument.build.game.id, argument.branch_name
                ),
            });
        };

        // 游戏服务器目录映射
        let host_path = game_cache
            .path
            .ok_or_else(|| NodeAgentError::InvalidRequest {
                message: format!(
                    "game cache path is empty for game_id={}, branch_name={}",
                    argument.build.game.id, argument.branch_name
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

        // 预创建实例数据目录：全新实例没有快照时宿主目录不存在，
        // 若交给 Docker 自动创建会以 root 所有，导致容器（以 node_agent 用户运行）无法写入 /data。
        // 容器 user 与 node_agent 同 uid:gid，因此此处以 node_agent 身份创建的目录容器即可写。
        tokio::fs::create_dir_all(&data_host_path)
            .await
            .map_err(|e| NodeAgentError::PathError {
                message: format!("create instance data dir {} failed: {}", data_host_path, e),
            })?;
        let path_mapping = ContainerFilePathMappingHost {
            host_path: HostFilePath {
                path: data_host_path,
            },
            container_file_path: ContainerFilePath {
                path: CONTAINER_DATA_PATH.to_string(),
            },
            mapped_permission: "rwx".to_string(),
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
                argument.env,
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

        // 恢复到 /data/game_instances/{game_instance_id}（HOST_DATA_PATH）
        let data_path = HostSnapShotDataPath::new(instance_id.clone());
        let restore_path_string = data_path.as_ref().display().to_string();
        self.directory_service
            .restore_snapshot(&snapshot_record.bucket, &manifest, data_path.as_ref(), None)
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

    async fn restart_instance(
        &self,
        instance_id: InstanceId,
    ) -> Result<InstanceRuntimeRecord, NodeAgentError> {
        let instance_id = instance_id.0;
        let mut local_game_instance = self.game_instance_repos.get(instance_id.clone()).await?;
        let docker_id = local_game_instance.container_id.clone();
        if docker_id.is_none() {
            return Err(NodeAgentError::ConatinerFail {
                source: crate::ports::ContainerError::NotFound(format!(
                    "instance id {} 无docker_id",
                    instance_id
                )),
            });
        }
        let docker_id = docker_id.unwrap();
        self.container_client
            .restart_container(docker_id)
            .await
            .map_err(|e| NodeAgentError::ConatinerFail { source: e })?;

        local_game_instance.status = crate::domain::GameInstanceStatus::Running;
        self.game_instance_repos.save(&local_game_instance).await?;

        Ok(InstanceRuntimeRecord {
            instance_id: InstanceId(instance_id),
            node_id: NodeId("".to_string()),
            state: RuntimeState::Running,
            endpoint: None,
            failure: None,
            updated_at: Utc::now(),
        })
    }

    async fn clean_instance(&self, instance_id: InstanceId) -> Result<(), NodeAgentError> {
        let instance_id = instance_id.0;
        let mut game_instance = self.game_instance_repos.get(instance_id.clone()).await?;
        let node_id =
            self.system_info
                .get_node_id()
                .await
                .ok_or_else(|| NodeAgentError::Internal {
                    message: "node_id not registered".to_string(),
                })?;

        // clean = 实例最终停止，快照类型用 FINAL_STOP；记录由本流程创建并完成
        let record = self
            .asset_service
            .create_snapshot_record(
                &instance_id,
                Some(game_instance.game_build_id.clone()),
                SnapshotType::FinalStop as i32,
                Some(node_id),
            )
            .await?;
        let bucket = record.bucket;

        // 停机场景直接用宿主数据目录增量上传
        let _manifest = self
            .directory_service
            .create_snapshot(
                &bucket,
                game_instance.host_data_path.as_ref(),
                &instance_id,
                &record.snapshot_id,
                None,
            )
            .await
            .map_err(|err| NodeAgentError::S3UploadFail {
                message: err.to_string(),
            })?;

        // 完成记录并设为最新
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

        // 删除容器
        if let Some(docker_id) = game_instance.container_id.clone() {
            self.container_client.remove_container(docker_id).await?;
        } else {
            let err = format!("容器不存在，game_instance: {}", game_instance.id);
            log::error!("{}", err);
            return Err(NodeAgentError::Internal { message: err });
        }

        // 删除本地data
        let path = game_instance
            .host_data_path
            .to_string()
            .expect("host_data_path.to_string fail");
        match fs::remove_dir_all(game_instance.host_data_path.as_ref()).await {
            Ok(_) => log::info!("{} 目录及内容删除成功", path),
            Err(e) => log::error!("{} 目录删除失败: {}", path, e),
        }

        // 标记实例为 Stopped
        game_instance.status = crate::domain::GameInstanceStatus::Stopped;
        self.game_instance_repos.save(&game_instance).await?;
        Ok(())
    }
}