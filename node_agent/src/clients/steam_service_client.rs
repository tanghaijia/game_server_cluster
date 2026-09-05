use std::collections::HashMap;
use std::path::PathBuf;
use std::println;
use std::sync::{Arc, Mutex, OnceLock};
use std::time::{Duration, SystemTime};

use anyhow::anyhow;
use regex::Regex;
use sysinfo::Disks;
use tokio::fs;
use tokio::io::{AsyncBufReadExt, AsyncReadExt, BufReader};
use tokio::process::Command;
use tonic::async_trait;

use crate::common::GAME_CACHE_SERVER_ROOT_PATH;
use crate::domain::{GameCache, GameCacheStatus};
use crate::ports::GameCacheRepository;
use crate::service::{SteamService, SteamServiceError};

pub struct SteamServiceClient {
    game_cache_repos: Arc<dyn GameCacheRepository>,
}

impl SteamServiceClient {
    pub fn new(game_cache_repos: Arc<dyn GameCacheRepository>) -> Self {
        Self { game_cache_repos }
    }

    pub async fn program_init(&self) -> anyhow::Result<()> {
        let game_caches = self.game_cache_repos.get_all().await?;
        for mut cache in game_caches {
            if cache.status == GameCacheStatus::Downloading {
                cache.status = GameCacheStatus::Removed;
                self.game_cache_repos.save(&cache).await?;
            }
        }
        Ok(())
    }

