use std::net::IpAddr;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Mutex;
use std::time::Instant;

use async_trait::async_trait;
use sysinfo::{Disks, Networks, System};

use crate::domain::{NodeId, SystemError};
use crate::error::NodeAgentError;
use crate::ports::{NodeHeartbeat, SystemInfoProvider};

pub struct RealSystemInfoProvider {
    system: Mutex<System>,
    node_id: NodeId,
    networks: Mutex<Networks>,
    last_rx: AtomicU64,
    last_tx: AtomicU64,
    last_sample: Mutex<Option<Instant>>,
}

impl RealSystemInfoProvider {
    pub fn new(node_id: String) -> Self {
        Self {
            system: Mutex::new(System::new_all()),
            node_id: NodeId(node_id),
            networks: Mutex::new(Networks::new_with_refreshed_list()),
            last_rx: AtomicU64::new(0),
            last_tx: AtomicU64::new(0),
            last_sample: Mutex::new(None),
        }
    }
}

#[async_trait]
impl SystemInfoProvider for RealSystemInfoProvider {
    async fn heartbeat(&self) -> Result<NodeHeartbeat, NodeAgentError> {
        // CPU usage
        let mut system = self.system.lock().unwrap();
        system.refresh_cpu_usage();
        let cpu_usage = system.global_cpu_usage();

        // 内存
        system.refresh_memory();
        let total_mem = system.total_memory();
        let used_mem = system.used_memory();
        let mem_pct = if total_mem > 0 {
            (used_mem as f64 / total_mem as f64 * 100.0) as f32
        } else {
            0.0
        };
        drop(system);

        // 磁盘：取使用率最高的磁盘（修正：不只第一个非空盘，避免漏报数据盘打满，§9.5）
        let disk_pct = Disks::new()
            .iter()
            .filter(|d| !d.mount_point().as_os_str().is_empty())
            .map(|d| {
                let total = d.total_space();
                let available = d.available_space();
                if total > 0 {
                    ((total - available) as f64 / total as f64 * 100.0) as f32
                } else {
                    0.0
                }
            })
            .fold(0.0f32, |max, p| if p > max { p } else { max });

        // 带宽速率（字节/秒）：累计字节差分 / 采样间隔（P3 带宽评分数据源）
        let mut networks = self.networks.lock().unwrap();
        networks.refresh(true); // 移除已不存在的接口
        let mut total_rx: u64 = 0;
        let mut total_tx: u64 = 0;
        for (_, data) in networks.iter() {
            total_rx = total_rx.saturating_add(data.received());
            total_tx = total_tx.saturating_add(data.transmitted());
        }
        let last_rx = self.last_rx.load(Ordering::Relaxed);
        let last_tx = self.last_tx.load(Ordering::Relaxed);
        let now = Instant::now();
        let mut last_sample = self.last_sample.lock().unwrap();
        let (net_rx_bps, net_tx_bps) = match *last_sample {
            Some(last) => {
                let elapsed = now.duration_since(last).as_secs_f64();
                if elapsed > 0.0 {
                    let rx = (total_rx.saturating_sub(last_rx) as f64 / elapsed) as u64;
                    let tx = (total_tx.saturating_sub(last_tx) as f64 / elapsed) as u64;
                    (rx, tx)
                } else {
                    (0, 0)
                }
            }
            None => (0, 0), // 首次采样无基准，下一轮起有速率
        };
        self.last_rx.store(total_rx, Ordering::Relaxed);
        self.last_tx.store(total_tx, Ordering::Relaxed);
        *last_sample = Some(now);

        Ok(NodeHeartbeat {
            node_id: self.node_id.clone(),
            cpu_usage_pct: cpu_usage,
            memory_usage_pct: mem_pct,
            disk_usage_pct: disk_pct,
            // running_instances 由 NodeAgentService.heartbeat 覆盖为实例仓库真实值
            running_instances: 0,
            net_rx_bps,
            net_tx_bps,
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
                ip: "0.0.0.0".to_string(),
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
