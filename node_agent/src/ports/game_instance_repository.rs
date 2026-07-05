use tonic::async_trait;

use crate::{domain::GameInstance, error::NodeAgentError};

#[async_trait]
pub trait GameInstanceRepository: Send + Sync {
    async fn save(&self, game_instance: &GameInstance) -> Result<(), NodeAgentError>;

    async fn get(&self, game_instance_id: String) -> Result<GameInstance, NodeAgentError>;
}
