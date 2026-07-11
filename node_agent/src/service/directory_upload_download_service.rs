use async_compression::tokio::{bufread::ZstdDecoder, write::ZstdEncoder};
use sha2::{Digest, Sha256};
use std::path::Path;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio_util::io::SyncIoBridge;
use walkdir::WalkDir;

use chrono::Utc;

use crate::domain::{Entry, Manifest};
use crate::ports::ObjectStore;

// ============================================================
// 平台适配：Unix 获取文件权限模式，Windows 回退默认值
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

// ============================================================
// 目录遍历 —— 收集文件元数据，用于生成 manifest
// ============================================================

fn collect_file_entries(src_dir: &Path) -> Result<Vec<Entry>, std::io::Error> {
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
        entries.push(Entry {
            path: relative.to_string_lossy().replace('\\', "/"),
            size: metadata.len(),
            mode: get_file_mode(&metadata),
        });
    }
    Ok(entries)
}

// ============================================================
// 上传：tar + zstd 流式压缩上传，捎带计算 SHA256
// ============================================================

/// 将目录以 tar.zst 格式流式上传到对象存储，同时计算压缩数据的 SHA256。
/// 文件路径是去掉 src_dir 前缀后的相对路径存入 manifest。
/// 举例
///
/// 磁盘上的 src_dir = /data/instances/abc/
/// ├── level.dat
/// ├── region/
/// │   └── r.0.0.mca
/// └── playerdata/
///     └── uuid.dat
///
/// ↓ 上传后 tar包内的路径结构
///
/// ./
/// ├── level.dat
/// ├── region/r.0.0.mca
/// └── playerdata/uuid.dat
///
/// 不包含 abc/ 这一层。
/// 返回值为 hex 编码的 SHA256 摘要，格式为 `"sha256:abcdef..."`。
pub async fn upload_dir_as_tar_zst(
    object_store: &dyn ObjectStore,
    bucket: &str,
    key: &str,
    src_dir: impl AsRef<Path>,
) -> Result<String, Box<dyn std::error::Error>> {
    let src_dir = src_dir.as_ref().to_path_buf();

    // 1. 创建内存管道，20MB 缓冲区
    let (mut pipe_reader, pipe_writer) = tokio::io::duplex(20 * 1024 * 1024);

    // 2. 将管道的写端包装进异步 Zstd 编码器
    let zstd_encoder = ZstdEncoder::new(pipe_writer);

    // 3. 派生阻塞线程处理 tar 打包
    tokio::task::spawn_blocking(move || {
        let rt = tokio::runtime::Handle::current();

        struct SyncWriter {
            encoder: ZstdEncoder<tokio::io::DuplexStream>,
            handle: tokio::runtime::Handle,
        }

        impl std::io::Write for SyncWriter {
            fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
                self.handle.block_on(async {
                    self.encoder
                        .write(buf)
                        .await
                        .map_err(|e| std::io::Error::new(std::io::ErrorKind::Other, e))
                })
            }
            fn flush(&mut self) -> std::io::Result<()> {
                self.handle.block_on(async {
                    self.encoder
                        .flush()
                        .await
                        .map_err(|e| std::io::Error::new(std::io::ErrorKind::Other, e))
                })
            }
        }

        let mut sync_writer = SyncWriter {
            encoder: zstd_encoder,
            handle: rt.clone(),
        };

        let mut archive = tar::Builder::new(&mut sync_writer);

        if let Err(e) = archive.append_dir_all(".", &src_dir) {
            eprintln!("Tar appending failed: {}", e);
            return;
        }

        if let Err(e) = archive.into_inner() {
            eprintln!("Tar finish failed: {}", e);
            return;
        }

        rt.block_on(async {
            let mut encoder = sync_writer.encoder;
            let _ = encoder.shutdown().await;
        });
    });

    // 4. 读取管道的全部输出到 buffer，同时计算 SHA256
    let mut hasher = Sha256::new();
    let mut compressed_data = Vec::new();
    let mut chunk = vec![0u8; 32 * 1024];
    loop {
        match pipe_reader.read(&mut chunk).await {
            Ok(0) => break,
            Ok(n) => {
                hasher.update(&chunk[..n]);
                compressed_data.extend_from_slice(&chunk[..n]);
            }
            Err(e) => return Err(e.into()),
        }
    }

    // 5. 上传到对象存储
    object_store
        .put_object(bucket, key, compressed_data)
        .await?;

    // 6. 取出最终哈希值
    let checksum = format!("sha256:{}", hex::encode(hasher.finalize()));
    Ok(checksum)
}

// ============================================================
// 生成并上传 manifest.json
// ============================================================

async fn generate_and_upload_manifest(
    object_store: &dyn ObjectStore,
    bucket: &str,
    manifest_key: &str,
    snapshot_id: &str,
    instance_id: &str,
    checksum: &str,
    entries: &[Entry],
) -> Result<(), Box<dyn std::error::Error>> {
    let total_size: u64 = entries.iter().map(|e| e.size).sum();
    let manifest = Manifest {
        snapshot_id: snapshot_id.to_string(),
        instance_id: instance_id.to_string(),
        captured_at: Utc::now().to_rfc3339(),
        checksum: checksum.to_string(),
        file_count: entries.len(),
        total_size_bytes: total_size,
        entries: entries.to_vec(),
    };

    let json = serde_json::to_vec_pretty(&manifest)?;
    object_store.put_object(bucket, manifest_key, json).await?;

    Ok(())
}

// ============================================================
// 编排函数：完整快照创建流程
// ============================================================

