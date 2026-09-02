//! node_agent 日志 tail 端点（P2，见 docs/node-agent-logging-design.md §4.1b）。
//!
//! `GET /v1/agent/logs/tail?lines=&level=&keyword=&offset=`：
//! - 只读固定日志目录下 `node-agent.log*`，**不接受任意 path 参数**（无路径穿越面）；
//! - 鉴权：Bearer agent_logs token（agent 级，不绑实例）；
//! - 无 `offset` = 首查/重置：读文件尾部窗口（最多 `SCAN_BYTES`），过滤后返回最后 `lines` 行；
//! - 带 `offset` = 增量：从 offset 读到 EOF，返回新增匹配行（日志整行 append，EOF 恒为行边界）；
//! - `offset` 失效（日志轮换后新文件变小）→ `rotated: true`，前端清零重查；
//! - 单次响应体 ≤ `MAX_RESPONSE_BYTES`（超限截断，`truncated: true`）。

use std::path::{Path, PathBuf};
use std::sync::Arc;

use axum::extract::{Query, RawQuery, State};
use axum::http::HeaderMap;
use axum::response::{IntoResponse, Response};
use axum::Json;
use serde::Deserialize;
use serde_json::json;

use super::auth::verify_agent_logs_token;
use super::FileServer;

/// 首查尾部扫描窗口：一次最多向后读 4MB 找匹配行
const SCAN_BYTES: u64 = 4 * 1024 * 1024;
/// 单次响应体字节上限
const MAX_RESPONSE_BYTES: usize = 512 * 1024;
/// 行数默认与上限
const DEFAULT_LINES: usize = 300;
const MAX_LINES: usize = 2000;

#[derive(Deserialize)]
pub struct TailQuery {
    lines: Option<usize>,
    /// info / warn / error；空 = 全部。warn 含 warn+error，info 含 info/warn/error
    level: Option<String>,
    keyword: Option<String>,
    /// 增量游标（上一响应的字节 offset）；无 = 首查尾部
    offset: Option<u64>,
}

/// 行是否通过级别过滤：行文本含 `[LEVEL]` 标记
fn level_pass(line: &str, level: &str) -> bool {
    match level {
        "error" => line.contains("[ERROR]"),
        "warn" => line.contains("[WARN]") || line.contains("[ERROR]"),
        "info" => {
            line.contains("[INFO]")
                || line.contains("[WARN]")
                || line.contains("[ERROR]")
                || line.contains("[DEBUG]") // info 用户通常也期望 debug 辅助排障
        }
        _ => true,
    }
}

fn keyword_pass(line: &str, keyword: &str) -> bool {
    keyword.is_empty() || line.to_ascii_lowercase().contains(&keyword.to_ascii_lowercase())
}

/// 读取文件 [start, end) 区间的行；首行若在行中间（残差）自动丢弃（日志整行 append，
/// 起点残差只可能来自尾部窗口起点）。返回 (行列表, 是否因窗口截断)。
fn read_lines_at(file: &std::fs::File, start: u64, end: u64) -> Result<(Vec<String>, bool), String> {
    use std::io::{Read, Seek, SeekFrom};
    let mut f = file;
    let mut buf = vec![0u8; (end - start) as usize];
    f.seek(SeekFrom::Start(start)).map_err(|e| format!("seek: {e}"))?;
    f.read_exact(&mut buf).map_err(|e| format!("read: {e}"))?;
    let text = String::from_utf8_lossy(&buf);
    let truncated = start > 0; // 文件更长，窗口起点之前还有内容
    let mut lines: Vec<String> = Vec::new();
    for (i, raw) in text.split('\n').enumerate() {
        if i == 0 && start > 0 {
            continue; // 窗口起点残差行丢弃
        }
        if i + 1 == text.split('\n').count() && raw.is_empty() {
            continue; // 末尾空串（最后换行后无内容）
        }
        if !raw.is_empty() {
            lines.push(raw.to_string());
        }
    }
    Ok((lines, truncated))
}

/// 主日志文件路径（仅固定目录内文件名）
fn main_log_path(log_dir: &Path) -> PathBuf {
    log_dir.join("node-agent.log")
}

