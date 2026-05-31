use async_trait::async_trait;

use crate::{domain::NodeId, error::NodeAgentError};

#[derive(Debug, Clone)]
pub struct NodeHeartbeat {
    pub node_id: NodeId,
    pub cpu_usage_pct: f32,
    pub memory_usage_pct: f32,
    pub disk_usage_pct: f32,
    pub running_instances: u32,
}

#[async_trait]
pub trait SystemInfoProvider: Send + Sync {
    async fn heartbeat(&self) -> Result<NodeHeartbeat, NodeAgentError>;
}
