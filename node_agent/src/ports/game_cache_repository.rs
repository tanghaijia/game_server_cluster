use tonic::async_trait;

use crate::domain::GameCache;

/// 游戏缓存仓库（P2-A 版本化）。
///
/// 每条记录 = 一个 (game_id, branch_name, build_id) 版本，key = `game:branch:buildid`。
/// 一个分支可同时存在多个版本记录（current + 被运行实例引用的旧版本孤儿）。
/// "current"（对外服务的版本）由实现按规则派生：
///   优先 buildid 最大的 Available；否则 buildid 最大的 Downloading；否则任意最高版本
/// （buildid 为 Steam 单调递增数字，同分支内可直接比较）。
#[async_trait]
pub trait GameCacheRepository: Send + Sync {
    /// 保存/覆盖一个版本记录（key = game:branch:buildid）
    async fn save(&self, game_cache: &GameCache) -> anyhow::Result<()>;

    /// 派生 current：见 trait 头注释。无任何版本记录时返回 None。
    async fn get(
        &self,
        game_id: &String,
        branch_name: &String,
    ) -> anyhow::Result<Option<GameCache>>;

    /// 指定版本记录
    async fn get_version(
        &self,
        game_id: &String,
        branch_name: &String,
        build_id: &String,
    ) -> anyhow::Result<Option<GameCache>>;

    /// 某分支的全部版本记录（current + 孤儿），供 GC/删除/观测
    async fn get_versions(
        &self,
        game_id: &String,
        branch_name: &String,
    ) -> anyhow::Result<Vec<GameCache>>;

    /// 全部版本记录（program_init 等全量场景）
    async fn get_all(&self) -> anyhow::Result<Vec<GameCache>>;

    /// 原子 get-or-create:key(game:branch:buildid)不存在则插入并返回 true;已存在则返回 false(不覆盖)。
    /// 用于 CacheGame 接口幂等:同一版本并发下仅有一个调用方返回 true 并负责启动下载。
    async fn insert_if_absent(&self, game_cache: &GameCache) -> anyhow::Result<bool>;

    /// 删除某分支的全部版本记录（key 前缀 game:branch:）。缓存不存在时视为成功(幂等)。
    /// 用于 RemoveCache:删除磁盘目录后清理记录,释放磁盘。
    async fn delete(&self, game_id: &String, branch_name: &String) -> anyhow::Result<()>;

    /// 删除单个版本记录（key = game:branch:buildid）。用于孤儿版本 GC。
    async fn delete_version(
        &self,
        game_id: &String,
        branch_name: &String,
        build_id: &String,
    ) -> anyhow::Result<()>;
}
