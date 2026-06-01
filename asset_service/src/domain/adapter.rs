use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

use super::{AdapterId, GameKind};

/// 适配器语义化版本号。
///
/// 每个适配器独立版本化，与游戏上游版本（`upstream_version`）解耦。
/// 两者通过 `GameBuild` 关联，镜像产物 tag 的形式为：
/// `{adapter_id}:{adapter_version}-{upstream_version}`，
/// 如 `dst:0.1.0-500000`。
#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
pub struct AdapterVersion {
    /// 不兼容的接口变更
    pub major: u32,
    /// 向后兼容的新功能
    pub minor: u32,
    /// 向后兼容的 bug 修复
    pub patch: u32,
}

impl AdapterVersion {
    pub fn new(major: u32, minor: u32, patch: u32) -> Self {
        Self { major, minor, patch }
    }
}

impl std::fmt::Display for AdapterVersion {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}.{}.{}", self.major, self.minor, self.patch)
    }
}

#[derive(Debug, Clone)]
pub struct InvalidAdapterVersion(pub String);

impl std::fmt::Display for InvalidAdapterVersion {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "invalid adapter version: {}", self.0)
    }
}

impl std::error::Error for InvalidAdapterVersion {}

impl std::str::FromStr for AdapterVersion {
    type Err = InvalidAdapterVersion;

    fn from_str(s: &str) -> Result<Self, Self::Err> {
        let parts: Vec<&str> = s.splitn(3, '.').collect();
        if parts.len() != 3 {
            return Err(InvalidAdapterVersion(s.to_string()));
        }
        let major = parts[0].parse().map_err(|_| InvalidAdapterVersion(s.to_string()))?;
        let minor = parts[1].parse().map_err(|_| InvalidAdapterVersion(s.to_string()))?;
        let patch = parts[2].parse().map_err(|_| InvalidAdapterVersion(s.to_string()))?;
        Ok(Self { major, minor, patch })
    }
}

/// 适配器版本状态。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum AdapterStatus {
    /// 活跃可用，推荐用于新构建
    Active,
    /// 已废弃，不应用于新构建，但已有产物仍然有效
    Deprecated,
    /// 已退役，使用该版本的所有 `GameBuild` 应标记为 Unavailable
    Retired,
}

/// 一个已注册的游戏适配器。
///
/// 适配器是**构建工具**——它负责拉取上游游戏文件、注入生命周期脚本和配置模板、
/// 构建 Docker 镜像并作为 `GameBuild` 注册到系统。
///
/// 适配器本身不在运行时部署。运行中的实例只需要最终的 Docker 镜像，
/// 不需要感知适配器。`GameBuild.adapter_version` 记录该构建使用的是
/// 哪个适配器版本，用于溯源和兼容性判断。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct GameAdapter {
    /// 适配器标识，对应适配器代码仓库名称，如 `"dst"`、`"minecraft"`
    pub adapter_id: AdapterId,
    /// 适配的游戏类型
    pub game: GameKind,
    /// 适配器展示名称
    pub name: String,
    /// 适配器描述
    pub description: Option<String>,
    /// 适配器代码仓库地址
    pub source_repository: Option<String>,
    /// 所有已注册的版本（按版本号降序）
    pub versions: Vec<AdapterVersionEntry>,
    /// 创建时间
    pub created_at: DateTime<Utc>,
    /// 最后更新时间
    pub updated_at: DateTime<Utc>,
}

impl GameAdapter {
    /// 返回当前推荐使用的版本（状态为 Active 的最高版本）。
    pub fn current_version(&self) -> Option<&AdapterVersionEntry> {
        self.versions
            .iter()
            .filter(|v| v.status == AdapterStatus::Active)
            .max_by_key(|v| &v.version)
    }

    /// 检查指定版本是否可用（存在且未退役）。
    pub fn is_version_usable(&self, version: &AdapterVersion) -> bool {
        self.versions
            .iter()
            .any(|v| &v.version == version && v.status != AdapterStatus::Retired)
    }
}

/// 适配器单个版本的记录。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct AdapterVersionEntry {
    /// 版本号
    pub version: AdapterVersion,
    /// 版本状态
    pub status: AdapterStatus,
    /// 变更说明
    pub changelog: Option<String>,
    /// 最低适配器规范版本（生命周期脚本契约的版本号）
    pub min_spec_version: u32,
    /// 注册时间
    pub registered_at: DateTime<Utc>,
}