    /// 下载到 staging 目录，成功后原子 rename 到正式目录（P2-A：不触碰现存目录）。
    /// 失败只清理 staging，旧版本（若有）原封不动 → 天然可回滚。
    async fn download(&self, mut game_cache: GameCache) -> Result<(), SteamServiceError> {
        // path 为空是调用方错误，不需写 DB。
        // 注意：不要对 Option 直接 unwrap（path 为 None 时 panic），
        // 这里只记录 game_id/branch_name 即可定位。
        let Some(path_str) = game_cache.path.clone() else {
            log::error!(
                "{} {} 下载路径空",
                game_cache.game_id.as_str(),
                game_cache.branch_name.as_str(),
            );
            game_cache.status = GameCacheStatus::Unavailable;
            let _ = self.game_cache_repos.save(&game_cache).await;
            return Err(SteamServiceError::EmptyDownloadPathError {});
        };

        // 正式目录 /server/{game}/{branch}/{buildid}；staging 同级 .staging/{buildid}
        let final_dir = PathBuf::from(&path_str);
        let staging_dir = match final_dir.parent() {
            Some(parent) => parent
                .join(".staging")
                .join(final_dir.file_name().unwrap_or_default()),
            None => final_dir.clone(),
        };

        // P3（磁盘预检）：spawn steamcmd 前拦截「必然失败」的下载（磁盘不足，见设计稿 §4.3）。
        // 需求大小：优先同 (game,branch) 历史版本实测大小；首下无参考时向 steamcmd
        // app_info_print 查询 size_on_disk（缓存 12h）；都拿不到则仅硬闸 1 GiB。
        if let Some(reject) = self.preflight_check(&game_cache).await {
            log::error!(
                "下载预检拒绝：game={} branch={} build={} final={} reason={}",
                game_cache.game_id.as_str(),
                game_cache.branch_name.as_str(),
                game_cache.build_id.as_str(),
                path_str.as_str(),
                reject
            );
            // P4：拒绝原因落库（last_error），admin/controller 直接可见
            game_cache.status = GameCacheStatus::Unavailable;
            game_cache.last_error = Some(reject.clone());
            let _ = self.game_cache_repos.save(&game_cache).await;
            return Err(SteamServiceError::PreflightRejected(reject));
        }

        // 清理上次崩溃残留的 staging（半成品）
        if staging_dir.exists() {
            let _ = fs::remove_dir_all(&staging_dir).await;
        }
        if let Some(parent) = staging_dir.parent() {
            fs::create_dir_all(parent).await.map_err(|e| {
                let error_msg = format!("Failed to create directory {:?}: {}", parent, e);
                log::error!("{}", error_msg);
                SteamServiceError::IoError(e)
            })?;
        }

        // P1（下载可观测性）：整段计时，失败/成功日志均带耗时与双路径（见 docs/game-cache-download-observability-design.md）
        let started = std::time::Instant::now();
        let result = self.run_download(&mut game_cache, &staging_dir).await;
        let elapsed = started.elapsed();

        match result {
            Ok(()) => {
                // 原子切换：staging → 正式目录（同文件系统 rename）。
                // 正式目录残留（旧版本孤儿/失败残留）仅当该版本非 current 才存在，可安全替换。
                if final_dir.exists() {
                    fs::remove_dir_all(&final_dir).await.map_err(|e| {
                        log::error!("Failed to remove stale final dir {:?}: {}", final_dir, e);
                        SteamServiceError::IoError(e)
                    })?;
                }
                fs::rename(&staging_dir, &final_dir).await.map_err(|e| {
                    let error_msg =
                        format!("Failed to promote staging {:?} -> {:?}: {}", staging_dir, final_dir, e);
                    log::error!("{}", error_msg);
                    SteamServiceError::IoError(e)
                })?;
                // P2-B：实测缓存大小（供 controller 磁盘记账/调度；统计失败不阻塞下载）
                game_cache.size_bytes = dir_size(&final_dir).await.unwrap_or(0);
                // P4：成功转 Available 时清空上次失败原因
                game_cache.last_error = None;
                game_cache.status = GameCacheStatus::Available;
                self.game_cache_repos.save(&game_cache).await?;
                log::info!(
                    "下载完成：game={} branch={} build={} final={} size={}B elapsed={:.1}s",
                    game_cache.game_id.as_str(),
                    game_cache.branch_name.as_str(),
                    game_cache.build_id.as_str(),
                    final_dir.display(),
                    game_cache.size_bytes,
                    elapsed.as_secs_f64()
                );
                Ok(())
            }
            Err(e) => {
                // P1：失败日志必须携带根因（错误对象含退出码/tail）+ 耗时 + staging/final 双路径，
                // 不再让失败原因只存在于 steamcmd 私有日志（content_log.txt）或 DB 之外。
                log::error!(
                    "下载失败：game={} branch={} build={} final={} staging={} elapsed={:.1}s error={:?}",
                    game_cache.game_id.as_str(),
                    game_cache.branch_name.as_str(),
                    game_cache.build_id.as_str(),
                    path_str.as_str(),
                    staging_dir.display(),
                    elapsed.as_secs_f64(),
                    e
                );
                // 清理半成品 staging，释放暂存空间（§8.4）
                if staging_dir.exists() {
                    let _ = fs::remove_dir_all(&staging_dir).await;
                }
                // P4：失败原因落库（admin/controller 可见；成功后再清空）
                game_cache.status = GameCacheStatus::Unavailable;
                game_cache.last_error = Some(steam_error_summary(&e));
                let _ = self.game_cache_repos.save(&game_cache).await;
                Err(e)
            }
        }
    }

    /// P3（磁盘预检，见设计稿 §4.3）：返回拒绝原因（None=放行）。
    /// 在 spawn steamcmd 之前调用，避免「下载前预分配 16.4G 失败」这类必然失败白跑一次。
    async fn preflight_check(&self, game_cache: &GameCache) -> Option<String> {
        let available = match cache_root_free_bytes() {
            Some(a) => a,
            None => {
                // 无法定位缓存盘（如开发机/容器无 /server 挂载）→ 放行 + WARN，不误伤
                log::warn!(
                    "磁盘预检跳过：无法定位 {} 所在挂载点（cache_root_free_bytes=None）",
                    GAME_CACHE_SERVER_ROOT_PATH
                );
                return None;
            }
        };
        let needed = self.estimate_needed_bytes(game_cache).await;
        preflight_reject_reason(available, needed)
    }

