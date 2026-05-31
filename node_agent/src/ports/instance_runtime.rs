use async_trait::async_trait;

use crate::{
    domain::{Endpoint, InstanceId, InstanceRuntimeRecord, InstanceRuntimeSpec},
    error::NodeAgentError,
};

#[derive(Debug, Clone)]
pub struct StartInstanceResult {
    pub endpoint: Option<Endpoint>,
}

#[async_trait]
pub trait InstanceRuntime: Send + Sync {
    async fn start_instance(
        &self,
        spec: InstanceRuntimeSpec,
    ) -> Result<StartInstanceResult, NodeAgentError>;

    async fn stop_instance(&self, instance_id: &InstanceId) -> Result<(), NodeAgentError>;

    async fn inspect_instance(
        &self,
        instance_id: &InstanceId,
    ) -> Result<Option<InstanceRuntimeRecord>, NodeAgentError>;
}
