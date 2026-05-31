use std::sync::Arc;

use chrono::{DateTime, Utc};
use tonic::{Request, Response, Status};

use crate::{
    domain::{
        BuildCompatibility, BuildId, BuildStatus, GameBuild, GameKind, ModEntry, ModManifest,
        ModManifestId, SnapshotRecord, SnapshotRestorePlan, SnapshotStatus, SnapshotType,
        VersionSelector,
    },
    error::AssetServiceError,
    ports::{BuildRepository, Clock, ModManifestRepository, SnapshotRepository},
    proto::asset_service::{
        self,
        asset_service_server::AssetService as AssetRpc,
        BuildCompatibility as ProtoBuildCompatibility, CompleteSnapshotRequest,
        CompleteSnapshotResponse, CreateSnapshotRequest, CreateSnapshotResponse,
        FailSnapshotRequest, FailSnapshotResponse, GameBuild as ProtoGameBuild,
        GetGameBuildRequest, GetGameBuildResponse, GetLatestSnapshotRequest,
        GetLatestSnapshotResponse, GetModManifestRequest, GetModManifestResponse,
        GetSnapshotRequest, GetSnapshotResponse, GetSnapshotRestorePlanRequest,
        GetSnapshotRestorePlanResponse, ListSnapshotsRequest, ListSnapshotsResponse,
        ModEntry as ProtoModEntry,
        ModManifest as ProtoModManifest, RegisterGameBuildRequest, RegisterGameBuildResponse,
        RegisterModManifestRequest, RegisterModManifestResponse, ResolveGameBuildRequest,
        ResolveGameBuildResponse, SnapshotRecord as ProtoSnapshotRecord,
        SnapshotRestorePlan as ProtoSnapshotRestorePlan,
    },
    service::{
        AssetService, CompleteSnapshotRequest as CompleteSnapshotCmd,
        CreateSnapshotRequest as CreateSnapshotCmd, FailSnapshotRequest as FailSnapshotCmd,
        RegisterBuildRequest,
    },
};

pub struct GrpcAssetService<B, S, M, C>
where
    B: BuildRepository,
    S: SnapshotRepository,
    M: ModManifestRepository,
    C: Clock,
{
    service: Arc<AssetService<B, S, M, C>>,
}

impl<B, S, M, C> GrpcAssetService<B, S, M, C>
where
    B: BuildRepository,
    S: SnapshotRepository,
    M: ModManifestRepository,
    C: Clock,
{
    pub fn new(service: Arc<AssetService<B, S, M, C>>) -> Self {
        Self { service }
    }
}