    /// 需求大小估算（bytes）：① 同 (game,branch) 历史版本实测大小（更新/重复下载几乎总有）；
    /// ② 首下无参考 → steamcmd app_info_print 查询 size_on_disk（进程内缓存 12h）；失败返回 None。
    async fn estimate_needed_bytes(&self, game_cache: &GameCache) -> Option<u64> {
        if let Ok(versions) = self
            .game_cache_repos
            .get_versions(&game_cache.game_id, &game_cache.branch_name)
            .await
        {
            // 目标版本自身的残留记录 size=0（从未成功），天然被 size_bytes>0 过滤；
            // 若目标版本曾成功下载（size>0）而被清理，取其大小作参考同样合理。
            if let Some(known) = versions.iter().map(|v| v.size_bytes).max().filter(|&s| s > 0) {
                return Some(known);
            }
        }
        query_size_on_disk_cached(&game_cache.game_id).await
    }

    /// 实际的 steamcmd 下载流程（force_install_dir = staging 目录），
    /// 所有错误原样上抛给 download 统一处理（每处错误先打日志带操作上下文，P1 可观测性）。
    async fn run_download(
        &self,
        game_cache: &mut GameCache,
        install_dir: &std::path::Path,
    ) -> Result<(), SteamServiceError> {
        let install_dir_str = install_dir.to_str().unwrap_or_default();
        let mut child = Command::new("steamcmd")
            .args([
                "+force_install_dir",
                install_dir_str,
                "+login",
                "anonymous",
                "+app_update",
                game_cache.game_id.as_str(),
                "-beta",
                game_cache.branch_name.as_str(),
                "validate",
                "+quit",
            ])
            .stdout(std::process::Stdio::piped())
            .stderr(std::process::Stdio::piped())
            .spawn()
            .map_err(|e| {
                // spawn 失败（steamcmd 不在 PATH / 依赖缺失等）：带 install_dir 上下文打日志
                log::error!(
                    "steamcmd 启动失败：game={} branch={} install_dir={} err={e}",
                    game_cache.game_id.as_str(),
                    game_cache.branch_name.as_str(),
                    install_dir_str
                );
                SteamServiceError::IoError(e)
            })?;

        let stdout = child.stdout.take().unwrap();
        let stderr = child.stderr.take().unwrap();

        let mut out_lines = BufReader::new(stdout).lines();
        let mut err_lines = BufReader::new(stderr).lines();
        // P2：双流尾部缓冲（stdout 120 行 / stderr 60 行），失败时拼进错误上下文；
        // stdout 只保留 progress 实时解析，其余行进缓冲；stderr 不再继承丢失。
        let mut out_tail = TailBuffer::new(120);
        let mut err_tail = TailBuffer::new(60);

        game_cache.status = GameCacheStatus::Downloading;
        log::info!(
            "开始下载：game={} branch={} build={} staging={}",
            game_cache.game_id.as_str(),
            game_cache.branch_name.as_str(),
            game_cache.build_id.as_str(),
            install_dir.display()
        );
        self.game_cache_repos.save(game_cache).await?;

        // 双流并行消费（避免 stderr 管道缓冲写满阻塞子进程）；两路 EOF 后退出
        let mut out_done = false;
        let mut err_done = false;
        while !out_done || !err_done {
            tokio::select! {
                l = out_lines.next_line(), if !out_done => match l {
                    Ok(Some(line)) => {
                        if let Ok(Some(progress)) = progress_regex(&line) {
                            game_cache.download_progress = Some(progress);
                            self.game_cache_repos.save(game_cache).await?;
                        } else if steamcmd_error_line(&line) {
                            // steamcmd 的 ERROR!/Error! 原话实时转发进 node-agent.log（P2）
                            log::error!("steamcmd stdout: {}", line);
                        }
                        out_tail.push(line);
                    }
                    Ok(None) => out_done = true,
                    Err(e) => return Err(steamcmd_stream_read_error(
                        game_cache, install_dir_str, "stdout", e,
                    )),
                },
                l = err_lines.next_line(), if !err_done => match l {
                    Ok(Some(line)) => {
                        if steamcmd_error_line(&line) {
                            log::error!("steamcmd stderr: {}", line);
                        }
                        err_tail.push(line);
                    }
                    Ok(None) => err_done = true,
                    Err(e) => return Err(steamcmd_stream_read_error(
                        game_cache, install_dir_str, "stderr", e,
                    )),
                },
            }
        }

        let status = child.wait().await.map_err(|e| {
            log::error!(
                "等待 steamcmd 退出失败：game={} branch={} install_dir={} err={e}",
                game_cache.game_id.as_str(),
                game_cache.branch_name.as_str(),
                install_dir_str
            );
            SteamServiceError::IoError(e)
        })?;

        if !status.success() {
            // P2：把 steamcmd 输出尾部作为失败上下文带进错误（进日志 / 后续 last_error）
            let tail = format!(
                "stdout 尾部:\n{}stderr 尾部:\n{}",
                out_tail.tail(30),
                err_tail.tail(30)
            );
            return Err(SteamServiceError::DownloadError(
                game_cache.game_id.clone(),
                game_cache.branch_name.clone(),
                status,
                tail,
            ));
        }

        Ok(())
    }

