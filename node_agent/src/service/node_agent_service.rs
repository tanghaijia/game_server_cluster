use std::sync::Arc;

use aws_sdk_s3::Client;
use chrono::Utc;
use lmrc_docker::DockerClient;

use crate::domain::{
    ConatinerType, GameContainer, HostSnapShotDataPath, LocalGameBuildManager, RemoteImage,
};
use crate::ports::{ContainerClient, GameInstanceRepository};
use crate::service::download_and_extract_tar_zst;
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
        operation_id: &OperationId,
    ) -> Result<InstanceRuntimeRecord, NodeAgentError>;

    async fn restore_snapshot(
        &self,
        request: SnapshotRestoreRequest,
    ) -> Result<SnapshotRestoreResult, NodeAgentError>;
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
    s3_client: Arc<Client>,
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
        s3_client: Arc<Client>,
    ) -> Self {
        Self {
            game_instance_repos,
            system_info,
            asset_service,
            container_client: container_client,
            local_game_build_manager: LocalGameBuildManager::new(),
            s3_client,
        }
    }

    pub async fn stop_instance(&self, _instance_id: InstanceId) -> Result<(), NodeAgentError> {
        todo!()
    }

    pub async fn create_snapshot(
        &self,
        _request: SnapshotCaptureRequest,
    ) -> Result<(), NodeAgentError> {
        todo!()
    }

    pub async fn inspect_instance(
        &self,
        instance_id: &InstanceId,
    ) -> Result<InstanceRuntimeRecord, NodeAgentError> {
        todo!()
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
        // 1. 拉取镜像
        let remote_img = RemoteImage {
            id: "id".to_string(),
            name: "name".to_string(),
            tag: "tag".to_string(),
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
            .record_game_build_from_image(&request.build, &image)
            .map_err(|e| NodeAgentError::ImageRepositoryRequestFail {
                message: e.to_string(),
            })?;

        Ok(BuildPreparationResult {
            build_root: "asset_service".to_string(),
            prepared_at: Utc::now(),
            build_id: request.build.build_id,
        })
    }

    async fn start_instance(
        &self,
        argument: StartInstanceArgument,
        _operation_id: &OperationId,
    ) -> Result<InstanceRuntimeRecord, NodeAgentError> {
        let instance_id = argument.instance_id;

        let mut game_instance = self.game_instance_repos.get(instance_id.0).await?;
        game_instance.status = crate::domain::GameInstanceStatus::Preparing;
        self.game_instance_repos.save(&game_instance).await?;

        let local_game_build = self
            .local_game_build_manager
            .get(argument.build.build_id)
            .await
            .map_err(|err| NodeAgentError::DBOperationFail {
                message: "get local game build fail".to_string(),
            })?;

        let container = self
            .container_client
            .create_container(local_game_build, None, None, None)
            .await?;

        game_instance.status = crate::domain::GameInstanceStatus::Running;
        self.game_instance_repos.save(&game_instance).await?;

        todo!()
    }

    async fn restore_snapshot(
        &self,
        request: SnapshotRestoreRequest,
    ) -> Result<SnapshotRestoreResult, NodeAgentError> {
        let instance_id = request.instance_id.0.clone();
        // 获取snapshot
        let snapshot = self
            .asset_service
            .get_latest_snapshot(instance_id.as_str())
            .await?;
        if snapshot.is_none() {
            return Err(NodeAgentError::EmptySnapShotFail {
                message: format!("get empty snapshot for {}", instance_id.as_str()),
            });
        }

        // 下载到目录/data/game-instances/{game_intance_id}
        let snapshot_record = snapshot.unwrap();
        let data_path = HostSnapShotDataPath::new(instance_id.clone());
        let restore_path_string = data_path.as_ref().display().to_string();
        let _manifest = download_and_extract_tar_zst(
            &self.s3_client,
            &snapshot_record.bucket,
            &snapshot_record.key,
            data_path,
        )
        .await
        .map_err(|err| NodeAgentError::S3DownloadFail {
            message: err.to_string(),
        })?;

        let result = SnapshotRestoreResult {
            snapshot_id: snapshot_record.snapshot_id,
            restored_at: Utc::now(),
            restore_path: restore_path_string,
        };
        Ok(result)
    }
}
