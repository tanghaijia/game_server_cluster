# 内容寻址增量快照 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 node_agent 的目录上传/下载从 tar.zst 整包归档重写为内容寻址 + 每文件增量 + manifest 提交点，并接入 create_snapshot / restore_snapshot / clean_instance。

**Architecture:** 对象存储布局改为 `objects/{sha256}`（内容寻址，跨快照去重）+ `snapshots/{snapshot_id}/manifest.json`（提交点）。`DirectoryUploadDownloadService` 提供 create/restore/gc 三个方法，运行中快照先做本地冻结拷贝（reflink/cp），停机直接用原目录。`ObjectStore` trait 扩展 exists/list/delete。

**Tech Stack:** Rust, tokio, sha2, serde_json, aws_sdk_s3（S3ObjectStore）, walkdir。规格文档：`docs/superpowers/specs/2026-07-31-snapshot-incremental-sync-design.md`。

## Global Constraints

- 对象 key 布局固定：`objects/{sha256}`、`snapshots/{snapshot_id}/manifest.json`。
- 增量判定：每次备份全量 content-hash，hash 相同跳过上传；快路径查 prev manifest，权威判定用 `object_exists`。
- manifest 即提交点：所有文件上传完才写 manifest；恢复只认 manifest。
- 单文件保持 `Vec<u8>`（不引入流式/分块，YAGNI）。
- 保留现有 `BackgroundWorker` trait 签名（`clean_instance(instance_id, bucket, key)` 不变）。
- 与 asset_service 的集成点按规格文档约定实现（manifest_uri 指向 manifest、统一 bucket、key 作 snapshot_id 语义），代码内注释说明。

---
## 文件结构

| 文件 | 责任 |
|---|---|
| `node_agent/src/ports/object_store.rs` | 扩展 trait：+`object_exists` `list_objects` `delete_object` |
| `node_agent/src/clients/s3_object_store.rs` | 实现三个新方法（aws_sdk_s3） |
| `node_agent/src/providers/in_memory_object_store.rs` | 实现三个新方法（内存 HashMap） |
| `node_agent/src/domain/snapshot.rs` | `Entry` +`object_key` `sha256`；`Manifest` 移除 `checksum` |
| `node_agent/src/service/directory_upload_download_service.rs` | 重写：`create_snapshot` `restore_snapshot` `garbage_collect` `download_manifest` + `freeze_copy` + 单测；删除 tar.zst 三个方法 |
| `node_agent/src/service/node_agent_service.rs` | `create_snapshot`（wire）、`restore_snapshot`（改用 manifest）、`clean_instance`（改用增量上传） |

---

### Task 1: ObjectStore trait 扩展 + 两个实现

**Files:**
- Modify: `node_agent/src/ports/object_store.rs`
- Modify: `node_agent/src/clients/s3_object_store.rs`
- Modify: `node_agent/src/providers/in_memory_object_store.rs`

**Interfaces:**
- Consumes: 现有 `ObjectStore` trait（put_object / get_object）
- Produces:
  ```rust
  async fn object_exists(&self, bucket: &str, key: &str) -> Result<bool, Box<dyn Error>>;
  async fn list_objects(&self, bucket: &str, prefix: &str) -> Result<Vec<String>, Box<dyn Error>>; // 返回 key（不含 bucket 前缀）
  async fn delete_object(&self, bucket: &str, key: &str) -> Result<(), Box<dyn Error>>;
  ```

**步骤：**
- [ ] trait 加 3 个方法签名
- [ ] S3：`object_exists` 用 head_object（NotFound → false）；`list_objects` 用 list_objects_v2 拼前缀；`delete_object` 用 delete_object
- [ ] InMemory：`object_exists` 用 `{bucket}/{key}` 查 HashMap；`list_objects` 过滤 `{bucket}/{prefix}` 前缀并去掉 bucket；`delete_object` remove
- [ ] `cargo check` 通过

### Task 2: Manifest / Entry domain 扩展

**Files:**
- Modify: `node_agent/src/domain/snapshot.rs`

**Interfaces:**
- Produces:
  ```rust
  pub struct Entry {
      pub path: String, pub mode: String, pub size: u64,
      pub object_key: String, // objects/{sha256}
      pub sha256: String,
  }
  pub struct Manifest { // 移除 checksum
      pub snapshot_id, instance_id, captured_at, file_count, total_size_bytes, entries
  }
  ```

**步骤：**
- [ ] 给 `Entry` 加 `object_key`、`sha256`；`Manifest` 删 `checksum`
- [ ] grep 确认无其它代码读 `Manifest.checksum` / 构造 `Entry`（除本计划覆盖处）
- [ ] `cargo check` 通过

### Task 3: DirectoryUploadDownloadService 重写

**Files:**
- Modify: `node_agent/src/service/directory_upload_download_service.rs`（删除 tar.zst 相关，重写）
- Test: 同文件 `mod tests`

