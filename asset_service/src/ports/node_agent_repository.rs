use async_trait::async_trait;

use crate::domain::NodeAgent;
use crate::error::AssetServiceError;

#[async_trait]
pub trait NodeAgentRepository: Send + Sync {
    async fn save(&self, agent: &NodeAgent) -> Result<(), AssetServiceError>;

    async fn get(&self, node_id: &str) -> Result<Option<NodeAgent>, AssetServiceError>;

    async fn list(&self) -> Result<Vec<NodeAgent>, AssetServiceError>;

    async fn delete(&self, node_id: &str) -> Result<(), AssetServiceError>;
}
