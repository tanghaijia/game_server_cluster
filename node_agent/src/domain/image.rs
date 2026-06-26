
/**
* 容器镜像
**/
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

pub enum ImageStatus {
    Runnable,
    Stopped
}