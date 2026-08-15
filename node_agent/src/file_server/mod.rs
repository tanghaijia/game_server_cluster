pub mod auth;
pub mod path;

use std::path::{Path, PathBuf};
use std::sync::Arc;

use axum::body::Body;
use axum::extract::{Path as AxumPath, Query, RawQuery, State};
use axum::http::{header, HeaderMap, StatusCode};
use axum::response::{IntoResponse, Response};
use axum::routing::{get, post, put};
use axum::{Json, Router};
use chrono::{DateTime, Utc};
use futures_util::{StreamExt, TryStreamExt};
use http_body::Frame;
use http_body_util::StreamBody;
use serde::{Deserialize, Serialize};
use tokio::io::AsyncWriteExt;
use tokio_util::io::ReaderStream;

use crate::ports::GameInstanceRepository;

use self::auth::verify_token;
use self::path::{ensure_no_symlink_escape, resolve_within};

/// 文本接口大小上限（设计文档：仅文本 ≤2MB）
const TEXT_SIZE_LIMIT: u64 = 2 * 1024 * 1024;

/// 实例文件管理 HTTP 服务（M1，见 docs/file-manager-design.md）
pub struct FileServer {
    instance_repo: Arc<dyn GameInstanceRepository>,
    secret: Vec<u8>,
    data_root_override: Option<PathBuf>, // 调试/测试用（环境变量 FILE_DATA_ROOT_OVERRIDE）
}

impl FileServer {
    pub fn new(
        instance_repo: Arc<dyn GameInstanceRepository>,
        secret: Vec<u8>,
        data_root_override: Option<PathBuf>,
    ) -> Self {
        Self { instance_repo, secret, data_root_override }
    }

    pub fn router(self: Arc<Self>) -> Router {
        Router::new()
            .route("/v1/instances/{instance_id}/files", get(list).delete(delete_file))
            .route("/v1/instances/{instance_id}/files/content", get(download).put(upload))
            .route("/v1/instances/{instance_id}/files/text", get(read_text).put(write_text))
            .route("/v1/instances/{instance_id}/files/rename", post(rename_file))
            .route("/v1/instances/{instance_id}/files/mkdir", post(mkdir))
            .with_state(self)
    }

    /// 授权：校验 Bearer 文件会话 token（绑定实例），返回该实例 data 根目录。
    /// query 中允许 ?token= 兜底（供 <a> 直接下载等无法带 header 的场景）。
    async fn authorize(
        &self,
        headers: &HeaderMap,
        raw_query: &str,
        instance_id: &str,
    ) -> Result<PathBuf, FileError> {
        let token = headers
            .get(header::AUTHORIZATION)
            .and_then(|v| v.to_str().ok())
            .and_then(|v| v.strip_prefix("Bearer "))
            .map(|s| s.to_string())
            .or_else(|| token_from_query(raw_query))
            .ok_or(FileError::Unauthorized)?;
        verify_token(&token, &self.secret, instance_id).map_err(|_| FileError::Unauthorized)?;
        self.data_root(instance_id).await
    }

    async fn data_root(&self, instance_id: &str) -> Result<PathBuf, FileError> {
        if let Some(root) = &self.data_root_override {
            return Ok(root.clone());
        }
        let inst = self
            .instance_repo
            .get(instance_id.to_string())
            .await
            .map_err(|e| FileError::NotFound(format!("instance not found: {e}")))?;
        Ok(inst.host_data_path.as_ref().to_path_buf())
    }
}

// ------------------------- 查询参数 / 响应 -------------------------

#[derive(Deserialize)]
struct PathQuery {
    path: Option<String>,
}

#[derive(Deserialize)]
struct RenameQuery {
    from: String,
    to: String,
}

#[derive(Deserialize)]
struct WriteTextBody {
    content: String,
}

#[derive(Serialize)]
struct FileEntry {
    name: String,
    is_dir: bool,
    size: u64,
    modified: String,
}

// ------------------------- handlers -------------------------