#[tonic::async_trait]
impl<B, S, M, C> AssetRpc for GrpcAssetService<B, S, M, C>
where
    B: BuildRepository + 'static,
    S: SnapshotRepository + 'static,
    M: ModManifestRepository + 'static,
    C: Clock + 'static,
{
    async fn resolve_game_build(
        &self,
        request: Request<ResolveGameBuildRequest>,
    ) -> Result<Response<ResolveGameBuildResponse>, Status> {
        println!("[resolve_game_build] ");
        let request = request.into_inner();
        let selector = request
            .selector
            .ok_or_else(|| Status::invalid_argument("selector is required"))?;
        let build = self
            .service
            .resolve_game_build(
                map_game_kind(request.game, request.custom_game)?,
                map_selector(selector),
            )
            .await
            .map_err(map_error)?;
        Ok(Response::new(ResolveGameBuildResponse {
            build: Some(map_build(build)),
        }))
    }

    async fn register_game_build(
        &self,
        request: Request<RegisterGameBuildRequest>,
    ) -> Result<Response<RegisterGameBuildResponse>, Status> {
        let request = request.into_inner();
        let build = request
            .build
            .ok_or_else(|| Status::invalid_argument("build is required"))?;
        let build = self
            .service
            .register_game_build(RegisterBuildRequest {
                build: map_build_from_proto(build)?,
            })
            .await
            .map_err(map_error)?;
        Ok(Response::new(RegisterGameBuildResponse {
            build: Some(map_build(build)),
        }))
    }

    async fn get_game_build(
        &self,
        request: Request<GetGameBuildRequest>,
    ) -> Result<Response<GetGameBuildResponse>, Status> {
        let build = self
            .service
            .get_game_build(&request.into_inner().build_id)
            .await
            .map_err(map_error)?;
        Ok(Response::new(GetGameBuildResponse {
            build: Some(map_build(build)),
        }))
    }

    async fn create_snapshot(
        &self,
        request: Request<CreateSnapshotRequest>,
    ) -> Result<Response<CreateSnapshotResponse>, Status> {
        let request = request.into_inner();
        let snapshot = self
            .service
            .create_snapshot(CreateSnapshotCmd {
                instance_id: request.instance_id,
                build_id: request.build_id,
                snapshot_type: map_snapshot_type(request.snapshot_type)?,
                source_node: request.source_node,
            })
            .await
            .map_err(map_error)?;
        Ok(Response::new(CreateSnapshotResponse {
            snapshot: Some(map_snapshot(snapshot)),
        }))
    }

    async fn complete_snapshot(
        &self,
        request: Request<CompleteSnapshotRequest>,
    ) -> Result<Response<CompleteSnapshotResponse>, Status> {
        let request = request.into_inner();
        let snapshot = self
            .service
            .complete_snapshot(CompleteSnapshotCmd {
                snapshot_id: request.snapshot_id,
                storage_uri: request.storage_uri,
                manifest_uri: request.manifest_uri,
                checksum: request.checksum,
            })
            .await
            .map_err(map_error)?;
        Ok(Response::new(CompleteSnapshotResponse {
            snapshot: Some(map_snapshot(snapshot)),
        }))
    }

    async fn fail_snapshot(
        &self,
        request: Request<FailSnapshotRequest>,
    ) -> Result<Response<FailSnapshotResponse>, Status> {
        let request = request.into_inner();
        let snapshot = self
            .service
            .fail_snapshot(FailSnapshotCmd {
                snapshot_id: request.snapshot_id,
                failure_message: request.failure_message,
            })
            .await
            .map_err(map_error)?;
        Ok(Response::new(FailSnapshotResponse {
            snapshot: Some(map_snapshot(snapshot)),
        }))
    }

    async fn get_snapshot(
        &self,
        request: Request<GetSnapshotRequest>,
    ) -> Result<Response<GetSnapshotResponse>, Status> {
        let snapshot = self
            .service
            .get_snapshot(&request.into_inner().snapshot_id)
            .await
            .map_err(map_error)?;
        Ok(Response::new(GetSnapshotResponse {
            snapshot: Some(map_snapshot(snapshot)),
        }))
    }

    async fn get_latest_snapshot(
        &self,
        request: Request<GetLatestSnapshotRequest>,
    ) -> Result<Response<GetLatestSnapshotResponse>, Status> {
        let snapshot = self
            .service
            .get_latest_snapshot(&request.into_inner().instance_id)
            .await
            .map_err(map_error)?;
        Ok(Response::new(GetLatestSnapshotResponse {
            snapshot: snapshot.map(map_snapshot),
        }))
    }

    async fn set_latest_snapshot(
        &self,
        request: Request<asset_service::SetLatestSnapshotRequest>,
    ) -> Result<Response<asset_service::SetLatestSnapshotResponse>, Status> {
        let request = request.into_inner();
        let snapshot = self
            .service
            .set_latest_snapshot(&request.instance_id, &request.snapshot_id)
            .await
            .map_err(map_error)?;
        Ok(Response::new(asset_service::SetLatestSnapshotResponse {
            snapshot: Some(map_snapshot(snapshot)),
        }))
    }

    async fn get_snapshot_restore_plan(
        &self,
        request: Request<GetSnapshotRestorePlanRequest>,
    ) -> Result<Response<GetSnapshotRestorePlanResponse>, Status> {
        let plan = self
            .service
            .get_snapshot_restore_plan(&request.into_inner().snapshot_id)
            .await
            .map_err(map_error)?;
        Ok(Response::new(GetSnapshotRestorePlanResponse {
            restore_plan: Some(map_restore_plan(plan)),
        }))
    }

    async fn list_snapshots(
        &self,
        request: Request<ListSnapshotsRequest>,
    ) -> Result<Response<ListSnapshotsResponse>, Status> {
        let snapshots = self
            .service
            .list_snapshots(&request.into_inner().instance_id)
            .await
            .map_err(map_error)?;
        Ok(Response::new(ListSnapshotsResponse {
            snapshots: snapshots.into_iter().map(map_snapshot).collect(),
        }))
    }

    async fn register_mod_manifest(
        &self,
        request: Request<RegisterModManifestRequest>,
    ) -> Result<Response<RegisterModManifestResponse>, Status> {
        let manifest = request
            .into_inner()
            .manifest
            .ok_or_else(|| Status::invalid_argument("manifest is required"))?;
        let manifest = self
            .service
            .register_mod_manifest(map_manifest_from_proto(manifest)?)
            .await
            .map_err(map_error)?;
        Ok(Response::new(RegisterModManifestResponse {
            manifest: Some(map_manifest(manifest)),
        }))
    }

    async fn get_mod_manifest(
        &self,
        request: Request<GetModManifestRequest>,
    ) -> Result<Response<GetModManifestResponse>, Status> {
        let manifest = self
            .service
            .get_mod_manifest(&request.into_inner().manifest_id)
            .await
            .map_err(map_error)?;
        Ok(Response::new(GetModManifestResponse {
            manifest: Some(map_manifest(manifest)),
        }))
    }

    async fn check_build_mod_compatibility(
        &self,
        request: Request<asset_service::CheckBuildModCompatibilityRequest>,
    ) -> Result<Response<asset_service::CheckBuildModCompatibilityResponse>, Status> {
        let request = request.into_inner();
        let compatibility = self
            .service
            .check_build_mod_compatibility(&request.build_id, &request.manifest_id)
            .await
            .map_err(map_error)?;
        Ok(Response::new(
            asset_service::CheckBuildModCompatibilityResponse {
                compatibility: Some(map_compatibility(compatibility)),
            },
        ))
    }
}

