use std::collections::HashMap;

use thiserror::Error;

use crate::domain::OperationError;
use crate::ports::ContainerError;
use crate::proto::node_agent::{BusinessErrorCode, ErrorCategory};

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
    #[error("game cache {game_id}:{branch_name} was not found")]
    GameCacheNotFound { game_id: String, branch_name: String },
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
    #[error("Game Build error: {message}")]
    GameBuildError { message: String },
}

impl NodeAgentError {
    /// 将领域错误映射为结构化业务错误详情(OperationError)。
    ///
    /// 数值字段与 error.proto 的 BusinessErrorCode / ErrorCategory 严格对应。
    /// `retryable` 决定调用方是否可自动重试:基础设施/超时类可重试,参数/资源缺失类不可重试。
    pub fn to_operation_error(&self) -> OperationError {
        let (code, category, retryable) = match self {
            NodeAgentError::InvalidRequest { .. } => (
                BusinessErrorCode::InvalidArgument,
                ErrorCategory::InvalidRequest,
                false,
            ),
            NodeAgentError::InstanceNotFound { .. } => (
                BusinessErrorCode::InstanceNotFound,
                ErrorCategory::NotFound,
                false,
            ),
            NodeAgentError::GameCacheNotFound { .. } => (
                BusinessErrorCode::BuildCacheMiss,
                ErrorCategory::NotFound,
                false,
            ),
            NodeAgentError::BuildPreparationFailed { .. } => (
                BusinessErrorCode::BuildPrepareFailed,
                ErrorCategory::Internal,
                false,
            ),
            NodeAgentError::InstanceRuntimeFailed { .. } => (
                BusinessErrorCode::InstanceRuntimeFailed,
                ErrorCategory::Internal,
                false,
            ),
            NodeAgentError::Internal { .. } => (
                BusinessErrorCode::InternalError,
                ErrorCategory::Internal,
                false,
            ),
            NodeAgentError::ImageRepositoryRequestFail { .. } => (
                BusinessErrorCode::ImagePullFailed,
                ErrorCategory::Infrastructure,
                true,
            ),
            NodeAgentError::DBOperationFail { .. } => (
                BusinessErrorCode::DbOperationFailed,
                ErrorCategory::Internal,
                true,
            ),
            NodeAgentError::EmptySnapShotFail { .. } => (
                BusinessErrorCode::SnapshotEmpty,
                ErrorCategory::NotFound,
                false,
            ),
            NodeAgentError::S3DownloadFail { .. } => (
                BusinessErrorCode::S3DownloadFailed,
                ErrorCategory::Infrastructure,
                true,
            ),
            NodeAgentError::S3UploadFail { .. } => (
                BusinessErrorCode::S3UploadFailed,
                ErrorCategory::Infrastructure,
                true,
            ),
            NodeAgentError::ConatinerFail { source } => match source {
                ContainerError::NotFound(_) => (
                    BusinessErrorCode::ContainerError,
                    ErrorCategory::NotFound,
                    false,
                ),
                ContainerError::InsufficientResources => (
                    BusinessErrorCode::ResourceInsufficient,
                    ErrorCategory::Infrastructure,
                    true,
                ),
                ContainerError::IOError { .. } => (
                    BusinessErrorCode::ContainerError,
                    ErrorCategory::Infrastructure,
                    true,
                ),
                ContainerError::Unknown => (
                    BusinessErrorCode::ContainerError,
                    ErrorCategory::Internal,
                    false,
                ),
            },
            NodeAgentError::PathError { .. } => (
                BusinessErrorCode::InternalError,
                ErrorCategory::Internal,
                false,
            ),
            NodeAgentError::GameBuildError { .. } => (
                BusinessErrorCode::BuildPrepareFailed,
                ErrorCategory::Internal,
                false,
            ),
        };

        let mut params = HashMap::new();
        if let NodeAgentError::InstanceNotFound { instance_id } = self {
            params.insert("instance_id".to_string(), instance_id.clone());
        }
        if let NodeAgentError::GameCacheNotFound {
            game_id,
            branch_name,
        } = self
        {
            params.insert("game_id".to_string(), game_id.clone());
            params.insert("branch_name".to_string(), branch_name.clone());
        }

        OperationError {
            code: code as i32,
            category: category as i32,
            message: self.to_string(),
            retryable,
            params,
        }
    }
}
