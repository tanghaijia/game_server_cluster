use std::collections::HashSet;
use std::path::{Path, PathBuf};
use std::sync::Arc;

use chrono::Utc;
use sha2::{Digest, Sha256};
use tokio::fs;
use walkdir::WalkDir;

use crate::domain::{Entry, Manifest};
use crate::ports::ObjectStore;

// ============================================================
// 平台适配：Unix 获取/设置文件权限模式，Windows 回退默认值
// ============================================================

#[cfg(unix)]
fn get_file_mode(metadata: &std::fs::Metadata) -> String {
    use std::os::unix::fs::PermissionsExt;
    format!("{:o}", metadata.permissions().mode() & 0o777)
}

#[cfg(not(unix))]
fn get_file_mode(_metadata: &std::fs::Metadata) -> String {
    "644".to_string()
}

#[cfg(unix)]
fn set_file_mode(path: &Path, mode: &str) -> Result<(), Box<dyn std::error::Error>> {
    use std::os::unix::fs::PermissionsExt;
    if let Ok(mode_val) = u32::from_str_radix(mode, 8) {
        std::fs::set_permissions(path, std::fs::Permissions::from_mode(mode_val))?;
    }
    Ok(())
}

#[cfg(not(unix))]
fn set_file_mode(_path: &Path, _mode: &str) -> Result<(), Box<dyn std::error::Error>> {
    Ok(())
}

// ============================================================
// 目录遍历 —— 收集文件元数据（相对路径、大小、权限）
// ============================================================

/// 返回 `(相对路径, 大小, 权限模式)` 列表，路径统一使用 `/` 分隔。
fn collect_file_entries(src_dir: &Path) -> Result<Vec<(String, u64, String)>, std::io::Error> {
    let mut entries = Vec::new();
    for entry in WalkDir::new(src_dir).sort_by_file_name() {
        let entry = entry?;
        if !entry.file_type().is_file() {
            continue;
        }
        let relative = entry
            .path()
            .strip_prefix(src_dir)
            .map_err(|e| std::io::Error::new(std::io::ErrorKind::Other, e))?;
        let metadata = entry.metadata()?;
        entries.push((
            relative.to_string_lossy().replace('\\', "/"),
            metadata.len(),
            get_file_mode(&metadata),
        ));
    }
    Ok(entries)
}

// ============================================================
// Key 布局约定
// ============================================================

/// 快照提交点 key：`snapshots/{snapshot_id}/manifest.json`
pub(crate) fn manifest_key(snapshot_id: &str) -> String {
    format!("snapshots/{}/manifest.json", snapshot_id)
}

/// 内容寻址对象 key：`objects/{sha256}`
fn object_key(sha256: &str) -> String {
    format!("objects/{}", sha256)
}

// ============================================================
// DirectoryUploadDownloadService — 内容寻址增量快照服务
// ============================================================

/// 封装目录与对象存储之间的上传/下载操作。
///
/// 生产环境：注入 S3ObjectStore
/// 开发环境：注入 InMemoryObjectStore
///
/// 布局：
/// - `objects/{sha256}` —— 内容寻址对象，跨快照/跨实例去重
/// - `snapshots/{snapshot_id}/manifest.json` —— 快照提交点（写完它才算快照成功）
pub struct DirectoryUploadDownloadService {
    object_store: Arc<dyn ObjectStore>,
}

impl DirectoryUploadDownloadService {
    pub fn new(object_store: Arc<dyn ObjectStore>) -> Self {
        Self { object_store }
    }

    /// 增量上传目录并写入 manifest 提交点。
    ///
    /// 对每个文件计算 sha256，内容已在对象存储中（快路径查 `previous_manifest`，
    /// 权威判定 `object_exists`）则跳过上传；全部上传完才写 manifest。
    /// 返回生成的 `Manifest`。
    pub async fn create_snapshot(
        &self,
        bucket: &str,
        src_dir: impl AsRef<Path>,
        instance_id: &str,
        snapshot_id: &str,
        previous_manifest: Option<&Manifest>,
    ) -> Result<Manifest, Box<dyn std::error::Error>> {
        let src_dir = src_dir.as_ref();

        let mut entries = Vec::new();
        let mut total_size: u64 = 0;

        for (rel_path, size, mode) in collect_file_entries(src_dir)? {
            let full_path = src_dir.join(&rel_path);
            let data = fs::read(&full_path).await?;
            let sha = hex::encode(Sha256::digest(&data));
            let object_key = object_key(&sha);

            // 增量判定：快路径 prev.entries 命中则跳过；否则 object_exists 权威判定
            let known = previous_manifest
                .map(|m| m.entries.iter().any(|e| e.sha256 == sha))
                .unwrap_or(false);
            if !known && !self.object_store.object_exists(bucket, &object_key).await? {
                self.object_store.put_object(bucket, &object_key, data).await?;
            }

            total_size += size;
            entries.push(Entry {
                path: rel_path,
                size,
                mode,
                object_key,
                sha256: sha,
            });
        }

        let manifest = Manifest {
            snapshot_id: snapshot_id.to_string(),
            instance_id: instance_id.to_string(),
            captured_at: Utc::now().to_rfc3339(),
            file_count: entries.len(),
            total_size_bytes: total_size,
            entries,
        };
        self.put_manifest(bucket, &manifest).await?;
        Ok(manifest)
    }

