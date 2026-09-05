//! node_agent 一键更新（P2，见 docs/node-agent-upgrade-design.md §3.2.4）。
//!
//! 流程：controller 经 gRPC 下发 `UpdateNodeAgentRequest{version, sha256, size_bytes, download_url}` →
//! 本模块 HTTP 拉取 → sha256/大小复核 → 写 staging → 备份当前二进制（node-agent.prev）→
//! 原子 rename 替换 → `exit(42)`（systemd Restart=always 自动重跑 start.sh 拉起新版本）。
//!
//! 运行中的二进制在 Linux 上可 rename（inode 句柄继续存活），Windows 不支持覆盖运行中 exe
//! （已知限制，见设计稿 §8）。

use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;

use log::{error, info, warn};

use sha2::{Digest, Sha256};

/// 更新完成后的请求重启退出码（systemd Restart=always 拉起重跑 ExecStart=start.sh）
pub const EXIT_CODE_REQUEST_RESTART: i32 = 42;

/// 当前 agent 版本（v 前缀 + Cargo 版本；与 release 清单 version 格式一致，如 v0.1.0）
pub fn current_version() -> String {
    format!("v{}", env!("CARGO_PKG_VERSION"))
}

/// 是否存在可用下载依赖（hyper 客户端）。恒 true，编译期保证。
pub fn download_supported() -> bool {
    true
}

/// HTTP GET 拉取二进制（明文 http，controller 内网；agent 与 controller 信任面同 gRPC）
async fn http_get(url: &str) -> Result<Vec<u8>, String> {
    use http_body_util::BodyExt;
    use hyper_util::client::legacy::Client;
    use hyper_util::rt::TokioExecutor;

    let uri: hyper::Uri = url.parse().map_err(|e| format!("非法下载地址: {e}"))?;
    // B = 请求体类型（Empty）；响应体固定 Incoming
    let client: Client<hyper_util::client::legacy::connect::HttpConnector, http_body_util::Empty<bytes::Bytes>> =
        Client::builder(TokioExecutor::new()).build_http();

    let req = hyper::Request::builder()
        .method(hyper::Method::GET)
        .uri(uri)
        .body(http_body_util::Empty::<bytes::Bytes>::new())
        .map_err(|e| format!("构造下载请求失败: {e}"))?;
    let resp = client
        .request(req)
        .await
        .map_err(|e| format!("下载失败: {e}"))?;
    let status = resp.status();
    let body = resp.into_body();
    let bytes = BodyExt::collect(body)
        .await
        .map_err(|e| format!("读取下载内容失败: {e}"))?
        .to_bytes()
        .to_vec();
    if !status.is_success() {
        return Err(format!("下载返回 HTTP {status}"));
    }
    Ok(bytes)
}

/// 拉取目标二进制：优先对象存储（object_key/bucket，P3 起），
/// download_url 为 P2 过渡遗留（controller 下载端点已退役）——object_key 为空时回退。
async fn fetch_binary(
    store: &Arc<dyn crate::ports::ObjectStore>,
    bucket: &str,
    object_key: &str,
    download_url: &str,
) -> Result<Vec<u8>, String> {
    if !object_key.is_empty() {
        if bucket.is_empty() {
            return Err("object_key 已提供但 bucket 为空".to_string());
        }
        store
            .get_object(bucket, object_key)
            .await
            .map_err(|e| format!("对象存储拉取失败 {bucket}/{object_key}: {e}"))
    } else if !download_url.is_empty() {
        http_get(download_url).await
    } else {
        Err("缺少下载源（object_key/bucket 或 download_url）".to_string())
    }
}

