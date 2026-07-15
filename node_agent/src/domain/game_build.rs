use crate::domain::game::{self, Game};
use crate::domain::image::Image;
use anyhow::anyhow;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::collections::HashSet;
use std::sync::Mutex;

/// 游戏构建的生命周期状态。
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum BuildStatus {
    /// 上游发现了新版本，但尚未开始解析
    Discovered,
    /// 正在解析中（下载、执行适配器脚本、计算校验和）
    Resolving,
    /// 解析完成，可用于部署实例
    Available,
    /// 已被更新版本取代，但已部署的实例仍可使用
    Deprecated,
    /// 不再可用（如上游删除了该版本）
    Unavailable,
    /// 管理层面标记为已删除，等待回收
    Deleted,
}

/**
* 运行游戏的'运行环境容器'镜像
**/
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct GameBuild {
    pub build_id: String,
    pub game: Game,
    pub channel: Option<String>,
    pub adapter_version: Option<String>,
    pub artifact_uri: Option<String>,
    /// 构建产物的校验和，用于下载后完整性校验
    pub artifact_image_name: Option<String>,
    pub artifact_image_tag: Option<String>,
}

/**
* 本地的被维护的GameBuild
**/
#[derive(Debug, Clone, Hash, Eq, PartialEq, Serialize, Deserialize)]
pub struct LocalGameBuild {
    pub build_id: String,
    pub game: Game,
    pub image: Image,
}

/**
* 节点本地的GameBuild集合
**/
pub struct LocalGameBuildSet {
    pub set: HashSet<LocalGameBuild>,
}

/**
* 本地GameBuild管理器
**/
pub struct LocalGameBuildManager {
    set: Mutex<Vec<LocalGameBuild>>,
}

impl LocalGameBuildManager {
    /**
     * 记录一条game_build记录，代表game_build已在本地保存。如果game_build已存在，则失败。
     **/
    pub fn record_game_build_from_image(
        &self,
        game_build: &GameBuild,
        image: &Image,
    ) -> anyhow::Result<LocalGameBuild> {
        let mut set = self.set.lock().map_err(|e| anyhow!("锁被污染: {e}"))?;

        for local_game_build in &*set {
            if local_game_build.build_id == game_build.build_id {
                return Err(anyhow!("已存在game_build"));
            }
        }

        let local_game_build = LocalGameBuild {
            build_id: game_build.build_id.to_string(),
            game: game_build.game.clone(),
            image: image.clone(),
        };
        set.push(local_game_build.clone());

        Ok(local_game_build)
    }

    pub async fn get(&self, build_id: String) -> anyhow::Result<LocalGameBuild> {
        let set = self.set.lock().map_err(|e| anyhow!("锁被污染: {e}"))?;

        for local_game_build in &*set {
            if local_game_build.build_id == build_id {
                return Ok((*local_game_build).clone());
            }
        }

        return Err(anyhow!("没找到game_build, build id: {}", build_id));
    }

    pub fn new() -> Self {
        Self {
            set: Mutex::new(Vec::new()),
        }
    }
}

pub fn map_game_build_to_local_game_build(game_build: GameBuild) -> LocalGameBuild {
    todo!()
}
