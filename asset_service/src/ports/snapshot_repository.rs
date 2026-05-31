use async_trait::async_trait;

use crate::{
    domain::{SnapshotId, SnapshotRecord},
    error::AssetServiceError,
};

#[async_trait]
pub trait SnapshotRepository: Send + Sync {
    async fn save(&self, snapshot: &SnapshotRecord) -> Result<(), AssetServiceError>;

    async fn get(
        &self,
        snapshot_id: &SnapshotId,
    ) -> Result<Option<SnapshotRecord>, AssetServiceError>;

    async fn list_by_instance(
        &self,
        instance_id: &str,
    ) -> Result<Vec<SnapshotRecord>, AssetServiceError>;
}