fn map_error(error: AssetServiceError) -> Status {
    match error {
        AssetServiceError::InvalidRequest { message } => Status::invalid_argument(message),
        AssetServiceError::BuildNotFound { build_id } => Status::not_found(build_id),
        AssetServiceError::SnapshotNotFound { snapshot_id } => Status::not_found(snapshot_id),
        AssetServiceError::ModManifestNotFound { manifest_id } => Status::not_found(manifest_id),
        AssetServiceError::Conflict { message } => Status::failed_precondition(message),
        AssetServiceError::Internal { message } => Status::internal(message),
    }
}

fn map_selector(value: asset_service::VersionSelector) -> VersionSelector {
    match value.selector {
        Some(asset_service::version_selector::Selector::Channel(channel)) => {
            VersionSelector::Channel { channel }
        }
        Some(asset_service::version_selector::Selector::BuildId(build_id)) => {
            VersionSelector::BuildId { build_id }
        }
        None => VersionSelector::Channel {
            channel: "public".to_string(),
        },
    }
}

fn map_game_kind(value: i32, custom: Option<String>) -> Result<GameKind, Status> {
    match asset_service::GameKind::try_from(value)
        .unwrap_or(asset_service::GameKind::Unspecified)
    {
        asset_service::GameKind::Dst => Ok(GameKind::Dst),
        asset_service::GameKind::Minecraft => Ok(GameKind::Minecraft),
        asset_service::GameKind::Custom => {
            Ok(GameKind::Custom(custom.unwrap_or_else(|| "custom".to_string())))
        }
        asset_service::GameKind::Unspecified => Err(Status::invalid_argument("game is required")),
    }
}