    /// 实际的 steamcmd 卸载流程，所有错误原样上抛。
    async fn run_uninstall(&self, game_cache: &GameCache) -> Result<String, SteamServiceError> {
        let path = game_cache
            .path
            .clone()
            .ok_or(SteamServiceError::EmptyDownloadPathError)?;

        let mut child = Command::new("steamcmd")
            .args([
                "+login",
                "anonymous",
                "+force_install_dir",
                path.as_str(),
                "+app_uninstall",
                game_cache.game_id.as_str(),
                "+quit",
            ])
            .stdout(std::process::Stdio::piped())
            .spawn()?;

        let stdout = child.stdout.take().unwrap();
        let mut lines = BufReader::new(stdout).lines();
        log::info!("开始卸载：");
        while let Some(line) = lines.next_line().await? {
            log::info!("{}", line);
        }

        let status = child.wait().await?;

        if !status.success() {
            return Err(SteamServiceError::DownloadError(
                game_cache.game_id.clone(),
                game_cache.branch_name.clone(),
                status,
                // 卸载流程暂不捕获输出尾部（改动最小；后续如需再加）
                String::new(),
            ));
        }

        Ok(path)
    }
}

#[async_trait]
impl SteamService for SteamServiceClient {
    async fn start_download(
        &self,
        game_cache: GameCache,
    ) -> tokio::task::JoinHandle<Result<(), SteamServiceError>> {
        let repos = self.game_cache_repos.clone();
        tokio::spawn(async move {
            let client = SteamServiceClient {
                game_cache_repos: repos,
            };
            client.download(game_cache).await
        })
    }

    async fn uninstall(&self, mut game_cache: GameCache) -> Result<String, SteamServiceError> {
        let result = self.run_uninstall(&game_cache).await;

        match &result {
            Ok(_) => {
                game_cache.status = GameCacheStatus::Removed;
                let _ = self.game_cache_repos.save(&game_cache).await;
            }
            Err(_) => {
                game_cache.status = GameCacheStatus::Unavailable;
                let _ = self.game_cache_repos.save(&game_cache).await;
            }
        }

        result
    }

