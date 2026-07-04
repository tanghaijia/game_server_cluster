mod DockerImageClient;

use async_trait::async_trait;
use tokio::sync::Mutex;
use tonic::transport::Channel;

use crate::domain::{
    BuildCompatibility, GameBuild, ModEntry, ModManifest, NodeAgentInfo, SnapshotRecord,
    SnapshotRestorePlan,
};
use crate::error::NodeAgentError;
use crate::ports::AssetServiceFace;
use crate::proto::asset_service::{
    asset_service_client::AssetServiceClient, business_service_client::BusinessServiceClient,
};

/// 连接 asset_service 的 gRPC 客户端。
///
/// 同时封装了 AssetService 和 BusinessService 两个 gRPC 服务，
/// 并实现 `AssetServiceFace` port 接口。
pub struct AssetServiceGrpcClient {
    asset: Mutex<AssetServiceClient<Channel>>,
    business: Mutex<BusinessServiceClient<Channel>>,
}

impl AssetServiceGrpcClient {
    /// 连接到 asset_service 的 gRPC 端点。
    ///
    /// `addr` 格式如 `"http://127.0.0.1:50053"`。
    pub async fn connect(addr: &str) -> Result<Self, NodeAgentError> {
        let channel = Channel::from_shared(addr.to_string())
            .map_err(|e| NodeAgentError::Internal {
                message: format!("invalid asset_service address: {e}"),
            })?
            .connect()
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("failed to connect to asset_service: {e}"),
            })?;

        Ok(Self {
            asset: Mutex::new(AssetServiceClient::new(channel.clone())),
            business: Mutex::new(BusinessServiceClient::new(channel)),
        })
    }
}

// ==================== proto ↔ domain 映射辅助函数 ====================

fn map_game_build(
    proto: crate::proto::asset_service::GameBuild,
) -> Result<GameBuild, NodeAgentError> {
    let game = proto.game.ok_or_else(|| NodeAgentError::Internal {
        message: "game_build missing game".to_string(),
    })?;
    Ok(GameBuild {
        build_id: proto.build_id,
        game: crate::domain::Game {
            id: game.id,
            name: game.name,
            app_id: game.app_id,
        },
        channel: proto.channel,
        adapter_version: proto.adapter_version,
    })
}

fn map_snapshot_record(proto: crate::proto::asset_service::SnapshotRecord) -> SnapshotRecord {
    SnapshotRecord {
        snapshot_id: proto.snapshot_id,
        instance_id: proto.instance_id,
        build_id: proto.build_id,
        snapshot_type: proto.snapshot_type,
        instance_data_path: proto.instance_data_path,
        storage_uri: proto.storage_uri,
        manifest_uri: proto.manifest_uri,
        checksum: proto.checksum,
        status: proto.status,
        source_node: proto.source_node,
        created_at: proto.created_at,
        completed_at: proto.completed_at,
        failure_message: proto.failure_message,
        bucket: proto.bucket,
        key: proto.key,
        host: proto.host,
        host_port: proto.host_port,
    }
}

fn map_restore_plan(
    proto: crate::proto::asset_service::SnapshotRestorePlan,
) -> SnapshotRestorePlan {
    SnapshotRestorePlan {
        snapshot_id: proto.snapshot_id,
        build_id: proto.build_id,
        storage_uri: proto.storage_uri,
        manifest_uri: proto.manifest_uri,
        checksum: proto.checksum,
        instance_data_path: proto.instance_data_path,
    }
}

fn map_mod_manifest(proto: crate::proto::asset_service::ModManifest) -> ModManifest {
    ModManifest {
        manifest_id: proto.manifest_id,
        game_id: proto.game.map_or_else(String::new, |g| g.id),
        mods: proto
            .mods
            .into_iter()
            .map(|m| ModEntry {
                mod_id: m.mod_id,
                version: m.version,
                required: m.required,
            })
            .collect(),
        config_hash: proto.config_hash,
        compatibility_note: proto.compatibility_note,
        created_at: proto.created_at,
    }
}

