use async_trait::async_trait;

use crate::error::AssetServiceError;
use crate::ports::SteamBranch;

/// Steam 分支信息仓库。
///
/// 存储从 steamcmd +app_info_print 解析出的分支列表。
/// `save_branches` 按 `game_id` 全量替换，同一个 `game_id` 下 `build_id` 不允许重复。
/// 输入列表中若出现同 `build_id` 的条目，实现方应取最后一条覆盖。
#[async_trait]
pub trait SteamBranchRepository: Send + Sync {
    /// 保存某个游戏的分支列表（全量替换，按 `build_id` 去重覆盖）。
    async fn save_branches(
        &self,
        game_id: &str,
        branches: &[SteamBranch],
    ) -> Result<(), AssetServiceError>;

    /// 查询某个游戏的所有分支。
    async fn get_branches(&self, game_id: &str) -> Result<Vec<SteamBranch>, AssetServiceError>;

    /// 查询某个游戏的指定分支。
    async fn get_branch(
        &self,
        game_id: &str,
        branch_name: &str,
    ) -> Result<Option<SteamBranch>, AssetServiceError>;
}
