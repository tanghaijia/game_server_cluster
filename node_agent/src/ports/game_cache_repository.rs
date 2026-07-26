use tonic::async_trait;

use crate::domain::GameCache;

#[async_trait]
pub trait GameCacheRepository: Send + Sync {
    async fn save(&self, game_cache: &GameCache) -> anyhow::Result<()>;

    async fn get(
        &self,
        game_id: &String,
        branch_name: &String,
    ) -> anyhow::Result<Option<GameCache>>;

    async fn get_all(&self) -> anyhow::Result<Vec<GameCache>>;
}
