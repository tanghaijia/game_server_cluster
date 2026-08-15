use std::ffi::OsString;
use std::path::{Component, Path, PathBuf};

/// 把 rel 解析到 data_root 之内：词法规范化（吞掉 ..），最终路径必须仍以 data_root 为前缀。
/// 对不存在的目标（上传/新建）同样安全；对已存在路径由调用方再做 canonicalize 防符号链接逃逸。
pub fn resolve_within(root: &Path, rel: &str) -> Result<PathBuf, String> {
    let rel = rel.trim_start_matches('/').trim_start_matches('\\');

    let mut parts: Vec<OsString> = Vec::new();
    for comp in Path::new(rel).components() {
        match comp {
            Component::Normal(c) => parts.push(c.to_os_string()),
            Component::ParentDir => {
                // 允许 .. 但吞掉上一段，保证不会越出 data_root
                parts.pop();
            }
            _ => {}
        }
    }

    let mut resolved = root.to_path_buf();
    for part in parts {
        resolved.push(part);
    }

    if !resolved.starts_with(root) {
        return Err("path escapes data root".to_string());
    }
    Ok(resolved)
}

/// 校验已存在的路径没有通过符号链接逃出 data_root
pub fn ensure_no_symlink_escape(root: &Path, target: &Path) -> Result<(), String> {
    let canon = target.canonicalize().map_err(|e| format!("canonicalize: {e}"))?;
    let root_canon = root.canonicalize().map_err(|e| format!("canonicalize root: {e}"))?;
    if !canon.starts_with(&root_canon) {
        return Err("path escapes data root via symlink".to_string());
    }
    Ok(())
}
