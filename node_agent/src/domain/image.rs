use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};

/**
* 容器镜像
**/
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct Image {
    pub id: String,
    pub name: String,
    pub tag: String,
    pub size: Option<i64>,
    pub created_at: DateTime<Utc>,
    pub status: ImageStatus
}

#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
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
    pub tag: String,
}

/**
* 外部镜像仓库
**/
pub struct ImageRepository {
    pub id: String,
    pub address: String,
    pub port: i64,
    pub image_repository_credentials: ImageRepositoryCredentials
}

/**
* 镜像仓库认证信息
**/
#[derive(Clone)]
pub struct ImageRepositoryCredentials {
    pub username: Option<String>,
    pub password: Option<String>,
    pub serveraddress: Option<String>,
    pub identitytoken: Option<String>,
    pub auth: Option<String>,
    pub email: Option<String>,
    pub registrytoken: Option<String>,
}
