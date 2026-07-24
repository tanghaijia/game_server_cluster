use std::process::ExitStatus;

use thiserror::Error;

use tonic::async_trait;

use crate::domain::GameCache;

#[derive(Debug, Error)]
pub enum SteamServiceError {
    #[error("IO error: {0}")]
    IoError(#[from] std::io::Error),
    #[error("Download Game {0} branch {1} error code {2}.")]
    DownloadError(String, String, ExitStatus),
    #[error("Repository operate error")]
    RepositoryOperateError(#[from] anyhow::Error),
    #[error("Empty Download Path Error")]
    EmptyDownloadPathError,
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
