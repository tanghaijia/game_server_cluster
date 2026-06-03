use async_trait::async_trait;

use crate::error::AssetServiceError;
use crate::ports::SteamBranch;

/// Steam 分支信息仓库。
///
/// 存储从 steamcmd +app_info_print 解析出的分支列表。
/// `save_branches` 为全量替换——每次 steamcmd 输出都是该游戏的完整分支列表。
#[async_trait]
pub trait SteamBranchRepository: Send + Sync {
    /// 保存某个游戏的分支列表（全量替换）。
    async fn save_branches(
        &self,
        game_id: &str,
        branches: &[SteamBranch],
    ) -> Result<(), AssetServiceError>;

    /// 查询某个游戏的所有分支。
    async fn get_branches(
        &self,
        game_id: &str,
    ) -> Result<Vec<SteamBranch>, AssetServiceError>;

    /// 查询某个游戏的指定分支。
    async fn get_branch(
        &self,
        game_id: &str,
        branch_name: &str,
    ) -> Result<Option<SteamBranch>, AssetServiceError>;
}
