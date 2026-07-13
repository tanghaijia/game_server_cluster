use async_trait::async_trait;

use crate::domain::Node;
use crate::error::AssetServiceError;

#[async_trait]
pub trait NodeRepository: Send + Sync {
    async fn save(&self, node: &Node) -> Result<(), AssetServiceError>;

    async fn get(&self, node_id: &str) -> Result<Option<Node>, AssetServiceError>;

    async fn list(&self) -> Result<Vec<Node>, AssetServiceError>;

    async fn delete(&self, node_id: &str) -> Result<(), AssetServiceError>;
}
