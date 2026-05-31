use async_trait::async_trait;

use crate::{
    domain::{ModManifest, ModManifestId},
    error::AssetServiceError,
};

#[async_trait]
pub trait ModManifestRepository: Send + Sync {
    async fn save(&self, manifest: &ModManifest) -> Result<(), AssetServiceError>;

    async fn get(
        &self,
        manifest_id: &ModManifestId,
    ) -> Result<Option<ModManifest>, AssetServiceError>;
}
