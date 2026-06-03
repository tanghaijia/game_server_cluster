use std::sync::Arc;
use std::time::Duration;

use tokio;

use crate::{
    domain::Game,
    ports::{SteamBranch, SteamBranchRepository, SteamService},
};

/// Steam 分支定期同步器。
///
/// 每隔 `interval` 遍历所有已注册 game，调用 SteamService 获取最新分支列表，
/// 按 `build_id` 去重后写入 SteamBranchRepository。
pub struct SteamBranchSync {
    steam_service: Arc<dyn SteamService>,
    branch_repo: Arc<dyn SteamBranchRepository>,
    game_repo: Arc<dyn crate::ports::GameRepository>,
    interval: Duration,
}

impl SteamBranchSync {
    pub fn new(
        steam_service: Arc<dyn SteamService>,
        branch_repo: Arc<dyn SteamBranchRepository>,
        game_repo: Arc<dyn crate::ports::GameRepository>,
        interval: Duration,
    ) -> Self {
        Self {
            steam_service,
            branch_repo,
            game_repo,
            interval,
        }
    }

    /// 启动后台循环，通过 `tokio::spawn` 在独立任务中运行。
    pub async fn run(&self) {
        loop {
            tokio::time::sleep(self.interval).await;
            self.sync_all_games().await;
        }
    }

    async fn sync_all_games(&self) {
        let games = match self.game_repo.list().await {
            Ok(games) => games,
            Err(e) => {
                eprintln!("[steam-branch-sync] failed to list games: {e}");
                return;
            }
        };

        for game in &games {
            if game.app_id.is_empty() {
                continue;
            }
            self.sync_game(game).await;
        }
    }

    async fn sync_game(&self, game: &Game) {
        let branches = match self
            .steam_service
            .get_steam_branchs(&game.app_id)
            .await
        {
            Ok(branches) => branches,
            Err(e) => {
                eprintln!(
                    "[steam-branch-sync] failed to fetch branches for {} (app_id={}): {e}",
                    game.id, game.app_id
                );
                return;
            }
        };

        if let Err(e) = self.branch_repo.save_branches(&game.app_id, &branches).await {
            eprintln!(
                "[steam-branch-sync] failed to save branches for {} (app_id={}): {e}",
                game.id, game.app_id
            );
        }
    }
}
