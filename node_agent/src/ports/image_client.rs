use async_trait::async_trait;

use crate::domain::{Image, ImageRespository, RemoteImage};

#[async_trait]
pub trait ImageClient: Send + Sync {
    async fn pull_image(&self, image_respository: &ImageRespository) -> anyhow::Result<Image>;

    async fn check_image(&self, image: &RemoteImage) -> anyhow::Result<bool>;

    async fn last_version(&self, image: &RemoteImage) -> anyhow::Result<String>;
}
