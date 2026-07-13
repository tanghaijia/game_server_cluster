use async_trait::async_trait;

use crate::domain::Game;
use crate::error::AssetServiceError;

/// 游戏注册信息仓库。
#[async_trait]
pub trait GameRepository: Send + Sync {
    /// 注册或更新游戏信息。
    async fn save(&self, game: &Game) -> Result<(), AssetServiceError>;

    /// 按游戏 ID 查询。
    async fn get(&self, game_id: &str) -> Result<Option<Game>, AssetServiceError>;

    /// 查询所有游戏。
    async fn list(&self) -> Result<Vec<Game>, AssetServiceError>;

    /// 删除游戏。
    async fn delete(&self, game_id: &str) -> Result<(), AssetServiceError>;
}
