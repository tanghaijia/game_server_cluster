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

    /// 原子 get-or-create:key(game_id:branch_name)不存在则插入并返回 true;已存在则返回 false(不覆盖)。
    /// 用于 CacheGame 接口幂等:并发下仅有一个调用方返回 true 并负责启动下载。
    async fn insert_if_absent(&self, game_cache: &GameCache) -> anyhow::Result<bool>;

    /// 删除缓存记录(key=game_id:branch_name)。缓存不存在时视为成功(幂等)。
    /// 用于 RemoveCache:删除磁盘目录后清理记录,释放磁盘。
    async fn delete(&self, game_id: &String, branch_name: &String) -> anyhow::Result<()>;
}
