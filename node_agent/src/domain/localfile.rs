use crate::domain::game::Game;

/**
* 二进制可执行程序文件
**/
pub struct ExecutableBinaryFile {
    pub path: String,
}

/**
* 游戏二进制可执行程序文件
**/
pub struct ExecutableBinaryGameFile {
    pub game: Game,
    pub exe_file: ExecutableBinaryFile,
}

/**
* 本地磁盘上的游戏相关文件的聚合体
**/
pub struct LocalGameFileAggregate {
    pub executable_binary_game_file: ExecutableBinaryGameFile,
    pub root_path: String, // 游戏聚合体的根路径
}

/**
* 游戏容器运行所依赖的环境
**/
pub struct LocalGameEnv {

}

/**
* 本地已缓存的游戏聚合体集合
**/
pub struct LocalCachedGameAggregateSet {

}