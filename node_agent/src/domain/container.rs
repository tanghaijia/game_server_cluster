use serde::{Deserialize, Serialize};

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

#[derive(Clone, Serialize, Deserialize)]
pub enum ConatinerType {
    DockerContainer,
}

#[derive(Clone, Serialize, Deserialize)]
pub enum ContainerStatus {
    Created,
    Running,
    Paused,
    Eestarting,
    Exited,
    Dead,
    Removing,
}

/**
* 游戏容器
**/
#[derive(Clone, Serialize, Deserialize)]
pub struct GameContainer {
    pub id: String,
    pub game_build: LocalGameBuild,
    pub container: ConatinerType,
    pub container_file_path_mapping: Option<ContainerFilePathMappingHost>,
    pub container_port_mapping: Option<ContainerPortMapping>,
    pub resource_limitation: Option<ContainerResourceLimitation>,
    pub status: ContainerStatus,
}

/**
* 宿主机的文件路径
**/
#[derive(Clone, Serialize, Deserialize)]
pub struct HostFilePath {
    pub path: String,
}

/**
* 游戏容器的文件路径
**/
#[derive(Clone, Serialize, Deserialize)]
pub struct ContainerFilePath {
    pub path: String,
}

/**
* 一个宿主机和容器的路径映射
**/
#[derive(Clone, Serialize, Deserialize)]
pub struct ContainerFilePathMappingHost {
    pub host_path: HostFilePath,
    pub container_file_path: ContainerFilePath,
    pub mapped_permission: String,
}

/**
* 容器内一个游戏的根路径
**/
pub struct MappedGameContainerRootFilePath {}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum ContainerPortMappingMod {
    HOST,
    NAT,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum MappingPortType {
    UDP,
    TCP,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PortMap {
    pub host_port: u16,
    pub container_port: u16,
    pub mapping_port_type: MappingPortType,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct ContainerPortMapping {
    pub container_port_mapping_mod: ContainerPortMappingMod,
    pub port_maps: Vec<PortMap>,
}

#[derive(Clone, Serialize, Deserialize)]
pub struct ContainerResourceLimitation {}
