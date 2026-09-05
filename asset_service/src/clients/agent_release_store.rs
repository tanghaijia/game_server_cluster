use async_trait::async_trait;
use aws_sdk_s3::primitives::ByteStream;
use aws_sdk_s3::Client as S3Client;

use crate::error::AssetServiceError;
use crate::ports::AgentReleaseStore;

/// S3/MinIO release 存储（生产）。配置与 node_agent 快照同款：
/// `AWS_*`（aws_config::load_from_env）+ 可选 `S3_ENDPOINT`（MinIO/R2 需 path-style）。
pub struct S3AgentReleaseStore {
    client: S3Client,
}

impl S3AgentReleaseStore {
    pub fn new(client: S3Client) -> Self {
        Self { client }
    }
}

#[async_trait]
impl AgentReleaseStore for S3AgentReleaseStore {
    async fn put_object(
        &self,
        bucket: &str,
        key: &str,
        body: Vec<u8>,
    ) -> Result<(), AssetServiceError> {
        let size = body.len();
        let result = self
            .client
            .put_object()
            .bucket(bucket)
            .key(key)
            .body(ByteStream::from(body))
            .send()
            .await
            .map_err(|e| AssetServiceError::Internal {
                message: format!("s3 put {bucket}/{key} 失败: {e}"),
            })?;
        let _ = result;
        log::info!("release 对象已写入 {bucket}/{key}（{size} B）");
        Ok(())
    }
}
