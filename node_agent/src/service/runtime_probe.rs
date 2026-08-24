use std::collections::HashMap;
use std::sync::Arc;
use std::time::Duration;

use chrono::Utc;
use serde::Deserialize;
use tokio::sync::RwLock;
use tokio_util::sync::CancellationToken;

use crate::domain::{GameInstance, GameInstanceStatus};
use crate::error::NodeAgentError;
use crate::ports::{ContainerClient, GameInstanceRepository};

/// 单实例运行时统计（B-04/P1-1）。
/// 由 RuntimeProbeService 周期探测产生，随 NodeHeartbeat.instance_runtime 上报 controller。
#[derive(Debug, Clone)]
pub struct InstanceRuntimeStat {
    pub instance_id: String,
    pub player_count: u32,
    pub max_players: u32,
    pub healthy: bool,
    pub probe_mode: String,
    pub probe_error: String,
    pub sampled_at: String, // RFC3339
}

/// health.sh 输出契约：{"healthy": bool, "reason": "..."}
#[derive(Debug, Deserialize)]
struct HealthOutput {
    #[serde(default)]
    healthy: bool,
    #[serde(default)]
    reason: String,
}

/// players.sh 输出契约：{"players": N, "max_players": M}
#[derive(Debug, Deserialize)]
struct PlayersOutput {
    #[serde(default)]
    players: u32,
    #[serde(default)]
    max_players: u32,
}

/// A2S_INFO 解析结果（B-04/P1-3）
struct A2sInfo {
    players: u32,
    max_players: u32,
}

/// 运行时探针服务（B-04/P1-1/P1-3）：
/// 后台周期对 Running 实例执行探测（a2s 后端：UDP A2S_INFO；script 后端：docker exec
/// health.sh/players.sh），解析结果写入内存缓存；GetHeartbeat handler 只读缓存
/// （不阻塞心跳，探测延迟与心跳解耦）。
/// 与 BackendContainerChecker 正交：后者管「容器 Exited→Failed」，本服务管「进程可服务性/人数」。
pub struct RuntimeProbeService {
    container_client: Arc<dyn ContainerClient>,
    game_instance_repos: Arc<dyn GameInstanceRepository>,
    cache: Arc<RwLock<HashMap<String, InstanceRuntimeStat>>>,
    token: Option<CancellationToken>,
    handle: Option<tokio::task::JoinHandle<()>>,
}

impl RuntimeProbeService {
    pub fn new(
        container_client: Arc<dyn ContainerClient>,
        game_instance_repos: Arc<dyn GameInstanceRepository>,
    ) -> Self {
        Self {
            container_client,
            game_instance_repos,
            cache: Arc::new(RwLock::new(HashMap::new())),
            token: None,
            handle: None,
        }
    }

    /// 启动后台探测循环（interval 每轮间隔）。
    pub async fn start(&mut self, interval: Duration) {
        let token = CancellationToken::new();
        let child_token = token.clone();
        self.token = Some(token);

        let client = self.container_client.clone();
        let repos = self.game_instance_repos.clone();
        let cache = self.cache.clone();
        self.handle = Some(tokio::spawn(async move {
            log::info!("[RuntimeProbe] 运行时探针循环启动 interval={interval:?}");
            loop {
                tokio::select! {
                    _ = child_token.cancelled() => {
                        log::info!("[RuntimeProbe] 运行时探针循环退出");
                        break;
                    }
                    _ = tokio::time::sleep(interval) => {
                        if let Err(e) = probe_once(&client, &repos, &cache).await {
                            log::error!("[RuntimeProbe] 探测循环失败: {e:?}");
                        }
                    }
                }
            }
        }));
    }

    pub async fn stop(&mut self) {
        if let Some(token) = &self.token {
            token.cancel();
        }
        self.token = None;
        self.handle = None;
    }

    /// 心跳读取缓存快照（仅 running 且至少探测过一次的实例；尚未有结果 = 未知态，不出现）。
    pub async fn snapshot(&self) -> Vec<InstanceRuntimeStat> {
        let cache = self.cache.read().await;
        cache.values().cloned().collect()
    }
}