fn map_compatibility(proto: crate::proto::asset_service::BuildCompatibility) -> BuildCompatibility {
    BuildCompatibility {
        compatible: proto.compatible,
        reason: proto.reason,
    }
}

fn map_node_agent(proto: crate::proto::asset_service::NodeAgent) -> NodeAgentInfo {
    NodeAgentInfo {
        node_id: proto.node_id,
        endpoint: proto.endpoint,
        status: proto.status,
        last_heartbeat_at: proto.last_heartbeat_at,
    }
}

// ==================== AssetServiceFace implementation ====================

#[async_trait]
impl AssetServiceFace for AssetServiceGrpcClient {
    // ---- GameBuild ----

    async fn resolve_game_build(
        &self,
        game_id: &str,
        channel: &str,
    ) -> Result<GameBuild, NodeAgentError> {
        use crate::proto::asset_service::{
            Game as ProtoGame, ResolveGameBuildRequest, VersionSelector, version_selector::Selector,
        };

        let mut client = self.asset.lock().await;
        let response = client
            .resolve_game_build(ResolveGameBuildRequest {
                game: Some(ProtoGame {
                    id: game_id.to_string(),
                    name: String::new(),
                    app_id: String::new(),
                }),
                selector: Some(VersionSelector {
                    selector: Some(Selector::Channel(channel.to_string())),
                }),
            })
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("resolve_game_build failed: {e}"),
            })?;

        let build = response
            .into_inner()
            .build
            .ok_or_else(|| NodeAgentError::Internal {
                message: "resolve_game_build returned empty build".to_string(),
            })?;
        map_game_build(build)
    }

    async fn get_game_build(&self, build_id: &str) -> Result<GameBuild, NodeAgentError> {
        use crate::proto::asset_service::GetGameBuildRequest;

        let mut client = self.asset.lock().await;
        let response = client
            .get_game_build(GetGameBuildRequest {
                build_id: build_id.to_string(),
            })
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("get_game_build failed: {e}"),
            })?;

        let build = response
            .into_inner()
            .build
            .ok_or_else(|| NodeAgentError::Internal {
                message: "get_game_build returned empty build".to_string(),
            })?;
        map_game_build(build)
    }

    // ---- Snapshot ----

    async fn create_snapshot_record(
        &self,
        instance_id: &str,
        build_id: Option<String>,
        snapshot_type: i32,
        source_node: Option<String>,
    ) -> Result<String, NodeAgentError> {
        use crate::proto::asset_service::CreateSnapshotRequest;

        let mut client = self.asset.lock().await;
        let response = client
            .create_snapshot(CreateSnapshotRequest {
                instance_id: instance_id.to_string(),
                build_id,
                snapshot_type,
                source_node,
            })
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("create_snapshot failed: {e}"),
            })?;

        let snapshot = response
            .into_inner()
            .snapshot
            .ok_or_else(|| NodeAgentError::Internal {
                message: "create_snapshot returned empty snapshot".to_string(),
            })?;
        Ok(snapshot.snapshot_id)
    }

    async fn complete_snapshot_record(
        &self,
        snapshot_id: &str,
        storage_uri: &str,
        manifest_uri: Option<String>,
        checksum: Option<String>,
    ) -> Result<(), NodeAgentError> {
        use crate::proto::asset_service::CompleteSnapshotRequest;

        let mut client = self.asset.lock().await;
        client
            .complete_snapshot(CompleteSnapshotRequest {
                snapshot_id: snapshot_id.to_string(),
                storage_uri: storage_uri.to_string(),
                manifest_uri,
                checksum,
            })
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("complete_snapshot failed: {e}"),
            })?;
        Ok(())
    }

    async fn fail_snapshot_record(
        &self,
        snapshot_id: &str,
        failure_message: &str,
    ) -> Result<(), NodeAgentError> {
        use crate::proto::asset_service::FailSnapshotRequest;

        let mut client = self.asset.lock().await;
        client
            .fail_snapshot(FailSnapshotRequest {
                snapshot_id: snapshot_id.to_string(),
                failure_message: failure_message.to_string(),
            })
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("fail_snapshot failed: {e}"),
            })?;
        Ok(())
    }

    async fn get_snapshot(&self, snapshot_id: &str) -> Result<SnapshotRecord, NodeAgentError> {
        use crate::proto::asset_service::GetSnapshotRequest;

        let mut client = self.asset.lock().await;
        let response = client
            .get_snapshot(GetSnapshotRequest {
                snapshot_id: snapshot_id.to_string(),
            })
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("get_snapshot failed: {e}"),
            })?;

        let snapshot = response
            .into_inner()
            .snapshot
            .ok_or_else(|| NodeAgentError::Internal {
                message: "get_snapshot returned empty snapshot".to_string(),
            })?;
        Ok(map_snapshot_record(snapshot))
    }

    async fn get_latest_snapshot(
        &self,
        instance_id: &str,
    ) -> Result<Option<SnapshotRecord>, NodeAgentError> {
        use crate::proto::asset_service::GetLatestSnapshotRequest;

        let mut client = self.asset.lock().await;
        let response = client
            .get_latest_snapshot(GetLatestSnapshotRequest {
                instance_id: instance_id.to_string(),
            })
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("get_latest_snapshot failed: {e}"),
            })?;

        Ok(response.into_inner().snapshot.map(map_snapshot_record))
    }

    async fn set_latest_snapshot(
        &self,
        instance_id: &str,
        snapshot_id: &str,
    ) -> Result<(), NodeAgentError> {
        use crate::proto::asset_service::SetLatestSnapshotRequest;

        let mut client = self.asset.lock().await;
        client
            .set_latest_snapshot(SetLatestSnapshotRequest {
                instance_id: instance_id.to_string(),
                snapshot_id: snapshot_id.to_string(),
            })
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("set_latest_snapshot failed: {e}"),
            })?;
        Ok(())
    }

    async fn list_snapshots(
        &self,
        instance_id: &str,
    ) -> Result<Vec<SnapshotRecord>, NodeAgentError> {
        use crate::proto::asset_service::ListSnapshotsRequest;

        let mut client = self.asset.lock().await;
        let response = client
            .list_snapshots(ListSnapshotsRequest {
                instance_id: instance_id.to_string(),
            })
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("list_snapshots failed: {e}"),
            })?;

        Ok(response
            .into_inner()
            .snapshots
            .into_iter()
            .map(map_snapshot_record)
            .collect())
    }

    async fn get_snapshot_restore_plan(
        &self,
        snapshot_id: &str,
    ) -> Result<SnapshotRestorePlan, NodeAgentError> {
        use crate::proto::asset_service::GetSnapshotRestorePlanRequest;

        let mut client = self.asset.lock().await;
        let response = client
            .get_snapshot_restore_plan(GetSnapshotRestorePlanRequest {
                snapshot_id: snapshot_id.to_string(),
            })
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("get_snapshot_restore_plan failed: {e}"),
            })?;

        let plan = response
            .into_inner()
            .restore_plan
            .ok_or_else(|| NodeAgentError::Internal {
                message: "get_snapshot_restore_plan returned empty plan".to_string(),
            })?;
        Ok(map_restore_plan(plan))
    }

    // ---- ModManifest ----

    async fn get_mod_manifest(&self, manifest_id: &str) -> Result<ModManifest, NodeAgentError> {
        use crate::proto::asset_service::GetModManifestRequest;

        let mut client = self.asset.lock().await;
        let response = client
            .get_mod_manifest(GetModManifestRequest {
                manifest_id: manifest_id.to_string(),
            })
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("get_mod_manifest failed: {e}"),
            })?;

        let manifest = response
            .into_inner()
            .manifest
            .ok_or_else(|| NodeAgentError::Internal {
                message: "get_mod_manifest returned empty manifest".to_string(),
            })?;
        Ok(map_mod_manifest(manifest))
    }

    async fn check_build_mod_compatibility(
        &self,
        build_id: &str,
        manifest_id: &str,
    ) -> Result<BuildCompatibility, NodeAgentError> {
        use crate::proto::asset_service::CheckBuildModCompatibilityRequest;

        let mut client = self.asset.lock().await;
        let response = client
            .check_build_mod_compatibility(CheckBuildModCompatibilityRequest {
                build_id: build_id.to_string(),
                manifest_id: manifest_id.to_string(),
            })
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("check_build_mod_compatibility failed: {e}"),
            })?;

        let compatibility =
            response
                .into_inner()
                .compatibility
                .ok_or_else(|| NodeAgentError::Internal {
                    message: "check_build_mod_compatibility returned empty result".to_string(),
                })?;
        Ok(map_compatibility(compatibility))
    }

    // ---- NodeAgent ----

    async fn register_node_agent(
        &self,
        node_id: &str,
        endpoint: &str,
    ) -> Result<(), NodeAgentError> {
        use crate::proto::asset_service::{NodeAgent as ProtoNodeAgent, RegisterNodeAgentRequest};

        let mut client = self.business.lock().await;
        client
            .register_node_agent(RegisterNodeAgentRequest {
                agent: Some(ProtoNodeAgent {
                    node_id: node_id.to_string(),
                    endpoint: endpoint.to_string(),
                    status: "online".to_string(),
                    last_heartbeat_at: 0,
                }),
            })
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("register_node_agent failed: {e}"),
            })?;
        Ok(())
    }

    async fn get_node_agent(&self, node_id: &str) -> Result<NodeAgentInfo, NodeAgentError> {
        use crate::proto::asset_service::GetNodeAgentRequest;

        let mut client = self.business.lock().await;
        let response = client
            .get_node_agent(GetNodeAgentRequest {
                node_id: node_id.to_string(),
            })
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("get_node_agent failed: {e}"),
            })?;

        let agent = response
            .into_inner()
            .agent
            .ok_or_else(|| NodeAgentError::Internal {
                message: "get_node_agent returned empty agent".to_string(),
            })?;
        Ok(map_node_agent(agent))
    }

    async fn update_node_agent(
        &self,
        node_id: &str,
        endpoint: &str,
        status: &str,
        last_heartbeat_at: i64,
    ) -> Result<(), NodeAgentError> {
        use crate::proto::asset_service::{NodeAgent as ProtoNodeAgent, UpdateNodeAgentRequest};

        let mut client = self.business.lock().await;
        client
            .update_node_agent(UpdateNodeAgentRequest {
                agent: Some(ProtoNodeAgent {
                    node_id: node_id.to_string(),
                    endpoint: endpoint.to_string(),
                    status: status.to_string(),
                    last_heartbeat_at,
                }),
            })
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("update_node_agent failed: {e}"),
            })?;
        Ok(())
    }

    async fn unregister_node_agent(&self, node_id: &str) -> Result<(), NodeAgentError> {
        use crate::proto::asset_service::UnregisterNodeAgentRequest;

        let mut client = self.business.lock().await;
        client
            .unregister_node_agent(UnregisterNodeAgentRequest {
                node_id: node_id.to_string(),
            })
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("unregister_node_agent failed: {e}"),
            })?;
        Ok(())
    }

    async fn list_node_agents(&self) -> Result<Vec<NodeAgentInfo>, NodeAgentError> {
        use crate::proto::asset_service::ListNodeAgentsRequest;

        let mut client = self.business.lock().await;
        let response = client
            .list_node_agents(ListNodeAgentsRequest {})
            .await
            .map_err(|e| NodeAgentError::Internal {
                message: format!("list_node_agents failed: {e}"),
            })?;

        Ok(response
            .into_inner()
            .agents
            .into_iter()
            .map(map_node_agent)
            .collect())
    }
}
