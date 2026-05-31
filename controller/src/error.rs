use thiserror::Error;

#[derive(Debug, Error)]
pub enum ControllerError {
    #[error("instance {instance_id} was not found")]
    InstanceNotFound { instance_id: String },
    #[error("instance {instance_id} is in an invalid state for {action}: {state}")]
    InvalidStateTransition {
        instance_id: String,
        action: &'static str,
        state: &'static str,
    },
    #[error("dependency failure: {message}")]
    DependencyFailure { message: String },
    #[error("conflict: {message}")]
    Conflict { message: String },
}
