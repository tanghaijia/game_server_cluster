use std::net::IpAddr;

use async_trait::async_trait;

use crate::{
    domain::{NodeId, SystemError},
    error::NodeAgentError,
};

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

    async fn get_host_ip(&self) -> Result<IpAddr, SystemError>;

    async fn set_node_id(&self, node_id: String);

    async fn get_node_id(&self) -> Option<String>;
}
