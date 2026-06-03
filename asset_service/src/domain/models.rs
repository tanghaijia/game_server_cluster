use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::{AdapterId, AdapterVersion, BuildId, ModManifestId, SnapshotId};

/// 指定要解析的游戏版本。
///
/// 两种定位方式：
/// - `Channel`: 按发布通道（如 `"stable"`, `"beta"`）查找该通道下最新的可用版本
/// - `BuildId`: 直接用构建 ID 精确查找
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum VersionSelector {
    Channel { channel: String },
    BuildId { build_id: String },
}

/// 游戏构建的生命周期状态。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum BuildStatus {
    /// 上游发现了新版本，但尚未开始解析
    Discovered,
    /// 正在解析中（下载、执行适配器脚本、计算校验和）
    Resolving,
    /// 解析完成，可用于部署实例
    Available,
    /// 已被更新版本取代，但已部署的实例仍可使用
    Deprecated,
    /// 不再可用（如上游删除了该版本）
    Unavailable,
    /// 管理层面标记为已删除，等待回收
    Deleted,
}

/// 一个确定版本的游戏服务器构建。
///
/// 生命周期：上游发布新版本 → adapter 脚本拉取并标准化 → 注册为 `GameBuild` → controller 用它部署实例
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct GameBuild {
    /// 构建的唯一标识
    pub build_id: BuildId,
    /// 所属游戏类型
    pub game_id: String,
    /// 发布通道，如 `"stable"`, `"beta"`，用于按通道解析最新版本
    pub channel: Option<String>,
    /// 构建所用适配器的标识，如 `"dst"`、`"minecraft"`
    pub adapter_id: AdapterId,
    /// 构建所用适配器的版本号
    pub adapter_version: AdapterVersion,
    /// 上游平台的原始版本号，如 Steam 的 build id、Mojang 的版本名
    pub upstream_version: Option<String>,
    /// 构建产物的下载地址（S3 / Docker Registry URI 等）
    pub artifact_uri: Option<String>,
    /// 构建产物的校验和，用于下载后完整性校验
    pub checksum: Option<String>,
    /// 当前状态
    pub status: BuildStatus,
    /// 是否钉选。钉选的构建不会被自动清理回收
    pub pinned: bool,
    /// 解析完成的时间
    pub resolved_at: DateTime<Utc>,
    /// 首次注册到系统的时间
    pub created_at: DateTime<Utc>,
    /// 最后更新时间
    pub updated_at: DateTime<Utc>,
}

/// 快照触发来源。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum SnapshotType {
    /// 用户手动创建的备份
    Manual,
    /// 定时任务自动创建的备份
    Scheduled,
    /// 升级前自动创建的备份（用于回滚）
    PreUpgrade,
    /// 实例最终停止前的备份
    FinalStop,
}

/// 快照生命周期的状态。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum SnapshotStatus {
    /// 已创建记录，等待执行
    Pending,
    /// 正在执行快照（复制实例数据）
    Running,
    /// 正在上传到远程存储
    Uploading,
    /// 上传完成，可以用于恢复
    Completed,
    /// 执行失败
    Failed,
    /// 已过期而被自动清除
    Expired,
}

/// 一次实例数据备份的记录。
///
/// 代表将某个游戏实例的数据目录完整备份到远程存储（如 S3）的一次操作。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SnapshotRecord {
    /// 快照唯一标识
    pub snapshot_id: SnapshotId,
    /// 被备份的实例 ID
    pub instance_id: String,
    /// 实例当时的游戏构建版本
    pub build_id: Option<BuildId>,
    /// 快照触发来源
    pub snapshot_type: SnapshotType,
    /// 实例数据在节点上的路径
    pub instance_data_path: String,
    /// 备份文件在远程存储的地址
    pub storage_uri: Option<String>,
    /// 备份文件的清单 URI
    pub manifest_uri: Option<String>,
    /// 备份文件的校验和
    pub checksum: Option<String>,
    /// 当前状态
    pub status: SnapshotStatus,
    /// 执行快照的物理节点
    pub source_node: Option<String>,
    /// 创建记录的时间
    pub created_at: DateTime<Utc>,
    /// 完成/失败的时间
    pub completed_at: Option<DateTime<Utc>>,
    /// 失败原因（仅 status == Failed 时有值）
    pub failure_message: Option<String>,
}

/// Mod 清单中的单个模组条目。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ModEntry {
    /// 模组的唯一标识
    pub mod_id: String,
    /// 模组的版本号
    pub version: String,
    /// 是否为必需模组（false 表示可选）
    pub required: bool,
}

/// 一份模组配置清单。
///
/// 描述一个游戏实例需要加载哪些模组及其版本。每个清单计算一个 `config_hash`，
/// 相同的清单产生相同的 hash，用于去重和快速查找。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ModManifest {
    /// 清单唯一标识
    pub manifest_id: ModManifestId,
    /// 适用的游戏 ID
    pub game_id: String,
    /// 模组列表
    pub mods: Vec<ModEntry>,
    /// 模组配置的哈希值，用于判断两份配置是否等价
    pub config_hash: String,
    /// 兼容性说明（如已知兼容性问题）
    pub compatibility_note: Option<String>,
    /// 创建时间
    pub created_at: DateTime<Utc>,
}

/// 构建与模组清单的兼容性检查结果。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct BuildCompatibility {
    /// 是否兼容
    pub compatible: bool,
    /// 不兼容时的原因说明
    pub reason: Option<String>,
}

/// 从快照恢复实例所需的完整信息。
///
/// `asset_service` 返回此计划给 `node_agent`，`node_agent` 据此下载备份文件并恢复到指定路径。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SnapshotRestorePlan {
    /// 要恢复的快照 ID
    pub snapshot_id: SnapshotId,
    /// 快照时的游戏构建版本（如有）
    pub build_id: Option<BuildId>,
    /// 备份文件在远程存储的下载地址
    pub storage_uri: String,
    /// 备份文件清单 URI
    pub manifest_uri: Option<String>,
    /// 备份文件的校验和
    pub checksum: Option<String>,
    /// 恢复到节点的目标路径
    pub instance_data_path: String,
}

/// 计算实例在节点上的数据目录路径。
pub fn instance_data_path(instance_id: &str) -> String {
    format!("/data/game-instances/{instance_id}")
}
