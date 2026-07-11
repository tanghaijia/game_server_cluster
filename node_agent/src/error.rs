use thiserror::Error;

use crate::ports::ContainerError;

#[derive(Debug, Error)]
pub enum NodeAgentError {
    #[error("invalid request: {message}")]
    InvalidRequest { message: String },
    #[error("build preparation failed: {message}")]
    BuildPreparationFailed { message: String },
    #[error("instance runtime failure: {message}")]
    InstanceRuntimeFailed { message: String },
    #[error("instance {instance_id} was not found")]
    InstanceNotFound { instance_id: String },
    #[error("internal error: {message}")]
    Internal { message: String },
    #[error("image repository request fail error: {message}")]
    ImageRepositoryRequestFail { message: String },
    #[error("DB operation fail error: {message}")]
    DBOperationFail { message: String },
    #[error("Empty Snapshot error: {message}")]
    EmptySnapShotFail { message: String },
    #[error("S3 Download error: {message}")]
    S3DownloadFail { message: String },
    #[error("S3 Upload error: {message}")]
    S3UploadFail { message: String },
    #[error("Container error: {source}")]
    ConatinerFail {
        #[from]
        source: ContainerError,
    },
    #[error("Path error: {message}")]
    PathError { message: String },
}
