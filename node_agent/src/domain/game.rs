use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct Game {
    pub id: String,
    pub name: String,
    pub app_id: String, // steam的app_id
}


/**
* 游戏所需要的资源
**/
pub struct GameRequestResource {

}

/**
* 游戏版本
**/
pub struct GameVersion {

}