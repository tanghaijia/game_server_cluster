use async_trait::async_trait;

use crate::{
    domain::{NodeOperation, OperationId},
    error::NodeAgentError,
};

#[async_trait]
pub trait OperationRepository: Send + Sync {
    async fn save(&self, operation: &NodeOperation) -> Result<(), NodeAgentError>;

    async fn get(
        &self,
        operation_id: &OperationId,
    ) -> Result<Option<NodeOperation>, NodeAgentError>;
}