fn map_build_from_proto(value: ProtoGameBuild) -> Result<GameBuild, Status> {
    Ok(GameBuild {
        build_id: BuildId(value.build_id),
        game: map_game_kind(value.game, value.custom_game)?,
        channel: value.channel,
        adapter_version: value.adapter_version,
        upstream_version: value.upstream_version,
        artifact_uri: value.artifact_uri,
        checksum: value.checksum,
        status: map_build_status(value.status)?,
        pinned: value.pinned,
        resolved_at: parse_time(&value.resolved_at)?,
        created_at: parse_time(&value.created_at)?,
        updated_at: parse_time(&value.updated_at)?,
    })
}

fn map_manifest_from_proto(value: ProtoModManifest) -> Result<ModManifest, Status> {
    Ok(ModManifest {
        manifest_id: ModManifestId(value.manifest_id),
        game: map_game_kind(value.game, value.custom_game)?,
        mods: value.mods.into_iter().map(map_mod_entry).collect(),
        config_hash: value.config_hash,
        compatibility_note: value.compatibility_note,
        created_at: parse_time(&value.created_at)?,
    })
}

fn map_mod_entry(value: ProtoModEntry) -> ModEntry {
    ModEntry {
        mod_id: value.mod_id,
        version: value.version,
        required: value.required,
    }
}

fn map_build(value: GameBuild) -> ProtoGameBuild {
    ProtoGameBuild {
        build_id: value.build_id.0,
        game: map_game_kind_to_proto(&value.game),
        channel: value.channel,
        adapter_version: value.adapter_version,
        upstream_version: value.upstream_version,
        artifact_uri: value.artifact_uri,
        checksum: value.checksum,
        status: map_build_status_to_proto(&value.status),
        pinned: value.pinned,
        resolved_at: value.resolved_at.to_rfc3339(),
        created_at: value.created_at.to_rfc3339(),
        updated_at: value.updated_at.to_rfc3339(),
        custom_game: match value.game {
            GameKind::Custom(name) => Some(name),
            _ => None,
        },
    }
}

fn map_snapshot(value: SnapshotRecord) -> ProtoSnapshotRecord {
    ProtoSnapshotRecord {
        snapshot_id: value.snapshot_id.0,
        instance_id: value.instance_id,
        build_id: value.build_id.map(|id| id.0),
        snapshot_type: map_snapshot_type_to_proto(&value.snapshot_type),
        instance_data_path: value.instance_data_path,
        storage_uri: value.storage_uri,
        manifest_uri: value.manifest_uri,
        checksum: value.checksum,
        status: map_snapshot_status_to_proto(&value.status),
        source_node: value.source_node,
        created_at: value.created_at.to_rfc3339(),
        completed_at: value.completed_at.map(|v| v.to_rfc3339()),
        failure_message: value.failure_message,
    }
}

fn map_restore_plan(value: SnapshotRestorePlan) -> ProtoSnapshotRestorePlan {
    ProtoSnapshotRestorePlan {
        snapshot_id: value.snapshot_id.0,
        build_id: value.build_id.map(|id| id.0),
        storage_uri: value.storage_uri,
        manifest_uri: value.manifest_uri,
        checksum: value.checksum,
        instance_data_path: value.instance_data_path,
    }
}

fn map_manifest(value: ModManifest) -> ProtoModManifest {
    ProtoModManifest {
        manifest_id: value.manifest_id.0,
        game: map_game_kind_to_proto(&value.game),
        mods: value
            .mods
            .into_iter()
            .map(|entry| ProtoModEntry {
                mod_id: entry.mod_id,
                version: entry.version,
                required: entry.required,
            })
            .collect(),
        config_hash: value.config_hash,
        compatibility_note: value.compatibility_note,
        created_at: value.created_at.to_rfc3339(),
        custom_game: match value.game {
            GameKind::Custom(name) => Some(name),
            _ => None,
        },
    }
}