    async fn get_download_progress(
        &self,
        game_id: &String,
        branch_name: &String,
    ) -> anyhow::Result<Option<f32>> {
        let game_cache = self.game_cache_repos.get(game_id, branch_name).await?;
        if game_cache.is_none() {
            return Ok(None);
        }

        let game_cache = game_cache.unwrap();
        match game_cache.status {
            GameCacheStatus::Available => Ok(Some(100.0)),
            GameCacheStatus::Removed => Ok(None),
            GameCacheStatus::Unavailable => Err(anyhow!("Game不可获取")),
            GameCacheStatus::Downloading => Ok(game_cache.download_progress),
        }
    }
}

pub fn progress_regex(line: &str) -> anyhow::Result<Option<f32>> {
    let progress_regex = Regex::new(r"progress:\s+(\d+\.\d+)")?;
    if let Some(caps) = progress_regex.captures(line) {
        let progress: f32 = caps[1].parse()?;
        Ok(Some(progress))
    } else {
        Ok(None)
    }
}

/// 递归统计目录内容字节数（P2-B，供缓存磁盘记账）。
/// 迭代实现（栈），避免 async 递归；统计失败返回 Ok(0) 由调用方决定是否忽略。
async fn dir_size(root: &std::path::Path) -> std::io::Result<u64> {
    let mut total = 0u64;
    let mut stack = vec![root.to_path_buf()];
    while let Some(dir) = stack.pop() {
        let mut entries = fs::read_dir(&dir).await?;
        while let Some(entry) = entries.next_entry().await? {
            let ft = entry.file_type().await?;
            let path = entry.path();
            if ft.is_dir() {
                stack.push(path);
            } else if ft.is_file() {
                total += entry.metadata().await.map(|m| m.len()).unwrap_or(0);
            }
        }
    }
    Ok(total)
}

// ============================================================
// P2（下载可观测性，见 docs/game-cache-download-observability-design.md）：
// steamcmd 输出尾部缓冲 + 错误行判定 + 流读取错误统一包装
// ============================================================

/// steamcmd 输出尾部环形缓冲：只保留最近 cap 行，行数超限丢弃头部并计数。
/// 失败时由 `tail()` 产出错误上下文（限制进日志/错误消息的长度）。
struct TailBuffer {
    cap: usize,
    buf: std::collections::VecDeque<String>,
    dropped: usize,
}

impl TailBuffer {
    fn new(cap: usize) -> Self {
        Self {
            cap: cap.max(1),
            buf: std::collections::VecDeque::new(),
            dropped: 0,
        }
    }

    fn push(&mut self, line: String) {
        if self.buf.len() >= self.cap {
            self.buf.pop_front();
            self.dropped += 1;
        }
        self.buf.push_back(line);
    }

    /// 从尾部取最多 max_lines 行；因容量/取数截断时带「已截断 N 行」提示。
    fn tail(&self, max_lines: usize) -> String {
        let skip = self.buf.len().saturating_sub(max_lines);
        let mut out = String::new();
        if self.dropped > 0 || skip > 0 {
            out.push_str(&format!("[已截断 {} 行]\n", self.dropped + skip));
        }
        for line in self.buf.iter().skip(skip) {
            out.push_str(line);
            out.push('\n');
        }
        out
    }
}

/// steamcmd 错误行判定（stdout/stderr 中 `Error!`/`ERROR!` 原话即时转发进 node-agent.log）。
/// 实测 steamcmd 下载失败输出形如 `Error! App '294420' state is 0x202 after update job.`
/// 或 `ERROR! ...`（详见设计稿 §2 事故复盘）。
fn steamcmd_error_line(line: &str) -> bool {
    line.contains("Error!") || line.contains("ERROR!")
}

/// 流读取失败统一包装：先打上下文日志，再返回 IoError（下载任务无人消费 JoinHandle，
/// 失败反馈以日志 + DB Unavailable 为主）。
fn steamcmd_stream_read_error(
    game_cache: &GameCache,
    install_dir: &str,
    stream: &str,
    e: std::io::Error,
) -> SteamServiceError {
    log::error!(
        "读取 steamcmd {stream} 失败：game={} branch={} install_dir={} err={e}",
        game_cache.game_id.as_str(),
        game_cache.branch_name.as_str(),
        install_dir
    );
    SteamServiceError::IoError(e)
}