/// 一轮探测：对每个 Running 实例按 probe_mode 执行探测并整体刷新缓存（非 running 自然清除）。
async fn probe_once(
    client: &Arc<dyn ContainerClient>,
    repos: &Arc<dyn GameInstanceRepository>,
    cache: &RwLock<HashMap<String, InstanceRuntimeStat>>,
) -> Result<(), NodeAgentError> {
    let instances = repos.get_all().await?;
    let mut fresh: HashMap<String, InstanceRuntimeStat> = HashMap::new();
    for inst in &instances {
        if inst.status != GameInstanceStatus::Running {
            continue;
        }
        if inst.probe_mode == "none" {
            continue; // 显式禁用探测（unknown 态）
        }
        let Some(container_id) = &inst.container_id else {
            continue;
        };
        let stat = probe_instance(client, container_id, inst).await;
        fresh.insert(inst.id.clone(), stat);
    }
    *cache.write().await = fresh;
    Ok(())
}

/// 探测单实例：按 probe_mode 路由（a2s / script）。
/// health 失败原因透传；players 失败降级为 0（不掩盖 healthy 状态）。
async fn probe_instance(
    client: &Arc<dyn ContainerClient>,
    container_id: &str,
    inst: &GameInstance,
) -> InstanceRuntimeStat {
    let sampled_at = Utc::now().to_rfc3339();

    let (player_count, max_players, healthy, probe_error, probe_mode) =
        match inst.probe_mode.as_str() {
            "a2s" => match inst.query_host_port {
                Some(port) => {
                    match a2s_info(std::net::Ipv4Addr::LOCALHOST.into(), port).await {
                        Ok(info) => (info.players, info.max_players, true, String::new(), "a2s".to_string()),
                        Err(e) => (0, 0, false, e, "a2s".to_string()),
                    }
                }
                // 未解析查询端口 → 回退 script 后端
                None => {
                    let (p, m, h, e) = probe_script(client, container_id).await;
                    (p, m, h, e, "script".to_string())
                }
            },
            // 缺省（含空/未知）：script 后端
            _ => {
                let (p, m, h, e) = probe_script(client, container_id).await;
                (p, m, h, e, "script".to_string())
            }
        };

    InstanceRuntimeStat {
        instance_id: inst.id.clone(),
        player_count,
        max_players,
        healthy,
        probe_mode,
        probe_error,
        sampled_at,
    }
}

/// script 后端：exec health.sh + players.sh，解析 JSON。
async fn probe_script(
    client: &Arc<dyn ContainerClient>,
    container_id: &str,
) -> (u32, u32, bool, String) {
    let (healthy, probe_error) = match run_script(client, container_id, "/scripts/health.sh").await {
        Ok((code, stdout)) => {
            if code != 0 {
                (false, format!("health.sh 退出码 {code}"))
            } else {
                match serde_json::from_str::<HealthOutput>(&stdout) {
                    Ok(h) => {
                        if h.healthy {
                            (true, String::new())
                        } else {
                            (false, if h.reason.is_empty() { "游戏进程不健康".to_string() } else { h.reason })
                        }
                    }
                    Err(e) => (false, format!("health.sh 输出解析失败: {e}")),
                }
            }
        }
        Err(e) => (false, e),
    };

    let (player_count, max_players) =
        match run_script(client, container_id, "/scripts/players.sh").await {
            Ok((code, stdout)) => {
                if code == 0 {
                    match serde_json::from_str::<PlayersOutput>(&stdout) {
                        Ok(p) => (p.players, p.max_players),
                        Err(_) => (0, 0),
                    }
                } else {
                    (0, 0)
                }
            }
            Err(_) => (0, 0),
        };

    (player_count, max_players, healthy, probe_error)
}

/// 容器内执行脚本（带超时，防止脚本挂起卡死探测循环）。
async fn run_script(
    client: &Arc<dyn ContainerClient>,
    container_id: &str,
    script: &str,
) -> Result<(i32, String), String> {
    let exec = client.exec(container_id.to_string(), vec![script.to_string()]);
    match tokio::time::timeout(Duration::from_secs(5), exec).await {
        Err(_) => Err(format!("exec {script} 超时(5s)")),
        Ok(Err(e)) => Err(format!("exec {script} 失败: {e}")),
        Ok(Ok(out)) => Ok((out.exit_code, out.stdout)),
    }
}

// ---------------------------------------------------------------------------
// A2S 后端（B-04/P1-3）：A2S_INFO 查询（UDP，无需 challenge）。
// 适用：Valheim / Palworld / V Rising / 7dtd 等 Source 系（查询端口由 controller 按
// game_container_configs.query_port_offset 解析下发，query_host_port 为宿主端口）。
// 健康判定：收到合法 A2S_INFO 响应 = 游戏在监听查询端口。
// ---------------------------------------------------------------------------

