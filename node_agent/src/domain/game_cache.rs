use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

#[derive(Clone, PartialEq, Debug, Serialize, Deserialize)]
pub enum GameCacheStatus {
    Downloading,
    Available,
    Removed,
    Unavailable,
}

#[derive(Clone, PartialEq, Serialize, Deserialize)]
pub struct GameCache {
    pub game_id: String,
    pub branch_name: String,
    /// 游戏版本号（Steam 分支 buildid）。与 game_id+branch_name 共同决定是否需要下载/更新。
    /// 内容按 (game, branch, build_id) 落盘（P2-A：路径带 buildid，staging+原子切换）。
    /// #[serde(default)] 兼容旧数据库记录(无该字段时反序列化为空串)。
    #[serde(default)]
    pub build_id: String,
    pub status: GameCacheStatus,
    pub path: Option<String>,
    pub download_progress: Option<f32>,
    /// 引用计数：运行中实例挂载该版本时 +1，stop/clean 时 −1。
    /// 仅用于"旧目录延迟删除"与观测（不是版本保留）：非 current 且 refcount==0 才可回收。
    /// #[serde(default)] 兼容旧记录（无该字段时为 0）。
    #[serde(default)]
    pub refcount: i32,
    /// 缓存内容实测字节数（P2-B：下载完成后统计上报，供 controller 磁盘记账/调度）。
    /// 0 = 未知（未下载完成或统计失败）。
    #[serde(default)]
    pub size_bytes: u64,
    pub create_time: DateTime<Utc>,
    pub update_time: DateTime<Utc>,
}

/// 按 Steam buildid（数字字符串，单调递增）比较；非数字兜底按字典序。
/// 用于派生"current"（该分支 buildid 最大的 Available）与孤儿 GC。
pub fn buildid_cmp(a: &str, b: &str) -> std::cmp::Ordering {
    match (a.parse::<u64>(), b.parse::<u64>()) {
        (Ok(x), Ok(y)) => x.cmp(&y),
        _ => a.cmp(b),
    }
}