/// P4：错误转可读摘要（last_error 落库用，进 controller/admin 视图）：
/// - 预检拒绝：直接取文案（如「磁盘可用空间不足：需约 16.9 GiB…」）；
/// - DownloadError：退出码 + 输出尾部前 3 行（拼接、截断 300 字符）；
/// - 其余：Display。
fn steam_error_summary(e: &SteamServiceError) -> String {
    match e {
        SteamServiceError::PreflightRejected(msg) => msg.clone(),
        SteamServiceError::DownloadError(game, branch, status, tail) => {
            let mut s = format!("steamcmd 下载失败：game={game} branch={branch} exit={status}");
            // 跳过我们自己的标记行（"stdout 尾部:"/"stderr 尾部:"/"[已截断 N 行]"），只取实质输出
            let first: Vec<&str> = tail
                .lines()
                .map(str::trim)
                .filter(|l| {
                    !l.is_empty()
                        && !l.starts_with("stdout 尾部")
                        && !l.starts_with("stderr 尾部")
                        && !l.starts_with("[已截断")
                })
                .take(3)
                .collect();
            if !first.is_empty() {
                s.push_str("，输出尾部：");
                s.push_str(&first.join(" | "));
            }
            if s.chars().count() > 300 {
                s.truncate(300);
            }
            s
        }
        other => other.to_string(),
    }
}

// ============================================================
// P3（磁盘预检，见 docs/game-cache-download-observability-design.md §4.3）
// ============================================================

/// 缓存根目录所在挂载点的可用字节数；无法判定返回 None（调用方放行 + WARN）。
/// 选「挂载点与 /server 互为前缀」中最深的挂载点（/server 挂独立盘时取它，否则取 /）。
fn cache_root_free_bytes() -> Option<u64> {
    let root = GAME_CACHE_SERVER_ROOT_PATH;
    let disks = Disks::new();
    let mut chosen: Option<(u64, usize)> = None; // (available, mount_len)
    for d in disks.iter() {
        let mp = d.mount_point().to_string_lossy().to_string();
        let matches = root.starts_with(&mp) || mp.starts_with(root);
        if matches && chosen.map_or(true, |(_, n)| mp.len() > n) {
            chosen = Some((d.available_space(), mp.len()));
        }
    }
    chosen.map(|(available, _)| available)
}

/// 下载前磁盘预检判定（纯函数，可单测）：available=可用字节，needed=需求字节（None=未知）。
/// 返回拒绝原因（None=放行）。硬闸 1 GiB；需求已知时需额外 0.5 GiB 余量。
fn preflight_reject_reason(available: u64, needed: Option<u64>) -> Option<String> {
    const MIN_FREE_BYTES: u64 = 1 << 30; // 1 GiB
    const HEADROOM_BYTES: u64 = 512 << 20; // 0.5 GiB
    if available < MIN_FREE_BYTES {
        return Some(format!(
            "磁盘可用空间不足：仅剩 {}，低于最低阈值 1 GiB",
            fmt_bytes(available)
        ));
    }
    if let Some(n) = needed {
        let demand = n.saturating_add(HEADROOM_BYTES);
        if available < demand {
            return Some(format!(
                "磁盘可用空间不足：下载需约 {}（含 {} 余量），当前可用 {}",
                fmt_bytes(demand),
                fmt_bytes(HEADROOM_BYTES),
                fmt_bytes(available)
            ));
        }
    }
    None
}

/// 人类可读字节数（GiB/MiB）
fn fmt_bytes(n: u64) -> String {
    const G: u64 = 1 << 30;
    const M: u64 = 1 << 20;
    if n >= G {
        format!("{:.1} GiB", n as f64 / G as f64)
    } else if n >= M {
        format!("{:.0} MiB", n as f64 / M as f64)
    } else {
        format!("{n} B")
    }
}

