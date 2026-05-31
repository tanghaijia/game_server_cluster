use std::{collections::HashMap, sync::{Arc, Mutex}};

use async_trait::async_trait;

use crate::{
    domain::{GameInstance, InstanceId, RuntimeState},
    error::ControllerError,
    ports::InstanceRepository,
};

#[derive(Default, Clone)]
pub struct InMemoryInstanceRepository {
    instances: Arc<Mutex<HashMap<String, GameInstance>>>,
}

#[async_trait]
impl InstanceRepository for InMemoryInstanceRepository {
    async fn create(&self, instance: &GameInstance) -> Result<(), ControllerError> {
        let mut instances = self.instances.lock().map_err(|_| ControllerError::DependencyFailure {
            message: "instance repository lock poisoned".to_string(),
        })?;
        if instances.contains_key(&instance.instance_id.0) {
            return Err(ControllerError::Conflict {
                message: format!("instance {} already exists", instance.instance_id.0),
            });
        }
        instances.insert(instance.instance_id.0.clone(), instance.clone());
        Ok(())
    }

    async fn get(&self, instance_id: &InstanceId) -> Result<Option<GameInstance>, ControllerError> {
        let instances = self.instances.lock().map_err(|_| ControllerError::DependencyFailure {
            message: "instance repository lock poisoned".to_string(),
        })?;
        Ok(instances.get(&instance_id.0).cloned())
    }

    async fn save(&self, instance: &GameInstance) -> Result<(), ControllerError> {
        let mut instances = self.instances.lock().map_err(|_| ControllerError::DependencyFailure {
            message: "instance repository lock poisoned".to_string(),
        })?;
        instances.insert(instance.instance_id.0.clone(), instance.clone());
        Ok(())
    }

    async fn list_unfinished(&self) -> Result<Vec<GameInstance>, ControllerError> {
        let instances = self.instances.lock().map_err(|_| ControllerError::DependencyFailure {
            message: "instance repository lock poisoned".to_string(),
        })?;
        Ok(instances.values().filter(|instance| !matches!(instance.runtime_state, RuntimeState::Running | RuntimeState::Stopped | RuntimeState::Failed)).cloned().collect())
    }
}
