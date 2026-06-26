use async_trait::async_trait;
use tokio::sync::Mutex;
use tonic::transport::Channel;

use crate::domain::SnapshotRestorePlan;
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

#[async_trait]
impl AssetServiceFace for AssetServiceGrpcClient {
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

        let snapshot = response.into_inner().snapshot.ok_or_else(|| {
            NodeAgentError::Internal {
                message: "create_snapshot returned empty snapshot".to_string(),
            }
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

        let plan = response.into_inner().restore_plan.ok_or_else(|| {
            NodeAgentError::Internal {
                message: "get_snapshot_restore_plan returned empty plan".to_string(),
            }
        })?;

        Ok(SnapshotRestorePlan {
            snapshot_id: plan.snapshot_id,
            build_id: plan.build_id,
            storage_uri: plan.storage_uri,
            manifest_uri: plan.manifest_uri,
            checksum: plan.checksum,
            instance_data_path: plan.instance_data_path,
        })
    }

    async fn register_node_agent(
        &self,
        node_id: &str,
        endpoint: &str,
    ) -> Result<(), NodeAgentError> {
        use crate::proto::asset_service::{
            RegisterNodeAgentRequest, NodeAgent as ProtoNodeAgent,
        };

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

    async fn update_node_agent(
        &self,
        node_id: &str,
        endpoint: &str,
        status: &str,
        last_heartbeat_at: i64,
    ) -> Result<(), NodeAgentError> {
        use crate::proto::asset_service::{
            UpdateNodeAgentRequest, NodeAgent as ProtoNodeAgent,
        };

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
}