    /// 按 manifest 逐文件下载并恢复到目标目录。
    ///
    /// 每个文件校验 sha256；`subset` 为要恢复的路径前缀列表，`None` 表示全量恢复。
    /// 覆盖写幂等、可重入。
    pub async fn restore_snapshot(
        &self,
        bucket: &str,
        manifest: &Manifest,
        dest_dir: impl AsRef<Path>,
        subset: Option<&[String]>,
    ) -> Result<(), Box<dyn std::error::Error>> {
        let dest_dir = dest_dir.as_ref();

        for entry in &manifest.entries {
            if let Some(prefixes) = subset {
                if !prefixes.iter().any(|p| entry.path.starts_with(p.as_str())) {
                    continue;
                }
            }

            let data = self.object_store.get_object(bucket, &entry.object_key).await?;
            let actual = hex::encode(Sha256::digest(&data));
            if actual != entry.sha256 {
                return Err(format!(
                    "checksum mismatch for {}: expected {}, got {}",
                    entry.path, entry.sha256, actual
                )
                .into());
            }

            let target = dest_dir.join(&entry.path);
            if let Some(parent) = target.parent() {
                fs::create_dir_all(parent).await?;
            }
            fs::write(&target, &data).await?;
            set_file_mode(&target, &entry.mode)?;
        }

        Ok(())
    }

    /// GC：删除未被任何保留 manifest 引用的 `objects/` 对象。
    ///
    /// 返回删除的对象数量。
    pub async fn garbage_collect(
        &self,
        bucket: &str,
        retained_manifests: &[Manifest],
    ) -> Result<usize, Box<dyn std::error::Error>> {
        let mut referenced: HashSet<String> = HashSet::new();
        for manifest in retained_manifests {
            for entry in &manifest.entries {
                referenced.insert(entry.object_key.clone());
            }
        }

        let all = self.object_store.list_objects(bucket, "objects/").await?;
        let mut deleted = 0;
        for key in all {
            if !referenced.contains(&key) {
                self.object_store.delete_object(bucket, &key).await?;
                deleted += 1;
            }
        }
        Ok(deleted)
    }

    /// 下载指定快照的 manifest。
    pub async fn download_manifest(
        &self,
        bucket: &str,
        snapshot_id: &str,
    ) -> Result<Manifest, Box<dyn std::error::Error>> {
        let key = manifest_key(snapshot_id);
        let data = self.object_store.get_object(bucket, &key).await?;
        let manifest: Manifest = serde_json::from_slice(&data)?;
        Ok(manifest)
    }

    async fn put_manifest(
        &self,
        bucket: &str,
        manifest: &Manifest,
    ) -> Result<(), Box<dyn std::error::Error>> {
        let data = serde_json::to_vec(manifest)?;
        let key = manifest_key(&manifest.snapshot_id);
        self.object_store.put_object(bucket, &key, data).await?;
        Ok(())
    }
}

// ============================================================
// 本地冻结拷贝 —— 运行中快照的一致性取源
// ============================================================

/// 把 `src` 目录复制到临时目录作为"点时间一致的冻结源"，返回临时目录路径。
///
/// Unix 上优先尝试 `cp --reflink=auto -r`（COW 秒级），失败回退纯 Rust 递归拷贝。
/// 调用方负责在完成后删除返回的临时目录。
pub(crate) async fn freeze_copy(
    src: impl AsRef<Path>,
) -> Result<PathBuf, Box<dyn std::error::Error>> {
    let src = src.as_ref();
    let tmp = std::env::temp_dir().join(format!(
        "freeze_copy_{}_{}",
        std::process::id(),
        Utc::now().timestamp_nanos_opt().unwrap_or(0)
    ));

    #[cfg(unix)]
    {
        let status = tokio::process::Command::new("cp")
            .args(["--reflink=auto", "-r"])
            .arg(src)
            .arg(&tmp)
            .status()
            .await;
        if let Ok(status) = status {
            if status.success() {
                return Ok(tmp);
            }
        }
        // 回退：清理可能的半成品拷贝
        let _ = fs::remove_dir_all(&tmp).await;
    }

    let src = src.to_path_buf();
    let tmp_for_copy = tmp.clone();
    tokio::task::spawn_blocking(move || copy_dir_recursive(&src, &tmp_for_copy))
        .await
        .map_err(|e| format!("freeze copy task failed: {e}"))??;
    Ok(tmp)
}

