use async_trait::async_trait;

use crate::error::AssetServiceError;

/// node_agent 发布二进制对象存储（P1，docs/agent-release-asset-service-redesign.md）：
/// 接收完整字节并写入对象存储。生产 = S3/MinIO（`S3AgentReleaseStore`），
/// 开发/无 S3 环境 = `InMemoryAgentReleaseStore`（进程内 HashMap）。
#[async_trait]
pub trait AgentReleaseStore: Send + Sync {
    /// 写入对象；失败返回可读错误（进 gRPC Status）。
    async fn put_object(
        &self,
        bucket: &str,
        key: &str,
        body: Vec<u8>,
    ) -> Result<(), AssetServiceError>;
}
