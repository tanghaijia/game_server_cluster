use std::println;
use std::sync::Arc;

use anyhow::anyhow;
use regex::Regex;
use tokio::io::{AsyncBufReadExt, BufReader};
use tokio::process::Command;
use tonic::async_trait;

use crate::domain::{GameCache, GameCacheStatus};
use crate::ports::GameCacheRepository;
use crate::service::{SteamService, SteamServiceError};

pub struct SteamServiceClient {
    game_cache_repos: Arc<dyn GameCacheRepository>,
}

impl SteamServiceClient {
    pub fn new(game_cache_repos: Arc<dyn GameCacheRepository>) -> Self {
        Self { game_cache_repos }
    }

    async fn download(&self, mut game_cache: GameCache) -> Result<(), SteamServiceError> {
        // path 为空是调用方错误，不需写 DB
        if game_cache.path.is_none() {
            return Err(SteamServiceError::EmptyDownloadPathError {});
        }

        let result = self.run_download(&mut game_cache).await;

        match result {
            Ok(()) => {
                game_cache.status = GameCacheStatus::Available;
                self.game_cache_repos.save(&game_cache).await?;
                Ok(())
            }
            Err(e) => {
                game_cache.status = GameCacheStatus::Unavailable;
                let _ = self.game_cache_repos.save(&game_cache).await;
                Err(e)
            }
        }
    }

    /// 实际的 steamcmd 下载流程，所有错误原样上抛给 download 统一处理。
    async fn run_download(&self, game_cache: &mut GameCache) -> Result<(), SteamServiceError> {
        let mut child = Command::new("steamcmd")
            .args([
                "+login",
                "anonymous",
                "+force_install_dir",
                game_cache.path.clone().unwrap().as_str(),
                "+app_update",
                game_cache.game_id.as_str(),
                "-beta",
                game_cache.branch_name.as_str(),
                "validate",
                "+quit",
            ])
            .stdout(std::process::Stdio::piped())
            .spawn()?;

        let stdout = child.stdout.take().unwrap();

        let mut lines = BufReader::new(stdout).lines();

        game_cache.status = GameCacheStatus::Downloading;
        self.game_cache_repos.save(game_cache).await?;
        while let Some(line) = lines.next_line().await? {
            if let Ok(Some(progress)) = progress_regex(&line) {
                game_cache.download_progress = Some(progress);
                self.game_cache_repos.save(game_cache).await?;
            }
        }

        let status = child.wait().await?;

        if !status.success() {
            return Err(SteamServiceError::DownloadError(
                game_cache.game_id.clone(),
                game_cache.branch_name.clone(),
                status,
            ));
        }

        Ok(())
    }

    /// 实际的 steamcmd 卸载流程，所有错误原样上抛。
    async fn run_uninstall(&self, game_cache: &GameCache) -> Result<String, SteamServiceError> {
        let path = game_cache
            .path
            .clone()
            .ok_or(SteamServiceError::EmptyDownloadPathError)?;

        let mut child = Command::new("steamcmd")
            .args([
                "+login",
                "anonymous",
                "+force_install_dir",
                game_cache.path.clone().unwrap().as_str(),
                "+app_uninstall",
                game_cache.game_id.as_str(),
                "+quit",
            ])
            .stdout(std::process::Stdio::piped())
            .spawn()?;

        let stdout = child.stdout.take().unwrap();
        let mut lines = BufReader::new(stdout).lines();
        log::info!("开始卸载：");
        while let Some(line) = lines.next_line().await? {
            log::info!("{}", line);
        }

        let status = child.wait().await?;

        if !status.success() {
            return Err(SteamServiceError::DownloadError(
                game_cache.game_id.clone(),
                game_cache.branch_name.clone(),
                status,
            ));
        }

        Ok(path)
    }
}

#[async_trait]
impl SteamService for SteamServiceClient {
    async fn start_download(
        &self,
        game_cache: GameCache,
    ) -> tokio::task::JoinHandle<Result<(), SteamServiceError>> {
        let repos = self.game_cache_repos.clone();
        tokio::spawn(async move {
            let client = SteamServiceClient {
                game_cache_repos: repos,
            };
            client.download(game_cache).await
        })
    }

    async fn uninstall(&self, mut game_cache: GameCache) -> Result<String, SteamServiceError> {
        let result = self.run_uninstall(&game_cache).await;

        match &result {
            Ok(_) => {
                game_cache.status = GameCacheStatus::Removed;
                let _ = self.game_cache_repos.save(&game_cache).await;
            }
            Err(_) => {
                game_cache.status = GameCacheStatus::Unavailable;
                let _ = self.game_cache_repos.save(&game_cache).await;
            }
        }

        result
    }

    async fn get_download_progress(
        &self,
        game_id: &String,
        branch_name: &String,
    ) -> anyhow::Result<Option<f32>> {
        let game_cache = self.game_cache_repos.get(game_id, branch_name).await?;
        if game_cache.is_none() {
            return Ok(None);
        }

        let game_cache = game_cache.unwrap();
        match game_cache.status {
            GameCacheStatus::Available => Ok(Some(100.0)),
            GameCacheStatus::Removed => Ok(None),
            GameCacheStatus::Unavailable => Err(anyhow!("Game不可获取")),
            GameCacheStatus::Downloading => Ok(game_cache.download_progress),
        }
    }
}

pub fn progress_regex(line: &str) -> anyhow::Result<Option<f32>> {
    let progress_regex = Regex::new(r"progress:\s+(\d+\.\d+)")?;
    if let Some(caps) = progress_regex.captures(line) {
        let progress: f32 = caps[1].parse()?;
        Ok(Some(progress))
    } else {
        Ok(None)
    }
}
