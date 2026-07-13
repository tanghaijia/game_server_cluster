use thiserror::Error;

#[derive(Debug, Error)]
pub enum AssetServiceError {
    #[error("invalid request: {message}")]
    InvalidRequest { message: String },
    #[error("game build {build_id} was not found")]
    BuildNotFound { build_id: String },
    #[error("snapshot {snapshot_id} was not found")]
    SnapshotNotFound { snapshot_id: String },
    #[error("mod manifest {manifest_id} was not found")]
    ModManifestNotFound { manifest_id: String },
    #[error("node {node_id} was not found")]
    NodeNotFound { node_id: String },
    #[error("node agent {node_id} was not found")]
    NodeAgentNotFound { node_id: String },
    #[error("conflict: {message}")]
    Conflict { message: String },
    #[error("internal error: {message}")]
    Internal { message: String },
}