pub async fn tail(
    State(srv): State<Arc<FileServer>>,
    Query(q): Query<TailQuery>,
    headers: HeaderMap,
    RawQuery(raw): RawQuery,
) -> Response {
    let log_dir = match &srv.log_dir {
        Some(d) => d.clone(),
        None => {
            return Json(json!({ "error": "agent 日志未启用（未配置 NODE_AGENT_LOG_DIR）" }))
                .into_response();
        }
    };
    if !log_dir.is_dir() {
        return Json(json!({ "error": "日志目录不存在，等待 agent 首次写入日志" })).into_response();
    }

    // 鉴权：agent_logs scope
    let token = headers
        .get(axum::http::header::AUTHORIZATION)
        .and_then(|v| v.to_str().ok())
        .and_then(|v| v.strip_prefix("Bearer "))
        .map(|s| s.to_string())
        .or_else(|| token_from_query(raw.as_deref().unwrap_or("")));
    match token {
        Some(t) => {
            if let Err(e) = verify_agent_logs_token(&t, &srv.secret) {
                return unauthorized(e);
            }
        }
        None => return unauthorized("missing token".to_string()),
    }

    let path = main_log_path(&log_dir);
    let file = match std::fs::File::open(&path) {
        Ok(f) => f,
        Err(_) => {
            return Json(json!({ "error": "暂无日志文件（agent 启动后生成 node-agent.log）" }))
                .into_response();
        }
    };
    let size = match file.metadata() {
        Ok(m) => m.len(),
        Err(e) => return internal(format!("stat log file: {e}")),
    };
    if size == 0 {
        return Json(json!({ "text": "", "offset": 0, "truncated": false, "rotated": false }))
            .into_response();
    }

    let max_lines = q.lines.unwrap_or(DEFAULT_LINES).clamp(1, MAX_LINES);
    let level = q.level.clone().unwrap_or_default();
    let keyword = q.keyword.clone().unwrap_or_default();

    // 增量模式：offset 在行边界（日志整行 append，EOF 恒为行边界）
    if let Some(offset) = q.offset {
        if offset >= size {
            // offset 未超当前 EOF：无新增；超了说明轮换/截断 → rotated
            return Json(json!({
                "text": "",
                "offset": size,
                "truncated": false,
                "rotated": offset > size,
            }))
            .into_response();
        }
        return read_range(&file, offset, size, &level, &keyword, max_lines).into_response();
    }

    // 首查尾部窗口
    let start = size.saturating_sub(SCAN_BYTES);
    read_range(&file, start, size, &level, &keyword, max_lines).into_response()
}

/// 读 [start,end) 行，过滤，返回最多 max_lines 行 + 新 offset(=end) + truncated
fn read_range(
    file: &std::fs::File,
    start: u64,
    end: u64,
    level: &str,
    keyword: &str,
    max_lines: usize,
) -> axum::Json<serde_json::Value> {
    match read_lines_at(file, start, end) {
        Ok((all_lines, window_truncated)) => {
            // 保留匹配行，取最后 max_lines 行（时间序）
            let matched: Vec<&String> = all_lines
                .iter()
                .filter(|l| level_pass(l, level) && keyword_pass(l, keyword))
                .collect();
            let head = matched.len().saturating_sub(max_lines);
            let shown: Vec<&String> = matched[head..].to_vec();
            let line_truncated = matched.len() > max_lines;

            // 组装 text（累计字节超上限截断）
            let mut text = String::new();
            let mut over = false;
            for l in &shown {
                if text.len() + l.len() + 1 > MAX_RESPONSE_BYTES {
                    over = true;
                    break;
                }
                text.push_str(l);
                text.push('\n');
            }
            axum::Json(json!({
                "text": text,
                "offset": end,
                "truncated": window_truncated || line_truncated || over,
                "rotated": false,
            }))
        }
        Err(e) => axum::Json(json!({ "error": e })),
    }
}

fn unauthorized(msg: String) -> Response {
    (
        axum::http::StatusCode::UNAUTHORIZED,
        Json(json!({ "error": format!("unauthorized: {msg}") })),
    )
        .into_response()
}

fn internal(msg: String) -> Response {
    (
        axum::http::StatusCode::INTERNAL_SERVER_ERROR,
        Json(json!({ "error": msg })),
    )
        .into_response()
}

fn token_from_query(raw: &str) -> Option<String> {
    raw.split('&')
        .find_map(|pair| {
            let mut it = pair.splitn(2, '=');
            let key = it.next()?;
            if key == "token" {
                Some(it.next().unwrap_or("").to_string())
            } else {
                None
            }
        })
}
