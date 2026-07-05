use crate::domain::game::Game;
use crate::domain::image::Image;
use anyhow::anyhow;
use serde::{Deserialize, Serialize};
use std::collections::HashSet;
use std::sync::Mutex;

/**
* 运行游戏的'运行环境容器'镜像
**/
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct GameBuild {
    pub build_id: String,
    pub game: Game,
    pub channel: Option<String>,
    pub adapter_version: Option<String>,
}
/**
* 本地的被维护的GameBuild
**/
#[derive(Debug, Clone, Hash, Eq, PartialEq)]
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
        todo!()
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