/// 创建快照归档：目录遍历 → 压缩上传 + 捎带 SHA256 → 生成并上传 manifest.json
///
/// `key_prefix` 例如 `"snapshots/{snapshot_id}"`，
/// 会生成 `{key_prefix}.tar.zst` 和 `{key_prefix}.manifest.json` 两个对象。
///
/// 返回 `(checksum, manifest_key)`。
pub async fn create_snapshot_archive(
    object_store: &dyn ObjectStore,
    bucket: &str,
    key_prefix: &str,
    snapshot_id: &str,
    instance_id: &str,
    src_dir: impl AsRef<Path>,
) -> Result<(String, String), Box<dyn std::error::Error>> {
    let src_dir = src_dir.as_ref();

    // 1. 收集文件元数据
    let entries = collect_file_entries(src_dir)?;

    // 2. 压缩上传 + 计算 checksum
    let archive_key = format!("{}.tar.zst", key_prefix);
    let checksum = upload_dir_as_tar_zst(object_store, bucket, &archive_key, src_dir).await?;

    // 3. 生成并上传 manifest
    let manifest_key = format!("{}.manifest.json", key_prefix);
    generate_and_upload_manifest(
        object_store,
        bucket,
        &manifest_key,
        snapshot_id,
        instance_id,
        &checksum,
        &entries,
    )
    .await?;

    Ok((checksum, manifest_key))
}

// ============================================================
// 下载：先拉 manifest.json 拿到 checksum，再下载解压 tar.zst 并自动校验
// ============================================================

/// 从 archive key 推导 manifest key。
///
/// 例如 `snapshots/snap_001.tar.zst` → `snapshots/snap_001.manifest.json`
fn manifest_key_from_archive_key(archive_key: &str) -> Result<String, Box<dyn std::error::Error>> {
    archive_key
        .strip_suffix(".tar.zst")
        .map(|prefix| format!("{}.manifest.json", prefix))
        .ok_or_else(|| format!("archive key '{}' does not end with '.tar.zst'", archive_key).into())
}

/// 从对象存储下载 manifest.json 到内存，反序列化为 `Manifest`。
async fn download_manifest(
    object_store: &dyn ObjectStore,
    bucket: &str,
    manifest_key: &str,
) -> Result<Manifest, Box<dyn std::error::Error>> {
    let data = object_store.get_object(bucket, manifest_key).await?;
    let manifest: Manifest = serde_json::from_slice(&data)?;
    Ok(manifest)
}

/// 从对象存储下载 tar.zst 归档并解压到目标目录。
///
/// 同时自动从对应的 `.manifest.json` 中获取 checksum 并在解压完成后校验。
/// 如果 checksum 不匹配，返回错误。
///
/// `key` 需要以 `.tar.zst` 结尾，manifest 键名由此自动推导。
/// 返回解析好的 `Manifest` 结构体。
pub async fn download_and_extract_tar_zst(
    object_store: &dyn ObjectStore,
    bucket: &str,
    key: &str,
    dest_dir: impl AsRef<Path>,
) -> Result<Manifest, Box<dyn std::error::Error>> {
    // 0. 先下载 manifest.json，拿到期望的 checksum
    let manifest_key = manifest_key_from_archive_key(key)?;
    let manifest = download_manifest(object_store, bucket, &manifest_key).await?;

    let dest_dir = dest_dir.as_ref().to_path_buf();

    // 1. 从对象存储获取 tar.zst 数据
    let data = object_store.get_object(bucket, key).await?;

    // 2. 创建内存管道
    let (pipe_reader, mut pipe_writer) = tokio::io::duplex(20 * 1024 * 1024);

    // 3. 管道的读端包装进 Zstd 解压器
    let buffered_reader = tokio::io::BufReader::new(pipe_reader);
    let zstd_decoder = ZstdDecoder::new(buffered_reader);

    // 4. 派生阻塞线程处理解压
    let extract_handle = tokio::task::spawn_blocking(move || -> Result<(), std::io::Error> {
        let sync_reader = SyncIoBridge::new(zstd_decoder);
        let mut archive = tar::Archive::new(sync_reader);
        archive.unpack(&dest_dir)
    });

    // 5. 创建 SHA256 哈希器
    let mut hasher = Sha256::new();

    // 6. 将数据喂给管道，同时计算哈希
    hasher.update(&data);
    pipe_writer.write_all(&data).await?;

    // 7. 关闭写端，避免死锁
    pipe_writer.shutdown().await?;

    // 8. 等待解压线程完成
    extract_handle.await??;

    // 9. 校验 checksum
    let actual = format!("sha256:{}", hex::encode(hasher.finalize()));
    if actual != manifest.checksum {
        return Err(format!(
            "checksum mismatch for {}: expected {}, got {}",
            key, manifest.checksum, actual
        )
        .into());
    }

    Ok(manifest)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::providers::InMemoryObjectStore;
    use std::fs::File;
    use std::io::Write;
    use tempfile::tempdir;

    #[tokio::test]
    async fn test_upload_dir_as_tar_zst_success() {
        // 1. 创建临时目录和文件
        let dir = tempdir().unwrap();
        let file_path = dir.path().join("test_file.txt");
        let mut file = File::create(file_path).unwrap();
        writeln!(file, "Hello, Rust S3 test!").unwrap();

        // 2. 使用 InMemoryObjectStore（零网络、零外部依赖）
        let object_store = InMemoryObjectStore::new();
        let bucket = "my-test-bucket";
        let key = "backups/test.tar.zst";

        let result = upload_dir_as_tar_zst(&object_store, bucket, key, dir.path()).await;

        // 3. 断言上传成功
        assert!(
            result.is_ok(),
            "upload_dir_as_tar_zst failed: {:?}",
            result.err()
        );

        // 4. 数据确实写入了内存存储
        let stored = object_store.get_object(bucket, key).await.unwrap();
        assert!(!stored.is_empty(), "stored data should not be empty");
    }
}
