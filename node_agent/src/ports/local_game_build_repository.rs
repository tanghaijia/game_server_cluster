use async_trait::async_trait;

use crate::domain::LocalGameBuild;
use crate::error::NodeAgentError;

/// 节点本地 GameBuild 仓库（持久化到表）。
///
/// 生产环境：SqliteLocalGameBuildRepository（SQLite 表）
/// 开发环境：InMemoryLocalGameBuildRepository（内存 HashMap）
#[async_trait]
pub trait LocalGameBuildRepository: Send + Sync {
    /// 保存一条本地 game_build（幂等：build_id 已存在则覆盖）。
    async fn save(&self, local_game_build: &LocalGameBuild) -> Result<(), NodeAgentError>;

    /// 按 build_id 查询本地 game_build。
    async fn get(&self, build_id: String) -> Result<LocalGameBuild, NodeAgentError>;
}
