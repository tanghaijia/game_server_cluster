use async_trait::async_trait;

use crate::{
    domain::{NodeOperation, OperationId, OperationKind},
    error::NodeAgentError,
};

#[async_trait]
pub trait OperationRepository: Send + Sync {
    async fn save(&self, operation: &NodeOperation) -> Result<(), NodeAgentError>;

    async fn get(
        &self,
        operation_id: &OperationId,
    ) -> Result<Option<NodeOperation>, NodeAgentError>;

    /// 查找指定 kind + 自然键(instance_id 或 build_id)的进行中(PENDING/RUNNING)操作。
    /// 用于接口幂等去重:存在则返回,调用方应复用该 operation 而非新建。
    async fn find_active(
        &self,
        kind: OperationKind,
        key: &str,
    ) -> Result<Option<NodeOperation>, NodeAgentError>;
}