/// 执行一次更新：下载 → 校验 → staging → 备份 → 原子替换 → 返回（调用方负责 exit(42)）。
/// 任一环节失败返回 Err，不触碰当前二进制（staging/备份文件残留由日志提示人工清理）。
pub async fn apply_update(
    version: &str,
    sha256_hex: &str,
    size_bytes: i64,
    bucket: &str,
    object_key: &str,
    download_url: &str,
    store: &Arc<dyn crate::ports::ObjectStore>,
) -> Result<PathBuf, String> {
    if version.is_empty() || sha256_hex.is_empty() {
        return Err("update 参数不完整（version/sha256 必填）".to_string());
    }
    if object_key.is_empty() && download_url.is_empty() {
        return Err("update 参数不完整（object_key 或 download_url 至少其一）".to_string());
    }
    if !object_key.is_empty() {
        info!("开始更新 node_agent: version={version} sha256={sha256_hex} object={bucket}/{object_key}");
    } else {
        info!("开始更新 node_agent: version={version} sha256={sha256_hex} url={download_url}");
    }

    // 1) 下载（对象存储优先，download_url 回退）
    let bytes = fetch_binary(store, bucket, object_key, download_url).await?;

    // 2) 大小校验（期望非 0 时）
    if size_bytes > 0 && bytes.len() as i64 != size_bytes {
        return Err(format!(
            "下载大小不符: got {} want {size_bytes}",
            bytes.len()
        ));
    }
    // 3) sha256 校验
    let digest = Sha256::digest(&bytes);
    let digest_hex = hex::encode(digest);
    if !digest_hex.eq_ignore_ascii_case(sha256_hex) {
        return Err(format!(
            "sha256 校验失败: got {digest_hex} want {sha256_hex}"
        ));
    }
    info!("二进制校验通过 size={} sha256={digest_hex}", bytes.len());

    // 4) 定位自身路径（start.sh 的 BINARY 默认同目录 node_agent）
    let exe = std::env::current_exe().map_err(|e| format!("定位自身路径失败: {e}"))?;
    let dir = exe
        .parent()
        .map(PathBuf::from)
        .ok_or_else(|| "无法定位二进制目录".to_string())?;
    if !dir.is_dir() {
        return Err(format!("二进制目录不存在: {}", dir.display()));
    }
    let staging = dir.join(".node-agent.new");
    let backup = dir.join("node-agent.prev");

    // 5) staging 落盘 + 设可执行权限（0o755）：rename 保留权限，
    //    否则替换后的新二进制无 x 权限，systemd 拉起 start.sh 报 Permission denied（126）失联。
    tokio::fs::write(&staging, &bytes)
        .await
        .map_err(|e| format!("写 staging 失败 {}: {e}", staging.display()))?;
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        tokio::fs::set_permissions(&staging, std::fs::Permissions::from_mode(0o755))
            .await
            .map_err(|e| format!("设置 staging 可执行权限失败 {}: {e}", staging.display()))?;
    }
    info!("staging 已写入（可执行）: {}", staging.display());

    // 6) 备份当前 → rename staging → 当前路径（Linux 下运行中 exe 可 rename）
    if let Err(e) = std::fs::rename(&exe, &backup) {
        let _ = tokio::fs::remove_file(&staging).await;
        // Windows 下替换运行中文件会拒绝：给出可读错误
        return Err(format!(
            "备份当前二进制失败 {} → {}: {e}（Windows 无法覆盖运行中进程，请改用部署通道）",
            exe.display(),
            backup.display()
        ));
    }
    if let Err(e) = std::fs::rename(&staging, &exe) {
        // 回滚：尝试恢复备份
        let _ = std::fs::rename(&backup, &exe);
        warn!("替换失败已回滚: {e}");
        return Err(format!("替换二进制失败: {e}"));
    }
    info!(
        "替换完成: {}（旧版备份 {}），即将退出并请求重启",
        exe.display(),
        backup.display()
    );
    Ok(exe)
}

/// 在后台执行更新并请求重启：sleep 短暂时间保证 gRPC 响应已发出，然后 exit(42)。
/// 由 gRPC handler 在返回 accepted 后调用（spawn）。
pub async fn run_update_and_restart(
    version: &str,
    sha256_hex: &str,
    size_bytes: i64,
    bucket: &str,
    object_key: &str,
    download_url: &str,
    store: Arc<dyn crate::ports::ObjectStore>,
) {
    match apply_update(version, sha256_hex, size_bytes, bucket, object_key, download_url, &store).await {
        Ok(_) => {
            // 等响应 flush（tonic 响应通常在 handler 返回后写出）
            tokio::time::sleep(Duration::from_millis(500)).await;
            info!("退出码 {EXIT_CODE_REQUEST_RESTART}：请求 systemd 重启");
            std::process::exit(EXIT_CODE_REQUEST_RESTART);
        }
        Err(e) => {
            error!("node_agent 更新失败（保持当前版本运行）: {e}");
        }
    }
}
