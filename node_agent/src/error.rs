use thiserror::Error;

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
}
