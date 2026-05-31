use async_trait::async_trait;

use crate::{
    domain::{BuildId, GameBuild, GameKind},
    error::AssetServiceError,
};

#[async_trait]
pub trait BuildRepository: Send + Sync {
    async fn save(&self, build: &GameBuild) -> Result<(), AssetServiceError>;

    async fn get(&self, build_id: &BuildId) -> Result<Option<GameBuild>, AssetServiceError>;

    async fn list_by_game(&self, game: &GameKind) -> Result<Vec<GameBuild>, AssetServiceError>;
}
