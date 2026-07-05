use crate::domain::{LocalGameBuild, game::Game};

/**
* Docker容器
**/
pub struct DockerContainer {
    pub base: ContainerBase,
}

impl Container for DockerContainer {
    fn base(&self) -> &ContainerBase {
        &self.base
    }
}

pub struct ContainerBase {
    pub id: String,
}

/**
* 容器
**/
pub trait Container {
    fn base(&self) -> &ContainerBase;
}

pub enum ConatinerType {
    DockerContainer,
}

/**
* 游戏容器
**/
pub struct GameContainer {
    pub id: String,
    pub game_build: LocalGameBuild,
    pub container: ConatinerType,
    pub container_file_path_mapping: Option<ContainerFilePathMappingHost>,
    pub container_port_mapping: Option<ContainerPortMapping>,
    pub resource_limitation: Option<ContainerResourceLimitation>,
}

/**
* 宿主机的文件路径
**/
pub struct HostFilePath {
    pub path: String,
}

/**
* 游戏容器的文件路径
**/
pub struct ContainerFilePath {
    pub path: String,
}

/**
* 一个宿主机和容器的路径映射
**/
pub struct ContainerFilePathMappingHost {
    pub host_path: HostFilePath,
    pub container_file_path: ContainerFilePath,
    pub mapped_permission: String,
}

/**
* 容器内一个游戏的根路径
**/
pub struct MappedGameContainerRootFilePath {}

pub struct ContainerPortMapping {}

pub struct ContainerResourceLimitation {}