/// GET /v1/instances/{id}/files?path= 目录列表
async fn list(
    State(srv): State<Arc<FileServer>>,
    AxumPath(instance_id): AxumPath<String>,
    Query(q): Query<PathQuery>,
    headers: HeaderMap,
    RawQuery(raw): RawQuery,
) -> Result<Json<serde_json::Value>, FileError> {
    let root = srv.authorize(&headers, raw.as_deref().unwrap_or(""), &instance_id).await?;
    let dir = match &q.path {
        Some(p) => resolve_within(&root, p)?,
        None => root.clone(),
    };

    let meta = tokio::fs::metadata(&dir).await.map_err(FileError::not_found)?;
    if !meta.is_dir() {
        return Err(FileError::BadRequest("not a directory".to_string()));
    }

    let mut entries = Vec::new();
    let mut rd = tokio::fs::read_dir(&dir).await.map_err(FileError::io)?;
    while let Some(entry) = rd.next_entry().await.map_err(FileError::io)? {
        let ft = entry.file_type().await.map_err(FileError::io)?;
        if ft.is_symlink() {
            continue; // 跳过符号链接，防逃逸
        }
        let md = entry.metadata().await.map_err(FileError::io)?;
        let modified: DateTime<Utc> = md.modified().map(|t| t.into()).unwrap_or_else(|_| Utc::now());
        entries.push(FileEntry {
            name: entry.file_name().to_string_lossy().into_owned(),
            is_dir: ft.is_dir(),
            size: md.len(),
            modified: modified.to_rfc3339(),
        });
    }
    entries.sort_by(|a, b| b.is_dir.cmp(&a.is_dir).then(a.name.cmp(&b.name)));

    Ok(Json(serde_json::json!({ "path": dir.display().to_string(), "entries": entries })))
}

/// GET /v1/instances/{id}/files/content?path= 下载（流式）
async fn download(
    State(srv): State<Arc<FileServer>>,
    AxumPath(instance_id): AxumPath<String>,
    Query(q): Query<PathQuery>,
    headers: HeaderMap,
    RawQuery(raw): RawQuery,
) -> Result<Response, FileError> {
    let root = srv.authorize(&headers, raw.as_deref().unwrap_or(""), &instance_id).await?;
    let path = q.path.ok_or_else(|| FileError::BadRequest("path is required".into()))?;
    let target = resolve_within(&root, &path)?;
    ensure_no_symlink_escape(&root, &target)?;

    let meta = tokio::fs::metadata(&target).await.map_err(FileError::not_found)?;
    if meta.is_dir() {
        return Err(FileError::BadRequest("cannot download a directory".to_string()));
    }

    let file = tokio::fs::File::open(&target).await.map_err(FileError::io)?;
    let name = target.file_name().unwrap_or_default().to_string_lossy().into_owned();
    // http-body 1.x 的 Data 是 Frame<Bytes>，ReaderStream 产出 Bytes，需包装
    let stream = ReaderStream::new(file).map_ok(Frame::data);
    let body = Body::new(StreamBody::new(stream));

    Response::builder()
        .header(header::CONTENT_TYPE, guess_mime(&name))
        .header(header::CONTENT_LENGTH, meta.len())
        .header(header::CONTENT_DISPOSITION, format!("attachment; filename=\"{name}\" "))
        .body(body)
        .map_err(FileError::internal)
}

