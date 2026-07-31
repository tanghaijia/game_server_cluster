# 目录上传/下载能力重写：内容寻址增量快照

日期：2026-07-31
状态：已评审（design review 通过）

## 背景与动机

`node_agent` 现有的目录上传/下载能力（`directory_upload_download_service.rs`）基于 **tar.zst 整包归档**：

- `upload_dir_as_tar_zst`：把目录打成 tar.zst 上传到对象存储（S3 / InMemory）
- `download_and_extract_tar_zst`：从对象存储下载整包解压
- `create_snapshot_archive`：整包归档 + `manifest.json`（文件清单 + 整包 checksum）

现状的四个问题：

1. **无增量**：每次快照全量重打重传，定期备份下带宽/存储浪费。
2. **上传全量内存缓冲**：`upload_dir_as_tar_zst` 把整个压缩包攒成 `Vec<u8>` 再 `put_object`，大目录一次快照内存飙升。
3. **下载全量读入内存**：`download_and_extract_tar_zst` 先 `get_object` 读整个归档再解压，同样问题。
4. **无法部分恢复**：恢复必须全量下载解压。

## 目标

1. 支持**定期备份 + 增量**：两次快照之间只上传变化的文件。
2. 同时支持**运行中**与**停机**两种快照状态，停机时目录静止，运行中保证点时间一致性。
3. 消除整包内存缓冲，改为每文件级上传/下载。
4. 保持/增强原子性与完整性校验。

## 已确认的需求与约束

| 维度 | 结论 |
|---|---|
| 快照模式 | 定期备份 + 增量 |
| 数据规模 | 几百 MB / 几千文件（单文件几十 KB ~ 几 MB） |
| 快照时机 | 运行中 + 停机都要 |
| 一致性 | 停机时目录静止；运行中需点时间冻结源 |

## 方案选型

- **选定 A：内容寻址 + 每文件增量 + manifest 提交点**
- 否决 B：tar.zst 全量归档（无增量，与需求冲突）
- 否决 C：增量 tar 差分（恢复慢、归档链复杂）
- 一致性取源选定 **B1：本地冻结拷贝**（运行中先 reflink/cp 到本地临时目录，再上传）

## 对象存储布局

```
objects/{sha256}                        ← 全局内容寻址，跨快照/跨实例去重
snapshots/{snapshot_id}/manifest.json   ← 快照提交点
```

- **manifest 即原子性**：所有文件上传完才写 manifest。恢复只认 manifest —— 不存在的 manifest = 不存在该快照。
- 全局 `objects/{sha256}`：同一实例定期备份只有变化文件产生新 object；相同内容天然只存一份。
- 统一使用一个 bucket（全局去重的前提）；`SnapshotRecord.bucket` 保持该值。

## Manifest 结构

基于现有 `Manifest`/`Entry`（`domain/snapshot.rs`）扩展：

```rust
pub struct Manifest {
    pub snapshot_id: String,
    pub instance_id: String,
    pub captured_at: String,
    pub file_count: usize,
    pub total_size_bytes: u64,
    pub entries: Vec<Entry>,
}

pub struct Entry {
    pub path: String,        // 相对路径（已有）
    pub mode: String,        // 权限（已有）
    pub size: u64,           // 已有
    pub object_key: String,  // 新增：objects/{sha256}
    pub sha256: String,      // 新增：内容哈希
}
```

- 去掉旧整包 `checksum`：每个文件自带 sha256，恢复时逐文件校验，比整包校验更精确。
- schema 变化需要与 `asset_service`（`SnapshotRecord.storage_uri / manifest_uri`）对齐，见"集成点"。

## 核心流程

### 快照创建（增量上传）

```
create_snapshot(instance_id, state):
  source = 取源(state)              // stopped: 原目录; running: 本地冻结拷贝
  prev   = 读上一份 manifest        // get_latest_snapshot → manifest_uri → 下载
  entries = []
  for 每个文件 (relpath, mode, size) in walk(source):
    sha = sha256(file)
    object_key = "objects/{sha}"
    if object_key 不存在:            // 快路径：prev.entries 有则跳过；权威：object_exists
      put_object(object_key, file)
    entries.push({path, mode, size, object_key, sha256: sha})
  manifest = { ...entries }
  put_object("snapshots/{snapshot_id}/manifest.json", manifest)   // 提交点
  running: 删除本地冻结拷贝
```

- 增量判定：**每次备份全量做 content-hash**，hash 相同 → 跳过上传。几百 MB 规模本地 sha256 约 1~2s，可接受，正确性最好（不依赖 mtime）。
- 上传短路：先查 `prev.entries`（快路径，省掉网络请求），再以 `object_exists("objects/{sha}")` 为权威判定；已存在则直接引用（幂等，重复快照零重传）。

### 恢复

```
restore(snapshot_id, dest_dir, subset?):
  manifest = 读 snapshots/{snapshot_id}/manifest.json
  for entry in manifest.entries（subset 过滤，subset = 要恢复的路径前缀列表，None = 全量）:
    data = get_object(objects/{entry.sha256})
    校验 sha256 == entry.sha256        // 不一致 → 报"数据损坏"
    写 dest_dir/{entry.path}，建目录、设 mode
  成功即完整；中途失败返回错误，调用方可清空重试（覆盖写幂等、可重入）
```

### GC / 保留策略

- 保留最近 **N** 份快照，N 由调用方（asset_service）传入。
- GC 流程：
  1. 收集所有保留 manifest 引用的 object_key 集合
  2. `list_objects("objects/")` 取全部 object
  3. 删除未被任何保留 manifest 引用的 object
- 增量判定只依赖"object 是否存在"，与 GC 时机解耦。

## 一致性取源

