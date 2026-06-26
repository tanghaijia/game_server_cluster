use std::collections::HashSet;
use serde::{Deserialize, Serialize};
use crate::domain::game::Game;
use crate::domain::image::Image;

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
* 本地的GameBuild
**/
pub struct LocalGameBuild {
    pub build_id: String,
    pub game: Game,
    pub image: Image
}

/**
* 节点本地的GameBuild集合
**/
pub struct LocalGameBuildSet {
    pub set: HashSet<LocalGameBuild>
}