fn map_compatibility(value: BuildCompatibility) -> ProtoBuildCompatibility {
    ProtoBuildCompatibility {
        compatible: value.compatible,
        reason: value.reason,
    }
}

fn map_build_status(value: i32) -> Result<BuildStatus, Status> {
    Ok(match asset_service::BuildStatus::try_from(value)
        .unwrap_or(asset_service::BuildStatus::Unspecified)
    {
        asset_service::BuildStatus::Discovered => BuildStatus::Discovered,
        asset_service::BuildStatus::Resolving => BuildStatus::Resolving,
        asset_service::BuildStatus::Available => BuildStatus::Available,
        asset_service::BuildStatus::Deprecated => BuildStatus::Deprecated,
        asset_service::BuildStatus::Unavailable => BuildStatus::Unavailable,
        asset_service::BuildStatus::Deleted => BuildStatus::Deleted,
        asset_service::BuildStatus::Unspecified => {
            return Err(Status::invalid_argument("build status is required"))
        }
    })
}

fn map_snapshot_type(value: i32) -> Result<SnapshotType, Status> {
    Ok(match asset_service::SnapshotType::try_from(value)
        .unwrap_or(asset_service::SnapshotType::Unspecified)
    {
        asset_service::SnapshotType::Manual => SnapshotType::Manual,
        asset_service::SnapshotType::Scheduled => SnapshotType::Scheduled,
        asset_service::SnapshotType::PreUpgrade => SnapshotType::PreUpgrade,
        asset_service::SnapshotType::FinalStop => SnapshotType::FinalStop,
        asset_service::SnapshotType::Unspecified => {
            return Err(Status::invalid_argument("snapshot_type is required"))
        }
    })
}

fn map_game_kind_to_proto(value: &GameKind) -> i32 {
    match value {
        GameKind::Dst => asset_service::GameKind::Dst as i32,
        GameKind::Minecraft => asset_service::GameKind::Minecraft as i32,
        GameKind::Custom(_) => asset_service::GameKind::Custom as i32,
    }
}

fn map_build_status_to_proto(value: &BuildStatus) -> i32 {
    match value {
        BuildStatus::Discovered => asset_service::BuildStatus::Discovered as i32,
        BuildStatus::Resolving => asset_service::BuildStatus::Resolving as i32,
        BuildStatus::Available => asset_service::BuildStatus::Available as i32,
        BuildStatus::Deprecated => asset_service::BuildStatus::Deprecated as i32,
        BuildStatus::Unavailable => asset_service::BuildStatus::Unavailable as i32,
        BuildStatus::Deleted => asset_service::BuildStatus::Deleted as i32,
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

fn map_snapshot_status_to_proto(value: &SnapshotStatus) -> i32 {
    match value {
        SnapshotStatus::Pending => asset_service::SnapshotStatus::Pending as i32,
        SnapshotStatus::Running => asset_service::SnapshotStatus::Running as i32,
        SnapshotStatus::Uploading => asset_service::SnapshotStatus::Uploading as i32,
        SnapshotStatus::Completed => asset_service::SnapshotStatus::Completed as i32,
        SnapshotStatus::Failed => asset_service::SnapshotStatus::Failed as i32,
        SnapshotStatus::Expired => asset_service::SnapshotStatus::Expired as i32,
    }
}

fn parse_time(value: &str) -> Result<DateTime<Utc>, Status> {
    DateTime::parse_from_rfc3339(value)
        .map(|v| v.with_timezone(&Utc))
        .map_err(|_| Status::invalid_argument(format!("invalid RFC3339 time: {value}")))
}