| 实例状态 | 取源方式 |
|---|---|
| stopped | 直接使用原目录 `host_data_path` |
| running | 先本地冻结拷贝：优先 `cp --reflink=auto`（COW，秒级），fallback `cp -r`（几百 MB 本地几秒）；上传完成删除临时目录 |

游戏**不暂停**。冻结拷贝把"一致性"与"上传耗时"解耦 —— 拷贝是点时间一致的，上传可以慢慢来。

## 组件与接口

### `ObjectStore` trait 扩展（`ports/object_store.rs`）

```rust
pub trait ObjectStore: Send + Sync {
    async fn put_object(&self, bucket, key, body: Vec<u8>) -> Result<(), Box<dyn Error>>;
    async fn get_object(&self, bucket, key) -> Result<Vec<u8>, Box<dyn Error>>;
    async fn object_exists(&self, bucket, key) -> Result<bool, Box<dyn Error>>;  // 新增
    async fn list_objects(&self, prefix) -> Result<Vec<String>, Box<dyn Error>>; // 新增
    async fn delete_object(&self, bucket, key) -> Result<(), Box<dyn Error>>;    // 新增
}
```

实现方：`S3ObjectStore`（`clients/s3_object_store.rs`）、`InMemoryObjectStore`（`providers/in_memory_object_store.rs`）。

单文件保持 `Vec<u8>`（当前规模内存可接受）。超大文件流式留作扩展点，本次不做。

### `DirectoryUploadDownloadService` 重构

```rust
pub struct DirectoryUploadDownloadService {
    object_store: Arc<dyn ObjectStore>,
}

impl DirectoryUploadDownloadService {
    pub fn new(object_store: Arc<dyn ObjectStore>) -> Self;

    // 快照创建（增量上传 + 写 manifest 提交点）
    pub async fn create_snapshot(
        &self,
        src_dir: impl AsRef<Path>,       // 取源后的冻结视图
        instance_id: &str,
        snapshot_id: &str,
        previous_manifest: Option<&Manifest>,
    ) -> Result<Manifest, ...>;

    // 恢复
    pub async fn restore_snapshot(
        &self,
        manifest: &Manifest,
        dest_dir: impl AsRef<Path>,
        subset: Option<&[String]>,       // 可选部分恢复
    ) -> Result<(), ...>;

    // GC：删除未被 retained_manifests 引用的 objects
    pub async fn garbage_collect(
        &self,
        retained_manifests: &[Manifest],
    ) -> Result<usize, ...>;
}
```

删除旧的 `upload_dir_as_tar_zst` / `download_and_extract_tar_zst` / `create_snapshot_archive` 方法（tar.zst 整包逻辑）。

### `NodeAgentService` 衔接

- `create_snapshot`（现 `todo!()`）：
  1. 判实例状态（running / stopped）→ 决定取源方式
  2. running 时调用冻结拷贝（reflink/cp 到本地临时目录）
  3. 取上一份 manifest：`asset_service.get_latest_snapshot(instance_id)` → `manifest_uri` → `download_manifest`
  4. 调 `directory_service.create_snapshot(...)`
  5. 更新 `SnapshotRecord`（storage_uri/manifest_uri 指向新 manifest）
- `clean_instance`：停机场景，复用 `create_snapshot` 的上传逻辑（或直接调同一路径）
- `restore_snapshot`：读 manifest → `directory_service.restore_snapshot(...)`

### 冻结拷贝工具

新增小工具（`service/` 内私有）：`freeze_copy(src, dst) -> Result<()>`，优先 reflink，fallback 递归拷贝，结束后清理。

## 集成点（与 asset_service 对齐）

1. **`manifest_uri` 语义**：`SnapshotRecord.manifest_uri` 指向 `snapshots/{snapshot_id}/manifest.json`（旧布局的 `{prefix}.manifest.json` 与 `.tar.zst` 不再存在）。
2. **`storage_uri` 语义**：无单归档后，`storage_uri` 改为指向 manifest（或弃用，由 manifest_uri 取代）——需与 asset_service 确认。
3. **保留份数 N**：由调用方传入（`complete_snapshot_record` 或单独接口），node_agent 不做默认决策。
4. **统一 bucket**：全局去重要求快照共用同一 bucket；当前 `SnapshotRecord.bucket` 保持。

## 错误处理

| 场景 | 行为 |
|---|---|
| 上传中断 | manifest 未写 → 该快照不存在，孤儿 objects 由 GC 清理 |
| 恢复中断 | 幂等可重入（覆盖写），返回错误，调用方清空目标目录重试 |
| 哈希不匹配 | 报"数据损坏"，返回错误（可选重试） |
| 单文件失败 | 默认 fail-fast（简单、明确） |

## 测试

- 扩展 `InMemoryObjectStore`：`object_exists` / `list_objects` / `delete_object`
- 单元测试：
  - 增量：第二次快照只上传变化的文件（断言上传次数）
  - 去重：相同文件只产生一个 object
  - 恢复：哈希不匹配报错；子集恢复
  - GC：删除未被引用 object，保留被引用 object
  - 取源：stopped 用原目录 / running 走冻结拷贝（mock reflink/cp）
- 集成测试：真实临时目录 + `InMemoryObjectStore` 全流程（创建 → 增量 → 恢复 → GC）

## 非目标（YAGNI）

- 分块级（chunk）去重：当前文件级足够，数据规模下收益低
- 超大单文件流式：单文件仍为 `Vec<u8>`，留扩展点
- 游戏适配器 quiesce（如 MC save-off）：留作未来扩展，不阻塞本次
- 并发恢复/并发上传：先顺序，后续可加
- skip-failed 部分成功模式：本次 fail-fast
