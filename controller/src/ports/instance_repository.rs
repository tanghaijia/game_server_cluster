use async_trait::async_trait;

use crate::{
    domain::{GameInstance, InstanceId},
    error::ControllerError,
};

#[async_trait]
pub trait InstanceRepository: Send + Sync {
    async fn create(&self, instance: &GameInstance) -> Result<(), ControllerError>;

    async fn get(&self, instance_id: &InstanceId) -> Result<Option<GameInstance>, ControllerError>;

    async fn save(&self, instance: &GameInstance) -> Result<(), ControllerError>;

    async fn list_unfinished(&self) -> Result<Vec<GameInstance>, ControllerError>;
}
