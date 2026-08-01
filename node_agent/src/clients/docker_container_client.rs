use std::sync::Arc;

use crate::domain::{
    ConatinerType, ContainerStatus, GameContainer, Image, ImageRepository,
    ImageRepositoryCredentials, ImageStatus, LocalGameBuild, MappingPortType, RemoteImage,
};
use crate::ports::{ContainerClient, ContainerError, DockerInstanceRepository};
use async_trait::async_trait;
use chrono::Utc;
use lmrc_docker::{DockerClient, DockerCredentials};

pub struct DockerContainerClient {
    image_repository: ImageRepository,
    docker_instances: Arc<dyn DockerInstanceRepository>,
}

impl DockerContainerClient {
    pub fn new(
        image_repository: ImageRepository,
        docker_instances: Arc<dyn DockerInstanceRepository>,
    ) -> Self {
        DockerContainerClient {
            image_repository,
            docker_instances,
        }
    }
}

#[async_trait]
impl ContainerClient for DockerContainerClient {
    async fn pull_image(&self, remote_image: &RemoteImage) -> anyhow::Result<Image> {
        let client = DockerClient::new()?;

        let id = Self::map_image_full_name(
            &self.image_repository.address,
            remote_image.name.as_str(),
            remote_image.tag.as_str(),
        );

        // 如果本地已存在，跳过远端 pull
        if client.registry().image_exists_locally(&id).await? {
            println!("镜像本地已存在，跳过 pull: {}", id);
        } else {
            let registry_credentials =
                Self::map_credentials(self.image_repository.image_repository_credentials.clone());
            client
                .images()
                .pull(id.as_str(), Some(registry_credentials))
                .await?;

            println!("私有镜像拉取成功！: {}", remote_image.name);
        }

        Ok(Image {
            id: remote_image.id.clone(),
            name: remote_image.name.clone(),
            tag: remote_image.tag.clone(),
            size: None,
            created_at: Utc::now(),
            status: ImageStatus::Runnable,
        })
    }

    async fn check_image(&self, image: &RemoteImage) -> anyhow::Result<bool> {
        let client = DockerClient::new()?;
        let full_name =
            Self::map_image_full_name(&self.image_repository.address, &image.name, &image.tag);
        Ok(client.registry().image_exists_locally(&full_name).await?)
    }

    async fn last_version(&self, image: &RemoteImage) -> anyhow::Result<String> {
        let client = DockerClient::new()?;
        let images = client.images().list(true).await?;
        let repo_prefix = format!("{}/{}", self.image_repository.address, image.name);

        let mut candidates: Vec<_> = images
            .into_iter()
            .filter(|img| img.repo_tags.iter().any(|t| t.starts_with(&repo_prefix)))
            .collect();
        candidates.sort_by_key(|img| -img.created);

        candidates
            .first()
            .and_then(|img| {
                img.repo_tags
                    .iter()
                    .find(|t| t.starts_with(&repo_prefix))
                    .and_then(|t| t.rsplit(':').next().map(|s| s.to_string()))
            })
            .ok_or_else(|| anyhow::anyhow!("no local image found for {}", image.name))
    }

    async fn get_container(&self, id: String) -> Result<GameContainer, ContainerError> {
        self.docker_instances
            .get(&id)
            .await
            .map_err(|e| ContainerError::Unknown)?
            .ok_or_else(|| ContainerError::NotFound(id))
    }

    async fn create_container(
        &self,
        container_name: String,
        game_build: LocalGameBuild,
        path_mapping: Vec<crate::domain::ContainerFilePathMappingHost>,
        port_mapping: Option<crate::domain::ContainerPortMapping>,
        resource_limitation: Option<crate::domain::ContainerResourceLimitation>,
    ) -> Result<GameContainer, ContainerError> {
        let client = DockerClient::new().map_err(to_io_error)?;

        let image_full_name = Self::map_image_full_name(
            &self.image_repository.address,
            &game_build.image.name,
            &game_build.image.tag,
        );

        // 构建容器
        let mut builder = client
            .containers()
            .create(&image_full_name)
            .name(format!("game-{}-{}", game_build.build_id, container_name))
            .label("managed-by", "node-agent");

        // 挂载卷
        if path_mapping.len() > 0 {
            for mapping in path_mapping.clone() {
                builder =
                    builder.volume(&mapping.host_path.path, &mapping.container_file_path.path);
            }
        }

        // 端口映射
        if let Some(ref port_mapping) = port_mapping {
            for port_map in &port_mapping.port_maps {
                let protocol = match port_map.mapping_port_type {
                    MappingPortType::TCP => "tcp",
                    MappingPortType::UDP => "udp",
                };
                builder = builder.port(port_map.host_port, port_map.container_port, protocol);
            }
        }

        // 资源限制
        if resource_limitation.is_some() {
            // ContainerResourceLimitation 当前为空结构体，后续扩展
        }

        let container_ref = builder.build().await.map_err(to_io_error)?;
        let container_id = container_ref.id().to_string();

        // 启动容器
        container_ref.start().await.map_err(to_io_error)?;

        println!("容器创建并启动成功: {}", container_id);

        let container = GameContainer {
            id: container_id,
            game_build,
            container: ConatinerType::DockerContainer,
            container_file_path_mapping: path_mapping,
            container_port_mapping: port_mapping,
            resource_limitation,
            status: ContainerStatus::Created,
        };

        // 持久化到 repository
        self.docker_instances
            .save(&container)
            .await
            .map_err(|e| ContainerError::Unknown)?;

        Ok(container)
    }