/// 进程级缓存：appid -> (size_on_disk 总字节, 查询时间)；TTL 12h。
static SIZE_ON_DISK_CACHE: OnceLock<Mutex<HashMap<String, (u64, SystemTime)>>> = OnceLock::new();
const SIZE_CACHE_TTL: Duration = Duration::from_secs(12 * 3600);

/// 带缓存的 app_info_print 大小查询（先查缓存，避免每次下载都跑一次 steamcmd）。
async fn query_size_on_disk_cached(app_id: &str) -> Option<u64> {
    {
        let cache = SIZE_ON_DISK_CACHE.get_or_init(|| Mutex::new(HashMap::new()));
        let guard = cache.lock().unwrap_or_else(|p| p.into_inner());
        if let Some((bytes, at)) = guard.get(app_id) {
            if at.elapsed().ok()? < SIZE_CACHE_TTL {
                return Some(*bytes);
            }
        }
    }
    let bytes = query_size_on_disk(app_id).await;
    if let Some(b) = bytes {
        let cache = SIZE_ON_DISK_CACHE.get_or_init(|| Mutex::new(HashMap::new()));
        let mut guard = cache.lock().unwrap_or_else(|p| p.into_inner());
        guard.insert(app_id.to_string(), (b, SystemTime::now()));
    }
    bytes
}

/// steamcmd `+app_info_print` 查询 App 的安装需求大小（各 depot `size_on_disk` 求和近似）。
/// 失败/无数据返回 None（调用方放行）。仅首下无参考时调用，成功结果缓存 12h。
async fn query_size_on_disk(app_id: &str) -> Option<u64> {
    log::info!("查询 App {} 磁盘需求大小（app_info_print）…", app_id);
    let mut child = Command::new("steamcmd")
        .args(["+login", "anonymous", "+app_info_print", app_id, "+quit"])
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::null())
        .spawn()
        .ok()?;
    let stdout = child.stdout.take()?;
    let mut text = String::new();
    BufReader::new(stdout).read_to_string(&mut text).await.ok()?;
    let status = child.wait().await.ok()?;
    if !status.success() {
        log::warn!("app_info_print 失败（App {app_id}），跳过大小预检");
        return None;
    }
    let total = parse_total_size_on_disk(&text);
    if let Some(t) = total {
        log::info!("App {app_id} 磁盘需求 ≈ {}", fmt_bytes(t));
    }
    total
}

