
/**
* 容器镜像
**/
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct Image {
    pub id: String,
    pub name: String,
    pub version: String,
    pub path: String,
    pub size: i64,
    pub created_at: i64,
    pub updated_at: i64,
    pub status: ImageStatus
}

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub enum ImageStatus {
    Runnable,
    Stopped
}

/**
* 外部容器镜像
**/
pub struct RemoteImage {
    pub id: String,
    pub name: String,
    pub version: String,
}
