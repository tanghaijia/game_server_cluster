use std::sync::Arc;

use async_trait::async_trait;
use tokio::sync::Mutex;
use tonic::transport::Channel;

use crate::{
    domain::{GameBuild, GameKind, SnapshotId, SnapshotReference, SnapshotType, VersionSelector},
    error::ControllerError,
    ports::{
        BuildResolver, CompleteSnapshotRecordRequest, CreateSnapshotRecordRequest, SnapshotRecord,
        SnapshotRestorePlan, SnapshotService,
    },
    proto::asset_service::{
        self, asset_service_client::AssetServiceClient,
    },
};

#[derive(Clone)]
pub struct AssetServiceGrpcClient {
    inner: Arc<Mutex<AssetServiceClient<Channel>>>,
}

impl AssetServiceGrpcClient {
    pub async fn connect(endpoint: String) -> Result<Self, tonic::transport::Error> {
        let client = AssetServiceClient::connect(endpoint).await?;
        Ok(Self { inner: Arc::new(Mutex::new(client)) })
    }
}

#[async_trait]
impl BuildResolver for AssetServiceGrpcClient {
    async fn resolve_build(
        &self,
        game: &GameKind,
        selector: &VersionSelector,
    ) -> Result<GameBuild, ControllerError> {
        let request = asset_service::ResolveGameBuildRequest {
            game: map_game_kind_to_proto(game),
            selector: Some(map_selector_to_proto(selector.clone())),
            custom_game: match game { GameKind::Custom(name) => Some(name.clone()), _ => None },
        };
        let mut client = self.inner.lock().await;
        let response = client
            .resolve_game_build(request)
            .await
            .map_err(map_status)?
            .into_inner()
            .build
            .ok_or_else(|| ControllerError::DependencyFailure {
                message: "asset-service returned empty build".to_string(),
            })?;
        Ok(map_build_from_proto(response))
    }
}

#[async_trait]
impl SnapshotService for AssetServiceGrpcClient {
    async fn create_snapshot_record(
        &self,
        request: CreateSnapshotRecordRequest,
    ) -> Result<SnapshotRecord, ControllerError> {
        let mut client = self.inner.lock().await;
        let response = client
            .create_snapshot(asset_service::CreateSnapshotRequest {
                instance_id: request.instance_id,
                build_id: request.build_id,
                snapshot_type: map_snapshot_type_to_proto(&request.snapshot_type),
                source_node: request.source_node,
            })
            .await
            .map_err(map_status)?
            .into_inner()
            .snapshot
            .ok_or_else(|| ControllerError::DependencyFailure {
                message: "asset-service returned empty snapshot".to_string(),
            })?;
        Ok(map_snapshot_record_from_proto(response))
    }

    async fn complete_snapshot_record(
        &self,
        request: CompleteSnapshotRecordRequest,
    ) -> Result<SnapshotRecord, ControllerError> {
        let mut client = self.inner.lock().await;
        let response = client
            .complete_snapshot(asset_service::CompleteSnapshotRequest {
                snapshot_id: request.snapshot_id.0,
                storage_uri: request.storage_uri,
                manifest_uri: request.manifest_uri,
                checksum: request.checksum,
            })
            .await
            .map_err(map_status)?
            .into_inner()
            .snapshot
            .ok_or_else(|| ControllerError::DependencyFailure {
                message: "asset-service returned empty snapshot".to_string(),
            })?;
        Ok(map_snapshot_record_from_proto(response))
    }

    async fn get_snapshot_restore_plan(
        &self,
        snapshot_id: &SnapshotId,
    ) -> Result<SnapshotRestorePlan, ControllerError> {
        let mut client = self.inner.lock().await;
        let response = client
            .get_snapshot_restore_plan(asset_service::GetSnapshotRestorePlanRequest {
                snapshot_id: snapshot_id.0.clone(),
            })
            .await
            .map_err(map_status)?
            .into_inner()
            .restore_plan
            .ok_or_else(|| ControllerError::DependencyFailure {
                message: "asset-service returned empty restore plan".to_string(),
            })?;
        Ok(SnapshotRestorePlan {
            snapshot_id: SnapshotId(response.snapshot_id),
            build_id: response.build_id,
            storage_uri: response.storage_uri,
            manifest_uri: response.manifest_uri,
            checksum: response.checksum,
            instance_data_path: response.instance_data_path,
        })
    }
}

fn map_status(status: tonic::Status) -> ControllerError {
    match status.code() {
        tonic::Code::NotFound => ControllerError::InstanceNotFound { instance_id: status.message().to_string() },
        tonic::Code::FailedPrecondition | tonic::Code::AlreadyExists => ControllerError::Conflict { message: status.message().to_string() },
        _ => ControllerError::DependencyFailure { message: status.to_string() },
    }
}

fn map_game_kind_to_proto(value: &GameKind) -> i32 {
    match value {
        GameKind::Dst => asset_service::GameKind::Dst as i32,
        GameKind::Minecraft => asset_service::GameKind::Minecraft as i32,
        GameKind::Custom(_) => asset_service::GameKind::Custom as i32,
    }
}

fn map_selector_to_proto(value: VersionSelector) -> asset_service::VersionSelector {
    asset_service::VersionSelector {
        selector: Some(match value {
            VersionSelector::Channel { channel } => asset_service::version_selector::Selector::Channel(channel),
            VersionSelector::BuildId { build_id } => asset_service::version_selector::Selector::BuildId(build_id),
        }),
    }
}

fn map_snapshot_type_to_proto(value: &SnapshotType) -> i32 {
    match value {
        SnapshotType::Manual => asset_service::SnapshotType::Manual as i32,
        SnapshotType::Scheduled => asset_service::SnapshotType::Scheduled as i32,
        SnapshotType::PreUpgrade => asset_service::SnapshotType::PreUpgrade as i32,
        SnapshotType::FinalStop => asset_service::SnapshotType::FinalStop as i32,
    }
}

fn map_game_kind_from_proto(value: i32, custom: Option<String>) -> GameKind {
    match asset_service::GameKind::try_from(value).unwrap_or(asset_service::GameKind::Unspecified) {
        asset_service::GameKind::Dst => GameKind::Dst,
        asset_service::GameKind::Minecraft => GameKind::Minecraft,
        asset_service::GameKind::Custom => GameKind::Custom(custom.unwrap_or_else(|| "custom".to_string())),
        asset_service::GameKind::Unspecified => GameKind::Dst,
    }
}

fn map_build_from_proto(value: asset_service::GameBuild) -> GameBuild {
    GameBuild {
        build_id: value.build_id,
        game: map_game_kind_from_proto(value.game, value.custom_game),
        channel: value.channel,
        adapter_version: value.adapter_version,
    }
}

fn map_snapshot_record_from_proto(value: asset_service::SnapshotRecord) -> SnapshotRecord {
    SnapshotRecord {
        snapshot: SnapshotReference {
            snapshot_id: SnapshotId(value.snapshot_id),
            storage_uri: value.storage_uri,
            manifest_uri: value.manifest_uri,
            checksum: value.checksum,
        },
        build_id: value.build_id,
        instance_data_path: value.instance_data_path,
    }
}