**Interfaces:**
- Consumes: `ObjectStore`（含新方法）、`Manifest`/`Entry`（新字段）
- Produces:
  ```rust
  pub struct DirectoryUploadDownloadService { object_store: Arc<dyn ObjectStore> }
  impl DirectoryUploadDownloadService {
      pub fn new(object_store: Arc<dyn ObjectStore>) -> Self;
      pub async fn create_snapshot(&self, bucket: &str, src_dir: impl AsRef<Path>,
          instance_id: &str, snapshot_id: &str, previous_manifest: Option<&Manifest>)
          -> Result<Manifest, Box<dyn std::error::Error>>;
      pub async fn restore_snapshot(&self, bucket: &str, manifest: &Manifest,
          dest_dir: impl AsRef<Path>, subset: Option<&[String]>)
          -> Result<(), Box<dyn std::error::Error>>;
      pub async fn garbage_collect(&self, bucket: &str, retained_manifests: &[Manifest])
          -> Result<usize, Box<dyn std::error::Error>>;
      pub async fn download_manifest(&self, bucket: &str, snapshot_id: &str)
          -> Result<Manifest, Box<dyn std::error::Error>>;
  }
  // 模块级 helper
  pub fn manifest_key(snapshot_id: &str) -> String;  // snapshots/{sid}/manifest.json
  pub(crate) async fn freeze_copy(src: impl AsRef<Path>) -> Result<PathBuf, Box<dyn Error>>;
      // 纯 Rust 递归拷贝到 temp_dir；Unix 上先试 cp --reflink=auto，失败回退
  ```

**核心逻辑：**
- `create_snapshot`：walk 目录收集 (relpath, mode, size) → 逐文件 sha256 → `objects/{sha}` 不存在才 put（快路径 prev.entries → object_exists）→ 组 manifest → put manifest（提交点）。返回 Manifest。
- `restore_snapshot`：按 entries（subset 过滤，subset = 路径前缀）get_object → 校验 sha256 → 写 dest_dir/{path}（建目录、设 mode）。顺序执行。
- `garbage_collect`：收集 retained 引用的 object_key 集合 → list_objects("objects/") → 删未引用者 → 返回删除数。
- `download_manifest`：`get_object(bucket, manifest_key(snapshot_id))` → serde_json。

**测试（InMemoryObjectStore + tempfile）：**
- [ ] create 后 manifest 存在；相同内容二次 create 不新增 object（比较 list_objects 数量）
- [ ] 修改一个文件后 create，只有新 hash object 被上传
- [ ] restore 全量恢复，文件内容/相对路径一致
- [ ] restore subset 只恢复匹配前缀的文件
- [ ] garbage_collect 删孤儿、保留被引用
- [ ] freeze_copy 拷贝内容与源一致
- [ ] `cargo test test_ 通过`（目录上传下载相关）

### Task 4: NodeAgentService 接线

**Files:**
- Modify: `node_agent/src/service/node_agent_service.rs`

**Interfaces:**
- Consumes: `directory_service`（Task 3）、`asset_service`（create_snapshot_record / complete_snapshot_record / set_latest_snapshot / get_latest_snapshot / fail_snapshot_record / get_snapshot）、`freeze_copy`
- Produces: 保持 `BackgroundWorker` trait 签名不变；`create_snapshot(request)` 从 `todo!()` 变为实现

**步骤：**
- [ ] `create_snapshot(request)`：
  1. `game_instance_repos.get` → 状态；Running → `freeze_copy(host_data_path)`，否则直接用 host_data_path
  2. `asset_service.create_snapshot_record(instance_id, build_id, 0, node_id)` → record（拿 bucket + snapshot_id）
  3. `get_latest_snapshot(instance_id)` → 有则 `directory_service.download_manifest(record.bucket, prev.snapshot_id)` 作 previous_manifest（失败则 None）
  4. `directory_service.create_snapshot(bucket, src, instance_id, record.snapshot_id, prev)`
  5. `complete_snapshot_record(record.snapshot_id, manifest_key, Some(manifest_key), None)` + `set_latest_snapshot`
  6. 无论成败删除冻结拷贝临时目录
  7. 失败：`fail_snapshot_record(record.snapshot_id, err)`
- [ ] `restore_snapshot(request)`：`get_snapshot(request.snapshot_id)` 拿 bucket → `download_manifest(bucket, request.snapshot_id)` → `directory_service.restore_snapshot(bucket, &manifest, HostSnapShotDataPath::new(instance_id), None)` → 返回 SnapshotRestoreResult
- [ ] `clean_instance(instance_id)`：委托 `create_snapshot`（完整生命周期：建记录→增量上传→complete/set_latest→失败标记），保留实例状态置 `Stopped`；`BackgroundWorker::clean_instance` 签名去掉 `bucket`/`key`，同步改 `CleanInstanceJob`/`enqueue_clean_instance`/gRPC handler
- [ ] `cargo check` 通过

### Task 5: 全量验证

- [ ] `cargo test`（node_agent 全量）通过
- [ ] `cargo check` 无新增 warning（除既有的 common/mod.rs 等）
- [ ] `gitnexus_detect_changes` 确认受影响范围符合预期

---
## 集成假设（代码注释中注明）

1. `SnapshotRecord.snapshot_id` 是快照标识，manifest 位置由 `snapshots/{snapshot_id}/manifest.json` 推导；`SnapshotRecord.key`（旧 tar.zst key）不再参与新流程。
2. `storage_uri`/`manifest_uri` 指向 `snapshots/{snapshot_id}/manifest.json`（无单归档）。
3. 统一 bucket：全局 `objects/` 去重要求同一 bucket。
4. `clean_instance` 走完整快照生命周期（`create_snapshot_record` 提供 bucket + 权威 snapshot_id），`BackgroundWorker::clean_instance` 不再接收 bucket/key；`SnapshotRecord.key` 语义随之废弃。
