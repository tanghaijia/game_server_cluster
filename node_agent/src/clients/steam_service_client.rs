use std::path::PathBuf;
use std::println;
use std::sync::Arc;

use anyhow::anyhow;
use regex::Regex;
use tokio::fs;
use tokio::io::{AsyncBufReadExt, BufReader};
use tokio::process::Command;
use tonic::async_trait;

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
                game_cache.status = GameCacheStatus::Unavailable;
                let _ = self.game_cache_repos.save(&game_cache).await;
                Err(e)
            }
        }
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
}
