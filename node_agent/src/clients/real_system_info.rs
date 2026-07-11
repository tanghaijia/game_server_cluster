use std::net::IpAddr;

use async_trait::async_trait;
use sysinfo::{Disks, System};

use crate::domain::{NodeId, SystemError};
use crate::error::NodeAgentError;
use crate::ports::{NodeHeartbeat, SystemInfoProvider};

pub struct RealSystemInfoProvider {
    system: System,
    node_id: NodeId,
}

impl RealSystemInfoProvider {
    pub fn new(node_id: String) -> Self {
        Self {
            system: System::new_all(),
            node_id: NodeId(node_id),
        }
    }
}

#[async_trait]
impl SystemInfoProvider for RealSystemInfoProvider {
    async fn heartbeat(&self) -> Result<NodeHeartbeat, NodeAgentError> {
        // CPU usage
        let cpu_usage = self.system.global_cpu_usage();

        // 内存
        let total_mem = self.system.total_memory();
        let used_mem = self.system.used_memory();
        let mem_pct = if total_mem > 0 {
            (used_mem as f64 / total_mem as f64 * 100.0) as f32
        } else {
            0.0
        };

        // 磁盘（取第一个非空磁盘）
        let disk_pct = Disks::new()
            .iter()
            .find(|d| !d.mount_point().as_os_str().is_empty())
            .map(|d| {
                let total = d.total_space();
                let available = d.available_space();
                if total > 0 {
                    ((total - available) as f64 / total as f64 * 100.0) as f32
                } else {
                    0.0
                }
            })
            .unwrap_or(0.0);

        Ok(NodeHeartbeat {
            node_id: self.node_id.clone(),
            cpu_usage_pct: cpu_usage,
            memory_usage_pct: mem_pct,
            disk_usage_pct: disk_pct,
            running_instances: 0,
        })
    }

    async fn get_host_ip(&self) -> Result<IpAddr, SystemError> {
        let socket = std::net::UdpSocket::bind("0.0.0.0:0")
            .map_err(|e| SystemError::HostIPError {
                ip: "0.0.0.0".to_string(),
                message: e.to_string(),
            })?;
        socket
            .connect("8.8.8.8:53")
            .map_err(|e| SystemError::HostIPError {
                ip: "8.8.8.8".to_string(),
                message: e.to_string(),
            })?;
        let local_addr = socket
            .local_addr()
            .map_err(|e| SystemError::HostIPError {
                ip: local_addr_fallback(),
                message: e.to_string(),
            })?;
        Ok(local_addr.ip())
    }

    async fn set_node_id(&self, _node_id: String) {}

    async fn get_node_id(&self) -> Option<String> {
        Some(self.node_id.0.clone())
    }
}

fn local_addr_fallback() -> String {
    "0.0.0.0".to_string()
}
