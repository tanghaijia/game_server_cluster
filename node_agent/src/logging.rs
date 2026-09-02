//! 双输出日志：stderr + 本地滚动文件（P1，见 docs/node-agent-logging-design.md）。
//!
//! 背景：node_agent 原用 `env_logger` 仅写 stderr，Web（admin 日志视图）无文件可读。
//! 本模块实现 `log::Log`：同一行同时写 stderr 与 `<NODE_AGENT_LOG_DIR>/node-agent.log`，
//! 统一格式 `YYYY-MM-DDTHH:MM:SS.mmmZ [LEVEL] target: message`，按大小滚动（默认 10MB × 5），
//! 零新增依赖（仅 log + chrono）。
//!
//! 级别由 `RUST_LOG` 控制（off|error|warn|info|debug|trace，默认 info），与原 env_logger 一致。
//! 文件打开失败 / 目录不可写时自动降级：仅写 stderr，不阻断业务（日志是尽力而为）。

use std::fs::{self, File, OpenOptions};
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::Mutex;

use chrono::Utc;
use log::{Level, LevelFilter, Metadata, Record};

/// 单个日志文件大小上限（10MB）
const MAX_FILE_BYTES: u64 = 10 * 1024 * 1024;
/// 保留的轮换文件份数（当前 + 历史 = MAX_FILES 个文件）
const MAX_FILES: usize = 5;
/// 主日志文件名
const FILE_NAME: &str = "node-agent.log";

/// 滚动文件 logger：同一日志行写 stderr + 文件。
pub struct RollingFileLogger {
    dir: PathBuf,
    max_bytes: u64,
    max_files: usize,
    max_level: LevelFilter,
    /// 懒打开的文件句柄；None = 尚未打开或打开失败（仅 stderr 输出）
    file: Mutex<Option<File>>,
}

impl RollingFileLogger {
    pub fn new(dir: PathBuf, max_bytes: u64, max_files: usize, max_level: LevelFilter) -> Self {
        Self { dir, max_bytes, max_files, max_level, file: Mutex::new(None) }
    }

    /// 组装一行文本：`2026-09-02T14:00:00.123Z [INFO] target: message\n`
    fn format_line(&self, record: &Record) -> String {
        let ts = Utc::now().format("%Y-%m-%dT%H:%M:%S%.3fZ");
        format!("{} [{}] {}: {}\n", ts, level_tag(record.level()), record.target(), record.args())
    }

    /// 轮换：删除最老份，历史文件后移一位，主文件清空由下次 open 重建。
    /// 调用前必须先 drop 旧的句柄（否则 rename 会因文件占用失败，Windows 下尤其如此）。
    fn rotate_files(&self) {
        let _ = fs::remove_file(self.dir.join(format!("{FILE_NAME}.{}", self.max_files - 1)));
        for i in (1..self.max_files).rev() {
            let from = if i == 1 {
                self.dir.join(FILE_NAME)
            } else {
                self.dir.join(format!("{FILE_NAME}.{}", i - 1))
            };
            let to = self.dir.join(format!("{FILE_NAME}.{i}"));
            let _ = fs::rename(from, to);
        }
    }

    fn write_line(&self, line: &str) {
        // stderr 恒写（保底通道，不依赖文件可用性）
        let _ = std::io::stderr().write_all(line.as_bytes());

        let mut guard = self.file.lock().unwrap_or_else(|p| p.into_inner());
        let need_reopen = match guard.as_ref() {
            None => true, // 尚未打开 / 上次失败，尝试（重新）打开
            Some(f) => f.metadata().map(|m| m.len() >= self.max_bytes).unwrap_or(true),
        };
        if need_reopen {
            // 先 drop 旧句柄（可能指向待轮换文件），再轮换，再打开新文件
            *guard = None;
            let _ = fs::create_dir_all(&self.dir);
            self.rotate_files();
            *guard = OpenOptions::new()
                .create(true)
                .append(true)
                .open(self.dir.join(FILE_NAME))
                .ok();
        }
        if let Some(f) = guard.as_mut() {
            let _ = f.write_all(line.as_bytes());
        }
    }
}

impl log::Log for RollingFileLogger {
    fn enabled(&self, metadata: &Metadata) -> bool {
        metadata.level() <= self.max_level
    }

    fn log(&self, record: &Record) {
        if !self.enabled(record.metadata()) {
            return;
        }
        self.write_line(&self.format_line(record));
    }

    fn flush(&self) {
        if let Ok(mut guard) = self.file.lock() {
            if let Some(f) = guard.as_mut() {
                let _ = f.flush();
            }
        }
    }
}

/// 解析 `RUST_LOG` 环境变量（与 env_logger 关键字一致，默认 info）
fn parse_level(raw: &str) -> LevelFilter {
    match raw.trim().to_ascii_lowercase().as_str() {
        "off" => LevelFilter::Off,
        "error" => LevelFilter::Error,
        "warn" => LevelFilter::Warn,
        "debug" => LevelFilter::Debug,
        "trace" => LevelFilter::Trace,
        _ => LevelFilter::Info,
    }
}

fn level_tag(l: Level) -> &'static str {
    match l {
        Level::Error => "ERROR",
        Level::Warn => "WARN",
        Level::Info => "INFO",
        Level::Debug => "DEBUG",
        Level::Trace => "TRACE",
    }
}

/// 初始化双输出日志（幂等：已设置过 logger 时静默忽略）。
/// `log_dir` 来自 `NODE_AGENT_LOG_DIR`（未设置时 main 给默认值）。
pub fn init_logging(log_dir: impl AsRef<Path>) {
    let level = parse_level(std::env::var("RUST_LOG").as_deref().unwrap_or("info"));
    let logger = RollingFileLogger::new(
        log_dir.as_ref().to_path_buf(),
        MAX_FILE_BYTES,
        MAX_FILES,
        level,
    );
    if log::set_boxed_logger(Box::new(logger)).is_ok() {
        log::set_max_level(level);
    }
}
