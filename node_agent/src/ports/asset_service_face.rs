use async_trait::async_trait;

use crate::domain::{
    BuildCompatibility, GameBuild, ModManifest, NodeAgentInfo, SnapshotRecord,
    SnapshotRestorePlan,
};
use crate::error::NodeAgentError;

/// NodeAgent 对 asset_service 的完整依赖接口。
///
/// 涵盖两个 gRPC 服务：
/// - `AssetService`：游戏资产核心（GameBuild、Snapshot、ModManifest）
/// - `BusinessService`：基础管理（Game、Node、NodeAgent CRUD）
#[async_trait]
pub trait AssetServiceFace: Send + Sync {
    // ==================== AssetService ====================

    /// 通过 channel 解析最新的 GameBuild。
    async fn resolve_game_build(
        &self,
        game_id: &str,
        channel: &str,
    ) -> Result<GameBuild, NodeAgentError>;

    /// 根据 build_id 获取 GameBuild。
    async fn get_game_build(&self, build_id: &str) -> Result<GameBuild, NodeAgentError>;

    /// 创建快照记录，返回新快照 ID。
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

    /// 获取单个快照。
    async fn get_snapshot(&self, snapshot_id: &str) -> Result<SnapshotRecord, NodeAgentError>;

    /// 获取实例的最新快照。
    async fn get_latest_snapshot(
        &self,
        instance_id: &str,
    ) -> Result<Option<SnapshotRecord>, NodeAgentError>;

    /// 设置实例的最新快照。
    async fn set_latest_snapshot(
        &self,
        instance_id: &str,
        snapshot_id: &str,
    ) -> Result<(), NodeAgentError>;

    /// 列出实例的所有快照。
    async fn list_snapshots(
        &self,
        instance_id: &str,
    ) -> Result<Vec<SnapshotRecord>, NodeAgentError>;

    /// 获取快照恢复计划。
    async fn get_snapshot_restore_plan(
        &self,
        snapshot_id: &str,
    ) -> Result<SnapshotRestorePlan, NodeAgentError>;

    /// 获取模组清单。
    async fn get_mod_manifest(
        &self,
        manifest_id: &str,
    ) -> Result<ModManifest, NodeAgentError>;

    /// 检查构建与模组清单的兼容性。
    async fn check_build_mod_compatibility(
        &self,
        build_id: &str,
        manifest_id: &str,
    ) -> Result<BuildCompatibility, NodeAgentError>;

    // ==================== BusinessService — NodeAgent ====================

    /// 注册节点代理。
    async fn register_node_agent(
        &self,
        node_id: &str,
        endpoint: &str,
    ) -> Result<(), NodeAgentError>;

    /// 获取节点代理信息。
    async fn get_node_agent(&self, node_id: &str) -> Result<NodeAgentInfo, NodeAgentError>;

    /// 更新节点状态（心跳）。
    async fn update_node_agent(
        &self,
        node_id: &str,
        endpoint: &str,
        status: &str,
        last_heartbeat_at: i64,
    ) -> Result<(), NodeAgentError>;

    /// 注销节点代理。
    async fn unregister_node_agent(&self, node_id: &str) -> Result<(), NodeAgentError>;

    /// 列出所有节点代理。
    async fn list_node_agents(&self) -> Result<Vec<NodeAgentInfo>, NodeAgentError>;
}
