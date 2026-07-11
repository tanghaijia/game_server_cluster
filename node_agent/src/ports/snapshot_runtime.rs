use async_trait::async_trait;

use crate::proto::node_agent::SnapshotArtifact;
use crate::{
    domain::{SnapshotCaptureRequest, SnapshotRestoreRequest, SnapshotRestoreResult},
    error::NodeAgentError,
};

#[async_trait]
pub trait Snapshot_manager: Send + Sync {
    async fn create_snapshot(
        &self,
        request: SnapshotCaptureRequest,
    ) -> Result<SnapshotArtifact, NodeAgentError>;

    async fn restore_snapshot(
        &self,
        request: SnapshotRestoreRequest,
    ) -> Result<SnapshotRestoreResult, NodeAgentError>;
}
