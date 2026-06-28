use async_trait::async_trait;
use chrono::Utc;
use lmrc_docker::{DockerClient, DockerCredentials};
use crate::domain::{Image, ImageRepository, ImageRepositoryCredentials, ImageStatus, RemoteImage};
use crate::ports::ImageClient;

struct DockerImageClient {
    image_repository: ImageRepository
}

#[async_trait]
impl ImageClient for DockerImageClient {
    async fn pull_image(&self, remote_image: &RemoteImage) -> anyhow::Result<Image> {
        let client = DockerClient::new()?;

        // 1. 配置私有仓库的认证信息
        let registry_credentials =
            DockerImageClient::map_credentials(self.image_repository.image_repository_credentials.clone());

        // 2. 拉取镜像，传递认证信息
        //    注意：这里的 "my-private-registry.example.com/my-image:tag"
        //    是完整的镜像地址
        let id = DockerImageClient::map_image_full_name(
            &self.image_repository.address,
            remote_image.name.as_str(),
            remote_image.tag.as_str()
        );
        client.images()
            .pull(id.as_str(), Some(registry_credentials))
            .await?;

        println!("私有镜像拉取成功！: {}", remote_image.name);

        Ok(Image {
            id: remote_image.id.clone(),
            name: remote_image.name.clone(),
            tag: remote_image.tag.clone(),
            size: None,
            created_at: Utc::now(),
            status: ImageStatus::Runnable
        })
    }

    async fn check_image(&self, image: &RemoteImage) -> anyhow::Result<bool> {
        todo!()
    }

    async fn last_version(&self, image: &RemoteImage) -> anyhow::Result<String> {
        todo!()
    }
}

impl DockerImageClient {
    fn map_credentials(image_repository_credentials: ImageRepositoryCredentials) -> DockerCredentials {
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