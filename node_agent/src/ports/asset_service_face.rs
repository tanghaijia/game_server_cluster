use async_trait::async_trait;

use crate::domain::SnapshotRestorePlan;
use crate::error::NodeAgentError;

/// NodeAgent 对 asset_service 的依赖接口。
///
/// 定义 NodeAgent 需要从 asset_service 获取或推送的所有数据操作。
#[async_trait]
pub trait AssetServiceFace: Send + Sync {
    /// 在 asset_service 创建快照记录，返回新快照的 ID。
    async fn create_snapshot_record(
        &self,
        instance_id: &str,
        build_id: Option<String>,
        snapshot_type: i32,
        source_node: Option<String>,
    ) -> Result<String, NodeAgentError>;

    /// 完成快照（上传完成后调用）。
    async fn complete_snapshot_record(
        &self,
        snapshot_id: &str,
        storage_uri: &str,
        manifest_uri: Option<String>,
        checksum: Option<String>,
    ) -> Result<(), NodeAgentError>;

    /// 标记快照失败。
    async fn fail_snapshot_record(
        &self,
        snapshot_id: &str,
        failure_message: &str,
    ) -> Result<(), NodeAgentError>;

    /// 获取快照恢复计划。
    async fn get_snapshot_restore_plan(
        &self,
        snapshot_id: &str,
    ) -> Result<SnapshotRestorePlan, NodeAgentError>;

    /// 注册当前节点到 asset_service。
    async fn register_node_agent(
        &self,
        node_id: &str,
        endpoint: &str,
    ) -> Result<(), NodeAgentError>;

    /// 更新节点状态（心跳）。
    async fn update_node_agent(
        &self,
        node_id: &str,
        endpoint: &str,
        status: &str,
        last_heartbeat_at: i64,
    ) -> Result<(), NodeAgentError>;
}
