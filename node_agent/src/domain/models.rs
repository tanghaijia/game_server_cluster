use super::{ContainerPortMapping, InstanceId, NodeId, OperationId};
use crate::domain::game_build::GameBuild;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;

/// 从 asset_service 获取的快照恢复计划。
#[derive(Debug, Clone)]
pub struct SnapshotRestorePlan {
    pub snapshot_id: String,
    pub build_id: Option<String>,
    pub storage_uri: String,
    pub manifest_uri: Option<String>,
    pub checksum: Option<String>,
    pub instance_data_path: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ResourceRequirements {
    pub cpu_millis: u32,
    pub memory_mb: u32,
    pub disk_mb: u32,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct InstanceSpec {
    pub cluster_name: String,
    pub max_players: u16,
    pub resources: ResourceRequirements,
    pub world_preset: Option<String>,
    pub mod_manifest_id: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct Endpoint {
    pub host: String,
    pub game_port: u16,
    pub query_port: Option<u16>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct StartInstanceArgument {
    pub instance_id: InstanceId,
    pub build: GameBuild,
    pub spec: InstanceSpec,
    pub container_server_path: String,
    pub branch_name: String,
    pub port_mapping: Option<ContainerPortMapping>,
    /// 容器环境变量（端口注入：如 SDTD_SERVER_PORT=<宿主端口>，adapter 用于改写游戏端口）
    pub env: HashMap<String, String>,
    /// 实例配置（platform + player 合并后的键值），写入 /data/.platform/game-config.json
    /// 供容器内 config-render.sh 按 manifest 渲染游戏配置文件
    pub config: HashMap<String, String>,
    /// 外部受限凭证（M8：credential pool 分配，如 DST cluster_token），
    /// 逐个写入 /data/.platform/{key} 供容器内 hook 使用
    pub credentials: HashMap<String, String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum RuntimeState {
    Pending,
    BuildPrepared,
    Starting,
    Running,
    Stopping,
    Stopped,
    Failed,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum OperationKind {
    PrepareBuild,
    StartInstance,
    StopInstance,
    RestartInstance,
    CreateSnapshot,
    RestoreSnapshot,
    CleanInstance,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum OperationStatus {
    Pending,
    Running,
    Succeeded,
    Failed,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct FailureInfo {
    pub message: String,
    pub retryable: bool,
}

/// 结构化业务错误详情(对应 proto 的 nodeagent.v1.ErrorDetail)。
/// 数值字段与 error.proto 中的 BusinessErrorCode / ErrorCategory 保持一致。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct OperationError {
    pub code: i32,
    pub category: i32,
    pub message: String,
    pub retryable: bool,
    pub params: HashMap<String, String>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct BuildPreparation {
    pub build_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct BuildPreparationResult {
    pub build_root: String,
    pub prepared_at: DateTime<Utc>,
    pub build_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SnapshotCaptureRequest {
    pub instance_id: InstanceId,
    pub snapshot_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SnapshotRestoreRequest {
    pub instance_id: InstanceId,
    pub snapshot_id: String,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SnapshotRestoreResult {
    pub snapshot_id: String,
    pub restore_path: String,
    pub restored_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct InstanceRuntimeRecord {
    pub instance_id: InstanceId,
    pub node_id: NodeId,
    pub state: RuntimeState,
    pub endpoint: Option<Endpoint>,
    pub failure: Option<FailureInfo>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct NodeOperation {
    pub operation_id: OperationId,
    pub kind: OperationKind,
    pub status: OperationStatus,
    pub instance_id: Option<InstanceId>,
    pub build_id: Option<String>,
    pub message: Option<String>,
    pub started_at: DateTime<Utc>,
    pub finished_at: Option<DateTime<Utc>>,
    pub error: Option<OperationError>,
}

/// 实例数据路径（与 HOST_DATA_PATH 一致：`/data/game_instances/{id}`，勿用连字符）
pub fn instance_data_path(instance_id: &InstanceId) -> String {
    format!("/data/game_instances/{}", instance_id.0)
}