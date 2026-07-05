use async_trait::async_trait;
use thiserror::Error;

use crate::domain::{
    ContainerFilePathMappingHost, ContainerPortMapping, ContainerResourceLimitation, GameContainer,
    Image, LocalGameBuild, RemoteImage,
};

#[async_trait]
pub trait ContainerClient: Send + Sync {
    async fn pull_image(&self, image: &RemoteImage) -> anyhow::Result<Image>;

    async fn check_image(&self, image: &RemoteImage) -> anyhow::Result<bool>;

    async fn last_version(&self, image: &RemoteImage) -> anyhow::Result<String>;

    async fn get_container(&self, id: String) -> Result<GameContainer, ContainerError>;

    async fn create_container(
        &self,
        game_build: LocalGameBuild,
        path_mapping: Option<ContainerFilePathMappingHost>,
        port_mapping: Option<ContainerPortMapping>,
        resource_limitation: Option<ContainerResourceLimitation>,
    ) -> Result<GameContainer, ContainerError>;
}

#[derive(Error, Debug)]
pub enum ContainerError {
    #[error("容器未找到 (ID: {0})")]
    NotFound(u64),

    #[error("节点资源不足")]
    InsufficientResources,

    // #[from] 会自动生成 From<std::io::Error> 实现，允许你用 `?` 操作符自动转换错误
    #[error("底层 I/O 错误: {source}")]
    IOError {
        #[from]
        source: std::io::Error,
    },

    #[error("未知的严重错误")]
    Unknown,
}