fn copy_dir_recursive(src: &Path, dst: &Path) -> Result<(), std::io::Error> {
    std::fs::create_dir_all(dst)?;
    for entry in std::fs::read_dir(src)? {
        let entry = entry?;
        let from = entry.path();
        let to = dst.join(entry.file_name());
        if entry.file_type()?.is_dir() {
            copy_dir_recursive(&from, &to)?;
        } else if entry.file_type()?.is_file() {
            std::fs::copy(&from, &to)?;
        }
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::providers::InMemoryObjectStore;
    use std::fs::File;
    use std::io::Write;
    use tempfile::tempdir;

    fn write_file(dir: &Path, rel: &str, content: &str) {
        let path = dir.join(rel);
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent).unwrap();
        }
        let mut f = File::create(path).unwrap();
        writeln!(f, "{}", content).unwrap();
    }

    #[tokio::test]
    async fn test_create_snapshot_incremental() {
        let service = DirectoryUploadDownloadService::new(Arc::new(InMemoryObjectStore::new()));
        let bucket = "my-test-bucket";
        let dir = tempdir().unwrap();
        write_file(dir.path(), "a.txt", "hello");
        write_file(dir.path(), "b.txt", "world");

        // 第一次快照：全量上传
        let m1 = service
            .create_snapshot(bucket, dir.path(), "inst-1", "snap-1", None)
            .await
            .unwrap();
        let objects_after_first = service
            .object_store
            .list_objects(bucket, "objects/")
            .await
            .unwrap();
        assert_eq!(objects_after_first.len(), 2);
        assert!(service
            .object_store
            .object_exists(bucket, &manifest_key("snap-1"))
            .await
            .unwrap());

        // 修改 b.txt、新增 c.txt：a.txt 不变（增量跳过）
        write_file(dir.path(), "b.txt", "world v2");
        write_file(dir.path(), "c.txt", "new file");
        service
            .create_snapshot(bucket, dir.path(), "inst-1", "snap-2", Some(&m1))
            .await
            .unwrap();

        let objects_after_second = service
            .object_store
            .list_objects(bucket, "objects/")
            .await
            .unwrap();
        // 2 (首次) + b.txt 新内容 + c.txt = 4
        assert_eq!(objects_after_second.len(), 4);
    }

    #[tokio::test]
    async fn test_restore_snapshot_full_and_subset() {
        let service = DirectoryUploadDownloadService::new(Arc::new(InMemoryObjectStore::new()));
        let bucket = "my-test-bucket";
        let src = tempdir().unwrap();
        write_file(src.path(), "root.txt", "root");
        write_file(src.path(), "sub/x.txt", "x");

        let m = service
            .create_snapshot(bucket, src.path(), "inst-1", "snap-1", None)
            .await
            .unwrap();

        // 全量恢复
        let full = tempdir().unwrap();
        service
            .restore_snapshot(bucket, &m, full.path(), None)
            .await
            .unwrap();
        assert_eq!(
            std::fs::read_to_string(full.path().join("root.txt")).unwrap().trim(),
            "root"
        );
        assert_eq!(
            std::fs::read_to_string(full.path().join("sub/x.txt")).unwrap().trim(),
            "x"
        );

        // 子集恢复：只恢复 sub/ 前缀
        let sub = tempdir().unwrap();
        service
            .restore_snapshot(bucket, &m, sub.path(), Some(&["sub/".to_string()]))
            .await
            .unwrap();
        assert!(sub.path().join("sub/x.txt").exists());
        assert!(!sub.path().join("root.txt").exists());
    }

    #[tokio::test]
    async fn test_garbage_collect() {
        let service = DirectoryUploadDownloadService::new(Arc::new(InMemoryObjectStore::new()));
        let bucket = "my-test-bucket";
        let dir = tempdir().unwrap();
        write_file(dir.path(), "a.txt", "A");
        let m1 = service
            .create_snapshot(bucket, dir.path(), "inst-1", "snap-1", None)
            .await
            .unwrap();

        write_file(dir.path(), "a.txt", "B");
        write_file(dir.path(), "b.txt", "b");
        let m2 = service
            .create_snapshot(bucket, dir.path(), "inst-1", "snap-2", Some(&m1))
            .await
            .unwrap();

        // 只保留 snap-1，则 snap-2 独有的两个 object 应被删除
        let deleted = service
            .garbage_collect(bucket, &[m1.clone()])
            .await
            .unwrap();
        assert_eq!(deleted, 2);
        let remaining = service
            .object_store
            .list_objects(bucket, "objects/")
            .await
            .unwrap();
        assert_eq!(remaining.len(), 1);

        // 保留 snap-1 + snap-2，不应删除任何对象
        let deleted = service
            .garbage_collect(bucket, &[m1, m2])
            .await
            .unwrap();
        assert_eq!(deleted, 0);
    }

    #[tokio::test]
    async fn test_freeze_copy() {
        let src = tempdir().unwrap();
        write_file(src.path(), "level.dat", "world-data");
        write_file(src.path(), "region/r.0.0.mca", "region-data");

        let tmp = freeze_copy(src.path()).await.unwrap();
        assert_eq!(
            std::fs::read_to_string(tmp.join("level.dat")).unwrap().trim(),
            "world-data"
        );
        assert_eq!(
            std::fs::read_to_string(tmp.join("region/r.0.0.mca")).unwrap().trim(),
            "region-data"
        );
        let _ = std::fs::remove_dir_all(&tmp);
    }
}
