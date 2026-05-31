use async_trait::async_trait;

use crate::{
    domain::{SnapshotId, SnapshotReference, SnapshotType},
    error::ControllerError,
};

#[derive(Debug, Clone)]
pub struct CreateSnapshotRecordRequest {
    pub instance_id: String,
    pub build_id: Option<String>,
    pub snapshot_type: SnapshotType,
    pub source_node: Option<String>,
}

#[derive(Debug, Clone)]
pub struct CompleteSnapshotRecordRequest {
    pub snapshot_id: SnapshotId,
    pub storage_uri: String,
    pub manifest_uri: Option<String>,
    pub checksum: Option<String>,
}

#[derive(Debug, Clone)]
pub struct SnapshotRecord {
    pub snapshot: SnapshotReference,
    pub build_id: Option<String>,
    pub instance_data_path: String,
}

#[derive(Debug, Clone)]
pub struct SnapshotRestorePlan {
    pub snapshot_id: SnapshotId,
    pub build_id: Option<String>,
    pub storage_uri: String,
    pub manifest_uri: Option<String>,
    pub checksum: Option<String>,
    pub instance_data_path: String,
}

#[async_trait]
pub trait SnapshotService: Send + Sync {
    async fn create_snapshot_record(
        &self,
        request: CreateSnapshotRecordRequest,
    ) -> Result<SnapshotRecord, ControllerError>;

    async fn complete_snapshot_record(
        &self,
        request: CompleteSnapshotRecordRequest,
    ) -> Result<SnapshotRecord, ControllerError>;

    async fn get_snapshot_restore_plan(
        &self,
        snapshot_id: &SnapshotId,
    ) -> Result<SnapshotRestorePlan, ControllerError>;
}