/// PUT /v1/instances/{id}/files/content?path= 上传（流式）
async fn upload(
    State(srv): State<Arc<FileServer>>,
    AxumPath(instance_id): AxumPath<String>,
    Query(q): Query<PathQuery>,
    headers: HeaderMap,
    RawQuery(raw): RawQuery,
    body: Body,
) -> Result<Json<serde_json::Value>, FileError> {
    let root = srv.authorize(&headers, raw.as_deref().unwrap_or(""), &instance_id).await?;
    let path = q.path.ok_or_else(|| FileError::BadRequest("path is required".into()))?;
    let target = resolve_within(&root, &path)?;

    if let Some(parent) = target.parent() {
        let md = tokio::fs::metadata(parent).await.map_err(FileError::io)?;
        if !md.is_dir() {
            return Err(FileError::BadRequest("parent is not a directory".to_string()));
        }
    }

    let mut file = tokio::fs::File::create(&target).await.map_err(FileError::io)?;
    let mut stream = body.into_data_stream();
    while let Some(chunk) = stream.next().await {
        let chunk = chunk.map_err(FileError::internal)?;
        file.write_all(&chunk).await.map_err(FileError::io)?;
    }
    file.flush().await.map_err(FileError::io)?;

    Ok(Json(serde_json::json!({ "message": "uploaded", "path": path })))
}

/// DELETE /v1/instances/{id}/files?path= 删除文件/空目录
async fn delete_file(
    State(srv): State<Arc<FileServer>>,
    AxumPath(instance_id): AxumPath<String>,
    Query(q): Query<PathQuery>,
    headers: HeaderMap,
    RawQuery(raw): RawQuery,
) -> Result<Json<serde_json::Value>, FileError> {
    let root = srv.authorize(&headers, raw.as_deref().unwrap_or(""), &instance_id).await?;
    let path = q.path.ok_or_else(|| FileError::BadRequest("path is required".into()))?;
    let target = resolve_within(&root, &path)?;
    ensure_no_symlink_escape(&root, &target)?;

    let meta = tokio::fs::metadata(&target).await.map_err(FileError::not_found)?;
    if meta.is_dir() {
        tokio::fs::remove_dir(&target).await.map_err(FileError::io)?;
    } else {
        tokio::fs::remove_file(&target).await.map_err(FileError::io)?;
    }
    Ok(Json(serde_json::json!({ "message": "deleted", "path": path })))
}

/// POST /v1/instances/{id}/files/rename?from=&to= 重命名/移动
async fn rename_file(
    State(srv): State<Arc<FileServer>>,
    AxumPath(instance_id): AxumPath<String>,
    Query(q): Query<RenameQuery>,
    headers: HeaderMap,
    RawQuery(raw): RawQuery,
) -> Result<Json<serde_json::Value>, FileError> {
    let root = srv.authorize(&headers, raw.as_deref().unwrap_or(""), &instance_id).await?;
    let from = resolve_within(&root, &q.from)?;
    let to = resolve_within(&root, &q.to)?;
    ensure_no_symlink_escape(&root, &from)?;
    if to.exists() {
        return Err(FileError::BadRequest("target already exists".to_string()));
    }
    tokio::fs::rename(&from, &to).await.map_err(FileError::io)?;
    Ok(Json(serde_json::json!({ "message": "renamed" })))
}

/// POST /v1/instances/{id}/files/mkdir?path= 新建目录
async fn mkdir(
    State(srv): State<Arc<FileServer>>,
    AxumPath(instance_id): AxumPath<String>,
    Query(q): Query<PathQuery>,
    headers: HeaderMap,
    RawQuery(raw): RawQuery,
) -> Result<Json<serde_json::Value>, FileError> {
    let root = srv.authorize(&headers, raw.as_deref().unwrap_or(""), &instance_id).await?;
    let path = q.path.ok_or_else(|| FileError::BadRequest("path is required".into()))?;
    let target = resolve_within(&root, &path)?;
    tokio::fs::create_dir_all(&target).await.map_err(FileError::io)?;
    Ok(Json(serde_json::json!({ "message": "created", "path": path })))
}