async fn a2s_info(host: std::net::IpAddr, port: u16) -> Result<A2sInfo, String> {
    let sock = tokio::net::UdpSocket::bind("0.0.0.0:0")
        .await
        .map_err(|e| format!("bind udp: {e}"))?;
    sock.connect((host, port))
        .await
        .map_err(|e| format!("connect {host}:{port}: {e}"))?;

    let mut req = vec![0xFFu8; 4];
    req.push(0x54); // 'T' = A2S_INFO
    req.extend_from_slice(b"Source Engine Query\0");
    sock.send(&req).await.map_err(|e| format!("send: {e}"))?;

    let mut buf = [0u8; 1400];
    let n = tokio::time::timeout(Duration::from_secs(2), sock.recv(&mut buf))
        .await
        .map_err(|_| format!("A2S_INFO 超时 ({port})"))?
        .map_err(|e| format!("recv: {e}"))?;
    parse_a2s_info(&buf[..n])
}

/// 解析 A2S_INFO 响应：`FF FF FF FF 49` + protocol + name/map/folder/game(4 个 NUL 串) +
/// appid(2) + players(1) + max_players(1)。
fn parse_a2s_info(d: &[u8]) -> Result<A2sInfo, String> {
    if d.len() < 6 || d[0..4] != [0xFFu8; 4] || d[4] != 0x49 {
        return Err("非 A2S_INFO 响应".to_string());
    }
    let mut i = 5; // 跳过 0x49
    if i >= d.len() {
        return Err("响应截断".to_string());
    }
    i += 1; // protocol
    for _ in 0..4 {
        // name / map / folder / game：4 个 NUL 结尾字符串
        while i < d.len() && d[i] != 0 {
            i += 1;
        }
        if i >= d.len() {
            return Err("响应截断（字符串）".to_string());
        }
        i += 1; // 跳过 NUL
    }
    if i + 4 > d.len() {
        return Err("响应截断（appid/人数）".to_string());
    }
    let players = d[i + 2] as u32;
    let max_players = d[i + 3] as u32;
    Ok(A2sInfo { players, max_players })
}

#[cfg(test)]
mod tests {
    use super::*;

    // 构造标准 A2S_INFO 响应（旧格式，无 EDF 扩展）
    fn sample_a2s_response() -> Vec<u8> {
        let mut d: Vec<u8> = vec![0xFF, 0xFF, 0xFF, 0xFF, 0x49, 0x11]; // header + protocol
        d.extend_from_slice(b"name\0");
        d.extend_from_slice(b"map\0");
        d.extend_from_slice(b"folder\0");
        d.extend_from_slice(b"game\0");
        d.extend_from_slice(&0x000A_u16.to_le_bytes()); // appid = 10
        d.push(3); // players = 3
        d.push(8); // max_players = 8
        d.extend_from_slice(&[0x00, 0x64, 0x6C, 0x00, 0x01]); // bots/server/env/visibility/vac
        d
    }

    #[test]
    fn parse_valid_a2s_info() {
        let info = parse_a2s_info(&sample_a2s_response()).expect("should parse");
        assert_eq!(info.players, 3);
        assert_eq!(info.max_players, 8);
    }

    #[test]
    fn parse_rejects_garbage() {
        assert!(parse_a2s_info(&[0x01, 0x02, 0x03]).is_err());
        assert!(parse_a2s_info(&[0xFF, 0xFF, 0xFF, 0xFF, 0x49, 0x11]).is_err()); // 截断
    }

    #[test]
    fn parse_handles_empty_strings() {
        // 服务器名等为空字符串：仍可解析人数
        let mut d: Vec<u8> = vec![0xFF, 0xFF, 0xFF, 0xFF, 0x49, 0x11];
        d.push(0); // name = ""
        d.push(0); // map = ""
        d.push(0); // folder = ""
        d.push(0); // game = ""
        d.extend_from_slice(&0x0001_u16.to_le_bytes());
        d.push(0);
        d.push(16);
        let info = parse_a2s_info(&d).expect("should parse");
        assert_eq!(info.players, 0);
        assert_eq!(info.max_players, 16);
    }
}
