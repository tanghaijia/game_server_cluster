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
        let mut hb = self.system_info.heartbeat().await?;
        // running_instances 真实值：从实例仓库统计 Running 实例（修正占位 0，§9.5）
        if let Ok(instances) = self.game_instance_repos.get_all().await {
            hb.running_instances = instances
                .iter()
                .filter(|i| i.status == crate::domain::GameInstanceStatus::Running)
                .count() as u32;
        }
        Ok(hb)
    }

    pub async fn cache_game(
        &self,
        game_id: &str,
        branch_name: &str,
        build_id: &str,
    ) -> Result<DomainGameCache, NodeAgentError> {
        let now = Utc::now();
        let target = build_id.to_string();

        // 1) 幂等：目标版本已是 Available/Downloading → 直接返回（并发共享同一次下载）
        if let Ok(Some(v)) = self
            .game_cache_repos
            .get_version(&game_id.to_string(), &branch_name.to_string(), &target)
            .await
        {
            if matches!(
                v.status,
                DomainGameCacheStatus::Available | DomainGameCacheStatus::Downloading
            ) {
                return Ok(v);
            }
        }
        // 1.1) 无版本记录但 current 已是目标且可用/下载中 → 返回 current
        if let Ok(Some(cur)) = self
            .game_cache_repos
            .get(&game_id.to_string(), &branch_name.to_string())
            .await
        {
            if cur.build_id == target
                && matches!(
                    cur.status,
                    DomainGameCacheStatus::Available | DomainGameCacheStatus::Downloading
                )
            {
                return Ok(cur);
            }
        }

        let game = self.asset_service.get_game(game_id).await?;

        // 路径带 buildid（P2-A）：/server/{game}/{branch}/{buildid}
        // 下载落在同级 .staging/{buildid}，成功后原子 rename（steam_service_client.download）
        let mut path = PathBuf::from(GAME_CACHE_SERVER_ROOT_PATH);
        path.push(game.id);
        path.push(branch_name);
        path.push(build_id);
        let path_str = path.to_str().ok_or_else(|| {
            let error = format!("{} {} path error.", game.name, branch_name);
            log::error!("{}", error);
            NodeAgentError::Internal { message: error }
        })?;

        let cache = DomainGameCache {
            game_id: game_id.to_string(),
            branch_name: branch_name.to_string(),
            build_id: target.clone(),
            status: DomainGameCacheStatus::Downloading,
            path: Some(path_str.to_string()),
            download_progress: None,
            refcount: 0,
            size_bytes: 0,
            create_time: now,
            update_time: now,
        };

        // 2) 原子 get-or-create（按版本 key）：仅"首个"插入者负责启动下载,防并发双下载
        match self.game_cache_repos.insert_if_absent(&cache).await {
            Ok(true) => {
                self.steam_service.start_download(cache.clone()).await;
                Ok(cache)
            }
            Ok(false) => {
                // 已存在同版本记录：可能是并发刚插入,或残留清理态(Unavailable/Removed)
                if let Ok(Some(existing)) = self
                    .game_cache_repos
                    .get_version(&game_id.to_string(), &branch_name.to_string(), &target)
                    .await
                {
                    if matches!(
                        existing.status,
                        DomainGameCacheStatus::Available | DomainGameCacheStatus::Downloading
                    ) {
                        return Ok(existing);
                    }
                    // 残留清理态：覆盖为 Downloading 重新下载（staging 全新下载，不碰旧目录）
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

    /// 删除节点上的 (game, branch) 全部缓存版本（RemoveCache RPC）。
    ///
    /// 语义（与设计稿 docs/cache-placement-design.md §7 对齐）：
    /// - 幂等：无缓存记录时直接返回成功（removed_path 为空）；
    /// - 引用检查：该 game 仍有活动实例（Preparing/Running/Stopping）引用该分支 → 拒绝删除；
    /// - 任一版本 refcount>0 或 Downloading → 拒绝（不打断下载、不误删引用目录）；
    /// - 成功：删除全部版本磁盘目录 + 清理记录，释放磁盘。
    pub async fn remove_cache(
        &self,
        game_id: &str,
        branch_name: &str,
    ) -> Result<String, NodeAgentError> {
        let versions = self
            .game_cache_repos
            .get_versions(&game_id.to_string(), &branch_name.to_string())
            .await
            .map_err(|e| NodeAgentError::DBOperationFail {
                message: format!("get game_cache failed: {e}"),
            })?;

        // 幂等：无缓存记录视为已删除
        if versions.is_empty() {
            return Ok(String::new());
        }

        // 引用检查：该 game 仍有活动实例引用该分支 → 拒绝删除
        if let Some(inst) = self.active_instance_of_game_branch(game_id, branch_name).await? {
            return Err(NodeAgentError::InvalidRequest {
                message: format!(
                    "cache in use by active instance {}, refuse remove: game_id={}, branch_name={}",
                    inst.id, game_id, branch_name
                ),
            });
        }

        // 版本检查：任一版本 refcount>0 或 Downloading → 拒绝
        for v in &versions {
            if v.refcount > 0 || v.status == DomainGameCacheStatus::Downloading {
                return Err(NodeAgentError::InvalidRequest {
                    message: format!(
                        "cache version {} in use (refcount={}, status={:?}), refuse remove: game_id={}, branch_name={}",
                        v.build_id, v.refcount, v.status, game_id, branch_name
                    ),
                });
            }
        }

        // 删除磁盘目录 + 记录（幂等）
        let mut removed = Vec::new();
        for v in &versions {
            if let Some(path) = &v.path {
                let dir = PathBuf::from(path);
                if dir.exists() {
                    tokio::fs::remove_dir_all(&dir)
                        .await
                        .map_err(|e| NodeAgentError::PathError {
                            message: format!("remove cache dir {} failed: {}", path, e),
                        })?;
                }
                removed.push(path.clone());
            }
        }
        self.game_cache_repos
            .delete(&game_id.to_string(), &branch_name.to_string())
            .await
            .map_err(|e| NodeAgentError::DBOperationFail {
                message: format!("delete game_cache failed: {e}"),
            })?;

        Ok(removed.join(","))
    }

    /// 启动时全量孤儿 GC（P2-D）：遍历所有缓存版本，按 (game,branch) 分组，
    /// 回收非 current 且 refcount==0 的孤儿版本（含上次崩溃残留的 Removed/半成品目录）。
    /// 失败只记日志，不影响启动。
    pub async fn gc_all_orphans(&self) {
        let versions = match self.game_cache_repos.get_all().await {
            Ok(v) => v,
            Err(e) => {
                log::warn!("gc_all_orphans get_all failed: {e}");
                return;
            }
        };
        let mut seen = std::collections::HashSet::new();
        for v in &versions {
            let key = format!("{}:{}", v.game_id, v.branch_name);
            if seen.insert(key) {
                if let Err(e) = self.gc_orphan_versions(&v.game_id, &v.branch_name).await {
                    log::warn!("gc_all_orphans 分支 {}:{} 失败: {}", v.game_id, v.branch_name, e);
                }
            }
        }
    }

    /// 该 game+branch 是否还有活动实例（Preparing/Running/Stopping）引用。
    /// 优先用实例落库的 game_id/branch_name（P2-A）；旧实例回退本地构建缓存解析。
    async fn active_instance_of_game_branch(
        &self,
        game_id: &str,
        branch_name: &str,
    ) -> Result<Option<crate::domain::GameInstance>, NodeAgentError> {
        let instances = self
            .game_instance_repos
            .get_all()
            .await
            .map_err(|e| NodeAgentError::DBOperationFail {
                message: format!("get instances failed: {e}"),
            })?;
        for inst in instances {
            let active = matches!(
                inst.status,
                crate::domain::GameInstanceStatus::Preparing
                    | crate::domain::GameInstanceStatus::Running
                    | crate::domain::GameInstanceStatus::Stopping
            );
            if !active {
                continue;
            }
            let same_game = if !inst.game_id.is_empty() {
                inst.game_id == game_id
            } else {
                match self.local_game_build_repos.get(inst.game_build_id.clone()).await {
                    Ok(lb) => lb.game.id == game_id,
                    Err(_) => true, // 解析不到 → 保守视为同 game
                }
            };
            let same_branch = inst.branch_name.is_empty() || inst.branch_name == branch_name;
            if same_game && same_branch {
                return Ok(Some(inst));
            }
        }
        Ok(None)
    }

    /// 释放实例对缓存版本的引用（stop/clean 时调用）：refcount −1，随后 GC 孤儿版本。
    /// 旧实例（无 cache_build_id 记录）直接跳过。
    async fn release_cache_reference(
        &self,
        instance: &crate::domain::GameInstance,
    ) -> Result<(), NodeAgentError> {
        if instance.cache_build_id.is_empty() {
            return Ok(());
        }
        let game_id = instance.game_id.clone();
        let branch_name = instance.branch_name.clone();
        let build_id = instance.cache_build_id.clone();
        if let Ok(Some(mut v)) = self
            .game_cache_repos
            .get_version(&game_id, &branch_name, &build_id)
            .await
        {
            v.refcount = (v.refcount - 1).max(0);
            let _ = self.game_cache_repos.save(&v).await;
        }
        self.gc_orphan_versions(&game_id, &branch_name).await
    }

    /// GC 孤儿版本：非 current（不是该分支 buildid 最大的 Available）且 refcount==0 → 删目录+记录。
    /// 只做"旧目录延迟删除"，不做版本保留（P2-A 口径；下载中的版本跳过）。
    async fn gc_orphan_versions(
        &self,
        game_id: &str,
        branch_name: &str,
    ) -> Result<(), NodeAgentError> {
        let versions = self
            .game_cache_repos
            .get_versions(&game_id.to_string(), &branch_name.to_string())
            .await
            .map_err(|e| NodeAgentError::DBOperationFail {
                message: format!("get game_cache versions failed: {e}"),
            })?;
        let current = versions
            .iter()
            .filter(|v| v.status == DomainGameCacheStatus::Available)
            .max_by(|a, b| crate::domain::buildid_cmp(&a.build_id, &b.build_id))
            .map(|v| v.build_id.clone());
        for v in versions {
            if v.status == DomainGameCacheStatus::Downloading {
                continue;
            }
            if Some(v.build_id.clone()) == current {
                continue;
            }
            if v.refcount > 0 {
                continue;
            }
            if let Some(path) = &v.path {
                let dir = PathBuf::from(path);
                if dir.exists() {
                    if let Err(e) = tokio::fs::remove_dir_all(&dir).await {
                        log::warn!("GC 删除缓存目录失败 {}: {}", path, e);
                    }
                }
            }
            match self
                .game_cache_repos
                .delete_version(&game_id.to_string(), &branch_name.to_string(), &v.build_id)
                .await
            {
                Ok(()) => log::info!(
                    "GC 回收孤儿缓存版本 {}:{}:{}",
                    game_id,
                    branch_name,
                    v.build_id
                ),
                Err(e) => log::warn!(
                    "GC 删除缓存记录失败 {}:{}:{}: {}",
                    game_id,
                    branch_name,
                    v.build_id,
                    e
                ),
            }
        }
        Ok(())
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
        // B-04/P1-3：记录探针声明（a2s 模式带查询宿主端口），供 RuntimeProbeService 使用
        game_instance.probe_mode = argument.probe_mode.clone();
        game_instance.query_host_port = argument.query_host_port;
        // P2-A：落库实例所属 game/branch（refcount 释放、remove_cache 判定用）
        game_instance.game_id = argument.build.game.id.clone();
        game_instance.branch_name = argument.branch_name.clone();
        game_instance.status = crate::domain::GameInstanceStatus::Preparing;
        self.game_instance_repos.save(&game_instance).await?;

        let local_game_build = self
            .local_game_build_repos
            .get(argument.build.build_id)
            .await
            .map_err(|err| NodeAgentError::DBOperationFail {
                message: format!("get local game build fail: {}", err),
            })?;

        // 解析 current 缓存版本（该分支 buildid 最大的 Available，P2-A）
        let mut game_cache = self
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

        // 游戏服务器目录映射（挂载具体 buildid 路径，不随 current 漂移）
        let host_path = game_cache
            .path
            .clone()
            .ok_or_else(|| NodeAgentError::InvalidRequest {
                message: format!(
                    "game cache path is empty for game_id={}, branch_name={}",
                    argument.build.game.id, argument.branch_name
                ),
            })?;

        // P2-A：实例持有该版本引用（refcount+1），记录挂载的 buildid；
        // 旧目录只有 refcount==0 且非 current 才被 GC 回收（延迟删除）。
        game_cache.refcount += 1;
        game_instance.cache_build_id = game_cache.build_id.clone();
        self.game_cache_repos
            .save(&game_cache)
            .await
            .map_err(|err| NodeAgentError::DBOperationFail {
                message: format!("save game cache refcount fail: {}", err),
            })?;
        self.game_instance_repos.save(&game_instance).await?;

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
                path: data_host_path.clone(),
            },
            container_file_path: ContainerFilePath {
                path: CONTAINER_DATA_PATH.to_string(),
            },
            mapped_permission: "rwx".to_string(),
        };
        v2.push(path_mapping);

        // 平台下发配置 → /data/.platform/game-config.json
        // 供容器内 config-render.sh 按 config-manifest.json 渲染游戏配置文件
        // （7dtd serverconfig.xml 等，见 adapter-framework-design.md §3.4）
        if !argument.config.is_empty() || !argument.credentials.is_empty() {
            let platform_dir = PathBuf::from(&data_host_path).join(".platform");
            tokio::fs::create_dir_all(&platform_dir)
                .await
                .map_err(|e| NodeAgentError::PathError {
                    message: format!("create platform config dir failed: {e}"),
                })?;
            if !argument.config.is_empty() {
                let config_json = serde_json::to_string(&argument.config).map_err(|e| {
                    NodeAgentError::Internal {
                        message: format!("serialize instance config: {e}"),
                    }
                })?;
                tokio::fs::write(platform_dir.join("game-config.json"), config_json)
                    .await
                    .map_err(|e| NodeAgentError::PathError {
                        message: format!("write game-config.json failed: {e}"),
                    })?;
                log::info!(
                    "instance {} platform config written to {}/.platform/game-config.json ({} keys)",
                    instance_id.0,
                    data_host_path,
                    argument.config.len()
                );
            }
            // M8：外部受限凭证 → /data/.platform/{key}
            // （如 cluster_token，供容器内 hook 复制到游戏配置目录）
            for (key, value) in &argument.credentials {
                tokio::fs::write(platform_dir.join(key), value)
                    .await
                    .map_err(|e| NodeAgentError::PathError {
                        message: format!("write .platform/{key} failed: {e}"),
                    })?;
                log::info!(
                    "instance {} credential {} written to {}/.platform/{}",
                    instance_id.0,
                    key,
                    data_host_path,
                    key
                );
            }
        }

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

            // 1) 优先 exec 容器内生命周期 stop.sh（save → 钩子优雅停止 → 兜底 TERM→KILL），
            //    与镜像契约（adapters/base 生命周期框架）对齐；失败降级为 docker stop。
            match self
                .container_client
                .exec(docker_id.clone(), vec!["/scripts/stop.sh".to_string()])
                .await
            {
                Ok(out) => log::info!(
                    "instance {} stop.sh exit_code={} stdout={} stderr={}",
                    instance_id,
                    out.exit_code,
                    out.stdout,
                    out.stderr
                ),
                Err(e) => log::warn!(
                    "instance {} exec stop.sh failed, fallback to docker stop: {}",
                    instance_id,
                    e
                ),
            }

            // 2) docker stop 兜底（graceful 30s 后 SIGKILL）
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

        // P2-A：释放实例对缓存版本的引用（refcount−1 + 孤儿 GC 延迟删除）。
        // 失败不影响实例清理主流程，只记日志。
        if let Err(e) = self.release_cache_reference(&game_instance).await {
            log::warn!("实例 {} 释放缓存引用失败: {}", instance_id, e);
        }

        Ok(())
    }
}