/// GET /v1/instances/{id}/files/text?path= 读文本（≤2MB）
async fn read_text(
    State(srv): State<Arc<FileServer>>,
    AxumPath(instance_id): AxumPath<String>,
    Query(q): Query<PathQuery>,
    headers: HeaderMap,
    RawQuery(raw): RawQuery,
) -> Result<Json<serde_json::Value>, FileError> {
    let root = srv.authorize(&headers, raw.as_deref().unwrap_or(""), &instance_id).await?;
    let path = q.path.ok_or_else(|| FileError::BadRequest("path is required".into()))?;
    let target = resolve_within(&root, &path)?;
    ensure_no_symlink_escape(&root, &target)?;

    let meta = tokio::fs::metadata(&target).await.map_err(FileError::not_found)?;
    if meta.is_dir() {
        return Err(FileError::BadRequest("cannot read a directory".to_string()));
    }
    if meta.len() > TEXT_SIZE_LIMIT {
        return Err(FileError::TooLarge);
    }
    let content = tokio::fs::read_to_string(&target).await.map_err(FileError::io)?;
    Ok(Json(serde_json::json!({ "path": path, "content": content })))
}

/// PUT /v1/instances/{id}/files/text?path= 写文本（≤2MB）
async fn write_text(
    State(srv): State<Arc<FileServer>>,
    AxumPath(instance_id): AxumPath<String>,
    Query(q): Query<PathQuery>,
    headers: HeaderMap,
    RawQuery(raw): RawQuery,
    Json(body): Json<WriteTextBody>,
) -> Result<Json<serde_json::Value>, FileError> {
    let root = srv.authorize(&headers, raw.as_deref().unwrap_or(""), &instance_id).await?;
    let path = q.path.ok_or_else(|| FileError::BadRequest("path is required".into()))?;
    let target = resolve_within(&root, &path)?;

    if body.content.len() as u64 > TEXT_SIZE_LIMIT {
        return Err(FileError::TooLarge);
    }
    tokio::fs::write(&target, body.content).await.map_err(FileError::io)?;
    Ok(Json(serde_json::json!({ "message": "saved", "path": path })))
}

// ------------------------- 工具 -------------------------

/// 从 query string 提取 token 参数（JWT 字符集无需 URL 解码）
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

fn guess_mime(name: &str) -> &'static str {
    let ext = name.split(|c| c == '.').last().unwrap_or_default().to_ascii_lowercase();
    match ext.as_str() {
        "txt" | "log" | "cfg" | "ini" | "xml" | "json" | "yaml" | "yml" | "conf" => "text/plain; charset=utf-8",
        "png" => "image/png",
        "jpg" | "jpeg" => "image/jpeg",
        "zip" => "application/zip",
        "gz" | "tgz" => "application/gzip",
        "tar" => "application/x-tar",
        _ => "application/octet-stream",
    }
}

// ------------------------- 错误 -------------------------

#[derive(Debug)]
enum FileError {
    Unauthorized,
    Forbidden(String),
    NotFound(String),
    BadRequest(String),
    TooLarge,
    Internal(String),
}

impl FileError {
    fn io(e: std::io::Error) -> Self {
        if e.kind() == std::io::ErrorKind::NotFound {
            FileError::NotFound("path not found".to_string())
        } else {
            FileError::Internal(format!("io error: {e}"))
        }
    }
    fn not_found(_e: std::io::Error) -> Self {
        FileError::NotFound("path not found".to_string())
    }
    fn internal(e: impl std::fmt::Display) -> Self {
        FileError::Internal(format!("internal error: {e}"))
    }
}

impl From<String> for FileError {
    fn from(msg: String) -> Self {
        FileError::BadRequest(msg)
    }
}

impl IntoResponse for FileError {
    fn into_response(self) -> Response {
        let (code, msg) = match self {
            FileError::Unauthorized => (StatusCode::UNAUTHORIZED, "unauthorized".to_string()),
            FileError::Forbidden(m) => (StatusCode::FORBIDDEN, m),
            FileError::NotFound(m) => (StatusCode::NOT_FOUND, m),
            FileError::BadRequest(m) => (StatusCode::BAD_REQUEST, m),
            FileError::TooLarge => (StatusCode::PAYLOAD_TOO_LARGE, "file too large".to_string()),
            FileError::Internal(m) => (StatusCode::INTERNAL_SERVER_ERROR, m),
        };
        (code, Json(serde_json::json!({ "error": msg }))).into_response()
    }
}