    async fn stop_container(&self, id: String) -> Result<GameContainer, ContainerError> {
        // 先从 repo 取出记录
        let mut container = self
            .docker_instances
            .get(&id)
            .await
            .map_err(|_| ContainerError::Unknown)?
            .ok_or_else(|| ContainerError::NotFound(id.clone()))?;

        let client = DockerClient::new().map_err(to_io_error)?;
        client
            .containers()
            .get(&id)
            .stop(Some(30))
            .await
            .map_err(|e| match e {
                lmrc_docker::DockerError::ContainerNotFound(_) => ContainerError::NotFound(id),
                _ => ContainerError::Unknown,
            })?;

        container.status = ContainerStatus::Exited;
        self.docker_instances
            .save(&container)
            .await
            .map_err(|_| ContainerError::Unknown)?;

        Ok(container)
    }

    async fn restart_container(&self, id: String) -> Result<GameContainer, ContainerError> {
        // 先从 repo 取出记录
        let mut container = self
            .docker_instances
            .get(&id)
            .await
            .map_err(|_| ContainerError::Unknown)?
            .ok_or_else(|| ContainerError::NotFound(id.clone()))?;

        let client = DockerClient::new().map_err(to_io_error)?;
        client
            .containers()
            .get(&id)
            .restart(Some(30))
            .await
            .map_err(|e| match e {
                lmrc_docker::DockerError::ContainerNotFound(_) => ContainerError::NotFound(id),
                _ => ContainerError::Unknown,
            })?;

        container.status = ContainerStatus::Running;
        self.docker_instances
            .save(&container)
            .await
            .map_err(|_| ContainerError::Unknown)?;

        Ok(container)
    }

    async fn remove_container(&self, id: String) -> Result<GameContainer, ContainerError> {
        let container = self
            .docker_instances
            .get(&id)
            .await
            .map_err(|_| ContainerError::Unknown)?
            .ok_or_else(|| ContainerError::NotFound(id.clone()))?;

        let client = DockerClient::new().map_err(to_io_error)?;
        client
            .containers()
            .get(&id)
            .remove(false, false)
            .await
            .map_err(|e| match e {
                lmrc_docker::DockerError::ContainerNotFound(_) => {
                    ContainerError::NotFound(id.clone())
                }
                _ => ContainerError::Unknown,
            })?;

        // 清除 repo 记录
        self.docker_instances
            .delete(&id)
            .await
            .map_err(|_| ContainerError::Unknown)?;

        Ok(container)
    }

    /**
     * 更新container的状态，使用lmrc获取真实的容器状态并更新到repos中
     */
    async fn update_container_status(&self) -> Result<i32, ContainerError> {
        let client = DockerClient::new().map_err(to_io_error)?;
        let summaries = client.containers().list(true).await.map_err(to_io_error)?;
        let mut updated_count = 0i32;

        for summary in &summaries {
            let container_id = match &summary.id {
                Some(id) => id,
                None => continue,
            };

            let docker_state = match &summary.state {
                Some(state) => state,
                None => continue,
            };

            // ContainerSummaryStateEnum → ContainerStatus
            let mapped_status = match format!("{:?}", docker_state).as_str() {
                "CREATED" => ContainerStatus::Created,
                "RUNNING" => ContainerStatus::Running,
                "PAUSED" => ContainerStatus::Paused,
                "RESTARTING" => ContainerStatus::Eestarting,
                "EXITED" => ContainerStatus::Exited,
                "DEAD" => ContainerStatus::Dead,
                "REMOVING" => ContainerStatus::Removing,
                _ => continue,
            };

            // 更新本地 repo 中的记录
            match self.docker_instances.get(container_id).await {
                Ok(Some(mut game_container)) => {
                    game_container.status = mapped_status;
                    if self.docker_instances.save(&game_container).await.is_ok() {
                        updated_count += 1;
                    }
                }
                _ => {
                    // 容器存在于 Docker 但不在本地 repo 中（孤立容器），跳过
                }
            }
        }

        Ok(updated_count)
    }
}

impl DockerContainerClient {
    fn map_credentials(
        image_repository_credentials: ImageRepositoryCredentials,
    ) -> DockerCredentials {
        DockerCredentials {
            username: image_repository_credentials.username,
            password: image_repository_credentials.password,
            auth: image_repository_credentials.auth,
            email: image_repository_credentials.email,
            serveraddress: image_repository_credentials.serveraddress,
            identitytoken: image_repository_credentials.identitytoken,
            registrytoken: image_repository_credentials.registrytoken,
        }
    }

    fn map_image_full_name(register_address: &str, image_name: &str, tag: &str) -> String {
        format!("{}/{}:{}", register_address, image_name, tag)
    }
}

fn to_io_error(e: lmrc_docker::DockerError) -> ContainerError {
    ContainerError::IOError {
        source: std::io::Error::new(std::io::ErrorKind::Other, e.to_string()),
    }
}
