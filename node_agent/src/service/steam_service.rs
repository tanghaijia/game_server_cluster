use std::process::ExitStatus;

use thiserror::Error;

use tonic::async_trait;

use crate::domain::GameCache;

#[derive(Debug, Error)]
pub enum SteamServiceError {
    #[error("IO error: {0}")]
    IoError(#[from] std::io::Error),
    /// tail：steamcmd 失败时的输出尾部上下文（stdout/stderr），供日志与 last_error 透传。
    /// 见 docs/game-cache-download-observability-design.md（P2）。
    #[error("Download Game {0} branch {1} error code {2}.")]
    DownloadError(String, String, ExitStatus, String),
    #[error("Repository operate error")]
    RepositoryOperateError(#[from] anyhow::Error),
    #[error("Empty Download Path Error")]
    EmptyDownloadPathError,
    /// P3 磁盘预检拒绝（下载前拦截必然失败的下载，见 docs/game-cache-download-observability-design.md）
    #[error("{0}")]
    PreflightRejected(String),
}

#[async_trait]
pub trait SteamService: Send + Sync {
    async fn start_download(
        &self,
        game_cache: GameCache,
    ) -> tokio::task::JoinHandle<Result<(), SteamServiceError>>;

    async fn uninstall(&self, game_cache: GameCache) -> Result<String, SteamServiceError>;

    async fn get_download_progress(
        &self,
        game_id: &String,
        branch_name: &String,
    ) -> anyhow::Result<Option<f32>>;
}