/// 解析 app_info_print 输出（VDF 文本）：收集所有 `"size_on_disk" "N"`（字节）求和。
/// 无匹配返回 None。
fn parse_total_size_on_disk(output: &str) -> Option<u64> {
    let re = Regex::new(r#""size_on_disk"\s+"(\d+)""#).ok()?;
    let mut total: u64 = 0;
    let mut found = false;
    for cap in re.captures_iter(output) {
        if let Some(v) = cap.get(1).and_then(|m| m.as_str().parse::<u64>().ok()) {
            total = total.saturating_add(v);
            found = true;
        }
    }
    found.then_some(total)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn tail_buffer_truncates_head_and_counts() {
        let mut t = TailBuffer::new(3);
        for i in 0..10 {
            t.push(format!("line{i}"));
        }
        assert_eq!(t.dropped, 7, "容量 3、压入 10 行应丢弃头部 7 行");
        let tail = t.tail(2);
        assert!(tail.contains("line8"), "尾部应含倒数第二行: {tail}");
        assert!(tail.contains("line9"), "尾部应含最后一行: {tail}");
        assert!(!tail.contains("line5"), "早期行应被截掉: {tail}");
        assert!(tail.contains("已截断"), "截断应有提示: {tail}");
    }

    #[test]
    fn tail_buffer_short_no_truncate_marker() {
        let mut t = TailBuffer::new(10);
        t.push("a".to_string());
        t.push("b".to_string());
        let tail = t.tail(5);
        assert_eq!(tail, "a\nb\n");
        assert!(!tail.contains("已截断"));
    }

    #[test]
    fn error_line_detection() {
        // 事故现场（见设计稿 §2）：294420 磁盘不足 steamcmd stdout 只有这一句
        assert!(steamcmd_error_line(
            "Error! App '294420' state is 0x202 after update job."
        ));
        assert!(steamcmd_error_line("ERROR! Not enough disk space"));
        // 正常进度/内容行不应误报
        assert!(!steamcmd_error_line("Update state (0x0) unknown, progress: 0.00 (0 / 0)"));
        assert!(!steamcmd_error_line("Downloading 105 chunks for depot 1006"));
        assert!(!steamcmd_error_line("Redirecting stderr to '/root/.local/share/Steam/logs/stderr.txt'"));
    }

    #[test]
    fn preflight_rejects_only_when_short() {
        let g = 1u64 << 30;
        // 硬闸：可用 < 1 GiB 无条件拒
        assert!(preflight_reject_reason(g - 1, None).is_some());
        // 需求已知且不足（16.4G 需求 + 0.5G 余量 > 11G 可用 → 拒，即 294420 事故场景）
        assert!(preflight_reject_reason(11 * g, Some(16 * g + 400 * (1 << 20))).is_some());
        // 充足 → 放行
        assert_eq!(preflight_reject_reason(20 * g, Some(16 * g)), None);
        // 需求未知（首下查询失败）→ 仅硬闸后放行
        assert_eq!(preflight_reject_reason(5 * g, None), None);
        // 拒绝文案带两侧数值，可读
        let msg = preflight_reject_reason(11 * g, Some(16 * g)).unwrap();
        assert!(msg.contains("GiB"), "文案应含数值: {msg}");
    }

    #[test]
    fn parse_size_on_disk_sums_depot_values() {
        // 近似 app_info_print 的 VDF 输出片段
        let sample = r#"stuff
"depots"
{
    "1006"
    {
        "name" "7d2d dedicated"
        "size_on_disk" "3485494806"
        "maxsize" "3485494806"
    }
    "294420"
    {
        "size_on_disk" "14000000000"
    }
}"#;
        assert_eq!(parse_total_size_on_disk(sample), Some(17485494806u64));
        assert_eq!(parse_total_size_on_disk("no depot sizes here"), None);
        assert_eq!(parse_total_size_on_disk(""), None);
    }

    #[test]
    fn error_summary_is_readable() {
        // 预检拒绝：直接文案（P4 落库给 admin 看的核心场景）
        let pre = SteamServiceError::PreflightRejected("磁盘可用空间不足：需约 16.9 GiB".into());
        assert_eq!(steam_error_summary(&pre), "磁盘可用空间不足：需约 16.9 GiB");
        // DownloadError：退出码 + tail 前几行
        let dl = SteamServiceError::DownloadError(
            "294420".into(),
            "public".into(),
            exit_status_for_test(10),
            "stdout 尾部:\n[已截断 30 行]\nError! App '294420' state is 0x202 after update job.\n".into(),
        );
        let s = steam_error_summary(&dl);
        assert!(s.contains("exit"), "应含退出码: {s}");
        assert!(s.contains("0x202"), "应含 steamcmd 原话: {s}");
        assert!(!s.contains("已截断"), "标记行不应进摘要: {s}");
    }

    /// 生成指定退出码的 ExitStatus（测试用；ExitStatus 无公开构造，需跑真实子进程）。
    fn exit_status_for_test(code: i32) -> std::process::ExitStatus {
        #[cfg(unix)]
        {
            std::process::Command::new("sh")
                .args(["-c", &format!("exit {code}")])
                .status()
                .unwrap()
        }
        #[cfg(windows)]
        {
            std::process::Command::new("cmd")
                .args(["/C", "exit", &code.to_string()])
                .status()
                .unwrap()
        }
    }
}
