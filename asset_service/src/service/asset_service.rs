use std::sync::Arc;

use crate::{
    domain::{
        instance_data_path, validate_adapter_schema, AdapterId, AdapterSchema, AdapterVersion,
        BuildCompatibility, BuildId, BuildStatus, GameBuild, ModManifest, ModManifestId,
        SnapshotId, SnapshotRecord, SnapshotRestorePlan, SnapshotStatus, SnapshotType,
        VersionSelector,
    },
    error::AssetServiceError,
    ports::{
        BuildRepository, Clock, GameRepository, ModManifestRepository, SnapshotRepository,
    },
};

#[derive(Debug, Clone)]
pub struct RegisterBuildRequest {
    /// 增量注册（迭代语义）：仅 artifact_image_tag 必填，其余字段未显式设置时从 base 继承
    pub build: GameBuild,
    /// 迭代基准 build_id；缺省 = 同 channel 最新 Available（无则按全新注册处理）
    pub base_build_id: Option<String>,
}

impl RegisterBuildRequest {
    /// 全新注册（无迭代基准）
    pub fn new(build: GameBuild) -> Self {
        Self {
            build,
            base_build_id: None,
        }
    }
}

#[derive(Debug, Clone)]
pub struct CreateSnapshotRequest {
    pub instance_id: String,
    pub build_id: Option<String>,
    pub snapshot_type: SnapshotType,
    pub source_node: Option<String>,
}

#[derive(Debug, Clone)]
pub struct CompleteSnapshotRequest {
    pub snapshot_id: String,
    pub storage_uri: String,
    pub manifest_uri: Option<String>,
    pub checksum: Option<String>,
}

#[derive(Debug, Clone)]
pub struct FailSnapshotRequest {
    pub snapshot_id: String,
    pub failure_message: String,
}

pub struct AssetService<B, S, M, C, G>
where
    B: BuildRepository,
    S: SnapshotRepository,
    M: ModManifestRepository,
    C: Clock,
    G: GameRepository,
{
    builds: Arc<B>,
    snapshots: Arc<S>,
    manifests: Arc<M>,
    clock: Arc<C>,
    game_repository: Arc<G>,
}

impl<B, S, M, C, G> AssetService<B, S, M, C, G>
where
    B: BuildRepository,
    S: SnapshotRepository,
    M: ModManifestRepository,
    C: Clock,
    G: GameRepository,
{
    pub fn new(
        builds: Arc<B>,
        snapshots: Arc<S>,
        manifests: Arc<M>,
        clock: Arc<C>,
        game_repository: Arc<G>,
    ) -> Self {
        Self {
            builds,
            snapshots,
            manifests,
            clock,
            game_repository,
        }
    }

    pub async fn resolve_game_build(
        &self,
        game_id: &str,
        selector: VersionSelector,
    ) -> Result<GameBuild, AssetServiceError> {
        let build = match selector {
            VersionSelector::BuildId { build_id } => self
                .builds
                .get(&BuildId(build_id.clone()))
                .await?
                .ok_or(AssetServiceError::BuildNotFound { build_id }),
            VersionSelector::Channel { channel } => {
                let candidates = self.builds.list_by_game(game_id).await?;
                candidates
                    .into_iter()
                    .filter(|build| {
                        build.channel.as_deref() == Some(channel.as_str())
                            && matches!(
                                build.status,
                                BuildStatus::Available | BuildStatus::Deprecated
                            )
                    })
                    // Available 优先于 Deprecated，同状态内按镜像 tag 版本号取最新
                    .max_by(|a, b| {
                        let a_rank = if a.status == BuildStatus::Available {
                            1
                        } else {
                            0
                        };
                        let b_rank = if b.status == BuildStatus::Available {
                            1
                        } else {
                            0
                        };
                        match a_rank.cmp(&b_rank) {
                            std::cmp::Ordering::Equal => {
                                compare_image_tags(&a.artifact_image_tag, &b.artifact_image_tag)
                            }
                            ord => ord,
                        }
                    })
                    .ok_or_else(|| AssetServiceError::BuildNotFound {
                        build_id: format!("{game_id}:{channel}"),
                    })
            }
        }?;

        // 适配器元数据/schema 随构建存储，随构建返回（无需二次查询）
        Ok(build)
    }

    /// 注册游戏构建（增量迭代语义）。
    ///
    /// 规则：
    /// 1. `artifact_image_tag` 必填（新版本身份）；`build_id` 一律由系统按
    ///    `{game_id}-{channel}-{tag}` 生成，请求中携带自定义 build_id → 拒绝；
    /// 2. `base_build_id` 指定迭代基准（缺省 = 同 channel 最新 Available）；请求中
    ///    未显式设置的字段（channel / adapter_id / adapter_version / upstream_version /
    ///    artifact_uri / artifact_image_name / adapter_metadata / schema_json / pinned）
    ///    从 base 继承，避免重复录入旧字段；
    /// 3. 携带 schema 时校验契约，并以其 adapter_id 作为权威（build 未显式提供时继承，
    ///    显式提供且冲突 → 拒绝）；
    /// 4. 同 build_id（同 tag 重传）幂等覆盖更新；新版本将同 channel 旧 Available
    ///    （非 pinned）标为 Deprecated。
    pub async fn register_game_build(
        &self,
        request: RegisterBuildRequest,
    ) -> Result<GameBuild, AssetServiceError> {
        let mut build = request.build;

        // build_id 不可自定义：由 {game_id}-{channel}-{tag} 规则生成
        let tag =
            build
                .artifact_image_tag
                .clone()
                .ok_or_else(|| AssetServiceError::InvalidRequest {
                    message: "artifact_image_tag is required to version a game build".to_string(),
                })?;
        if tag.trim().is_empty() {
            return Err(AssetServiceError::InvalidRequest {
                message: "artifact_image_tag must not be empty".to_string(),
            });
        }
        if build.game_id.trim().is_empty() {
            return Err(AssetServiceError::InvalidRequest {
                message: "game_id is required".to_string(),
            });
        }

        // 解析迭代基准：显式 base_build_id，或同 channel 最新 Available
        let base: Option<GameBuild> = match &request.base_build_id {
            Some(base_id) if !base_id.trim().is_empty() => {
                let base = self
                    .builds
                    .get(&BuildId(base_id.clone()))
                    .await?
                    .ok_or_else(|| AssetServiceError::BuildNotFound {
                        build_id: base_id.clone(),
                    })?;
                if let Some(req_channel) = &build.channel {
                    if base.channel.as_deref() != Some(req_channel.as_str()) {
                        return Err(AssetServiceError::InvalidRequest {
                            message: format!(
                                "迭代基准 {} 属于 channel {:?}，与请求 channel {:?} 不一致",
                                base_id, base.channel, build.channel
                            ),
                        });
                    }
                }
                Some(base)
            }
            _ => {
                let channel = build.channel.clone();
                let candidates = self.builds.list_by_game(&build.game_id).await?;
                candidates.into_iter().find(|c| {
                    c.channel == channel && c.status == BuildStatus::Available
                })
            }
        };

        // 增量合并：未显式设置的字段从 base 继承
        if let Some(base) = &base {
            if build.channel.is_none() {
                build.channel = base.channel.clone();
            }
            if build.adapter_id.0.is_empty() {
                build.adapter_id = base.adapter_id.clone();
            }
            if build.adapter_version == AdapterVersion::new(0, 0, 0) {
                build.adapter_version = base.adapter_version.clone();
            }
            if build.upstream_version.is_none() {
                build.upstream_version = base.upstream_version.clone();
            }
            if build.artifact_uri.is_none() {
                build.artifact_uri = base.artifact_uri.clone();
            }
            if build.artifact_image_name.is_none() {
                build.artifact_image_name = base.artifact_image_name.clone();
            }
            if build.adapter_metadata.is_none() {
                build.adapter_metadata = base.adapter_metadata.clone();
            }
            if build.schema_json.is_none() {
                build.schema_json = base.schema_json.clone();
            }
            if !build.pinned {
                build.pinned = base.pinned;
            }
        }

        // schema 契约校验（合并后的最终形态）：反序列化 + 规则校验。
        // 收敛模型：schema 是 adapter_id 的权威来源 —— build 未显式携带 adapter_id 时
        // 从 schema 继承；两者都显式提供且不一致才算冲突。
        if let Some(schema_json) = &build.schema_json {
            let schema: AdapterSchema = serde_json::from_str(schema_json).map_err(|e| {
                AssetServiceError::InvalidRequest {
                    message: format!("schema_json 解析失败: {e}"),
                }
            })?;
            validate_adapter_schema(&schema)
                .map_err(|message| AssetServiceError::InvalidRequest { message })?;
            if build.adapter_id.0.is_empty() {
                build.adapter_id = AdapterId(schema.adapter_id.clone());
            } else if schema.adapter_id != build.adapter_id.0 {
                return Err(AssetServiceError::InvalidRequest {
                    message: format!(
                        "schema.adapter_id ({}) 与 build.adapter_id ({}) 不一致",
                        schema.adapter_id, build.adapter_id.0
                    ),
                });
            }
        }

        // build_id 由系统生成：{game_id}-{channel}-{tag}，请求传入的 build_id 被忽略
        // 并校验——非空且不等于生成值视为尝试自定义，拒绝。
        let channel = build.channel.clone().unwrap_or_default();
        let generated = BuildId(if channel.is_empty() {
            format!("{}-{}", build.game_id, tag)
        } else {
            format!("{}-{}-{}", build.game_id, channel, tag)
        });
        if !build.build_id.0.is_empty() && build.build_id != generated {
            return Err(AssetServiceError::InvalidRequest {
                message: format!(
                    "build_id 由系统按 {{game_id}}-{{channel}}-{{tag}} 规则生成，不可自定义: {}",
                    build.build_id.0
                ),
            });
        }
        build.build_id = generated;

        // 已存在同 build_id（同 tag 幂等重传）→ 仅覆盖更新，不触发 Deprecated 逻辑
        if self.builds.get(&build.build_id).await?.is_some() {
            build.status = BuildStatus::Available;
            build.updated_at = self.clock.now();
            self.builds.save(&build).await?;
            return Ok(build);
        }

        // 新版本：注册为 Available，同 channel 旧 Available（非 pinned）标为 Deprecated
        build.status = BuildStatus::Available;
        let now = self.clock.now();
        let candidates = self.builds.list_by_game(&build.game_id).await?;
        for mut old in candidates {
            if old.build_id != build.build_id
                && old.channel.as_deref() == build.channel.as_deref()
                && old.status == BuildStatus::Available
                && !old.pinned
            {
                old.status = BuildStatus::Deprecated;
                old.updated_at = now;
                self.builds.save(&old).await?;
            }
        }

        self.builds.save(&build).await?;
        Ok(build)
    }

    pub async fn get_game_build(&self, build_id: &str) -> Result<GameBuild, AssetServiceError> {
        self.builds
            .get(&BuildId(build_id.to_string()))
            .await?
            .ok_or_else(|| AssetServiceError::BuildNotFound {
                build_id: build_id.to_string(),
            })
    }

    /// 列出某游戏的全部构建（可按 channel 过滤，结果按创建时间倒序）
    pub async fn list_game_builds(
        &self,
        game_id: &str,
        channel: Option<&str>,
    ) -> Result<Vec<GameBuild>, AssetServiceError> {
        let mut builds = self.builds.list_by_game(game_id).await?;
        if let Some(ch) = channel {
            builds.retain(|b| b.channel.as_deref() == Some(ch));
        }
        builds.sort_by(|a, b| b.created_at.cmp(&a.created_at));
        Ok(builds)
    }

    pub async fn create_snapshot(
        &self,
        request: CreateSnapshotRequest,
    ) -> Result<SnapshotRecord, AssetServiceError> {
        let now = self.clock.now();
        let instance_data_path = instance_data_path(&request.instance_id);
        let snapshot = SnapshotRecord {
            snapshot_id: SnapshotId::new(),
            instance_id: request.instance_id,
            build_id: request.build_id.map(BuildId),
            snapshot_type: request.snapshot_type,
            instance_data_path,
            storage_uri: None,
            manifest_uri: None,
            checksum: None,
            status: SnapshotStatus::Pending,
            source_node: request.source_node,
            created_at: now,
            completed_at: None,
            failure_message: None,
            bucket: "cluster".to_string(), // 快照统一存储 bucket（当前写死，后续可改为配置）
            key: String::new(),
            host: String::new(),
            host_port: 0,
        };
        self.snapshots.save(&snapshot).await?;
        Ok(snapshot)
    }

    pub async fn complete_snapshot(
        &self,
        request: CompleteSnapshotRequest,
    ) -> Result<SnapshotRecord, AssetServiceError> {
        let id = SnapshotId(request.snapshot_id.clone());
        let mut snapshot =
            self.snapshots
                .get(&id)
                .await?
                .ok_or(AssetServiceError::SnapshotNotFound {
                    snapshot_id: request.snapshot_id,
                })?;
        snapshot.storage_uri = Some(request.storage_uri);
        snapshot.manifest_uri = request.manifest_uri;
        snapshot.checksum = request.checksum;
        snapshot.status = SnapshotStatus::Completed;
        snapshot.completed_at = Some(self.clock.now());
        snapshot.failure_message = None;
        self.snapshots.save(&snapshot).await?;
        self.snapshots
            .set_latest(&snapshot.instance_id, &snapshot.snapshot_id)
            .await?;
        Ok(snapshot)
    }

    pub async fn fail_snapshot(
        &self,
        request: FailSnapshotRequest,
    ) -> Result<SnapshotRecord, AssetServiceError> {
        let id = SnapshotId(request.snapshot_id.clone());
        let mut snapshot =
            self.snapshots
                .get(&id)
                .await?
                .ok_or(AssetServiceError::SnapshotNotFound {
                    snapshot_id: request.snapshot_id,
                })?;
        snapshot.status = SnapshotStatus::Failed;
        snapshot.failure_message = Some(request.failure_message);
        snapshot.completed_at = Some(self.clock.now());
        self.snapshots.save(&snapshot).await?;
        Ok(snapshot)
    }

    pub async fn get_snapshot(
        &self,
        snapshot_id: &str,
    ) -> Result<SnapshotRecord, AssetServiceError> {
        self.snapshots
            .get(&SnapshotId(snapshot_id.to_string()))
            .await?
            .ok_or_else(|| AssetServiceError::SnapshotNotFound {
                snapshot_id: snapshot_id.to_string(),
            })
    }

    pub async fn get_snapshot_restore_plan(
        &self,
        snapshot_id: &str,
    ) -> Result<SnapshotRestorePlan, AssetServiceError> {
        let snapshot = self.get_snapshot(snapshot_id).await?;
        if snapshot.status != SnapshotStatus::Completed {
            return Err(AssetServiceError::Conflict {
                message: format!(
                    "snapshot {} is not restorable until it reaches Completed",
                    snapshot_id
                ),
            });
        }

        let storage_uri =
            snapshot
                .storage_uri
                .clone()
                .ok_or_else(|| AssetServiceError::Conflict {
                    message: format!("snapshot {} is missing storage_uri", snapshot_id),
                })?;

        Ok(SnapshotRestorePlan {
            snapshot_id: snapshot.snapshot_id,
            build_id: snapshot.build_id,
            storage_uri,
            manifest_uri: snapshot.manifest_uri,
            checksum: snapshot.checksum,
            instance_data_path: snapshot.instance_data_path,
        })
    }

    pub async fn list_snapshots(
        &self,
        instance_id: &str,
    ) -> Result<Vec<SnapshotRecord>, AssetServiceError> {
        self.snapshots.list_by_instance(instance_id).await
    }

    pub async fn get_latest_snapshot(
        &self,
        instance_id: &str,
    ) -> Result<Option<SnapshotRecord>, AssetServiceError> {
        self.snapshots.get_latest(instance_id).await
    }

    pub async fn set_latest_snapshot(
        &self,
        instance_id: &str,
        snapshot_id: &str,
    ) -> Result<SnapshotRecord, AssetServiceError> {
        let id = SnapshotId(snapshot_id.to_string());
        self.snapshots.set_latest(instance_id, &id).await?;
        self.snapshots
            .get(&id)
            .await?
            .ok_or_else(|| AssetServiceError::SnapshotNotFound {
                snapshot_id: snapshot_id.to_string(),
            })
    }

    pub async fn register_mod_manifest(
        &self,
        manifest: ModManifest,
    ) -> Result<ModManifest, AssetServiceError> {
        self.manifests.save(&manifest).await?;
        Ok(manifest)
    }

    pub async fn get_mod_manifest(
        &self,
        manifest_id: &str,
    ) -> Result<ModManifest, AssetServiceError> {
        self.manifests
            .get(&ModManifestId(manifest_id.to_string()))
            .await?
            .ok_or_else(|| AssetServiceError::ModManifestNotFound {
                manifest_id: manifest_id.to_string(),
            })
    }

    pub async fn check_build_mod_compatibility(
        &self,
        build_id: &str,
        manifest_id: &str,
    ) -> Result<BuildCompatibility, AssetServiceError> {
        let build = self.get_game_build(build_id).await?;
        let manifest = self.get_mod_manifest(manifest_id).await?;
        let compatible = build.game_id == manifest.game_id;

        Ok(BuildCompatibility {
            compatible,
            reason: if compatible {
                None
            } else {
                Some("build game kind does not match mod manifest game kind".to_string())
            },
        })
    }
}

/// 解析镜像 tag 中的数字版本段，如 `"0.2.2"` → `[0, 2, 2]`。
/// 无法提取出任何数字段（如 `"demo-upstream"`）时返回 `None`，交由字符串比较兜底。
fn parse_numeric_version(tag: &str) -> Option<Vec<u64>> {
    let mut nums = Vec::new();
    for part in tag.split(|c: char| !c.is_ascii_digit()) {
        if part.is_empty() {
            continue;
        }
        nums.push(part.parse().ok()?);
    }
    if nums.is_empty() {
        None
    } else {
        Some(nums)
    }
}

/// 比较两个镜像 tag 的版本高低（`None` 视为最低）。
///
/// 优先按数字段逐位比较（缺省补 0，保证 `0.2.10` > `0.2.2`）；
/// 双方都能解析数字时按版本号比较，否则回退到字典序。
fn compare_image_tags(a: &Option<String>, b: &Option<String>) -> std::cmp::Ordering {
    match (a.as_deref(), b.as_deref()) {
        (None, None) => std::cmp::Ordering::Equal,
        (None, Some(_)) => std::cmp::Ordering::Less,
        (Some(_), None) => std::cmp::Ordering::Greater,
        (Some(ta), Some(tb)) => match (parse_numeric_version(ta), parse_numeric_version(tb)) {
            (Some(x), Some(y)) => {
                let max = x.len().max(y.len());
                for i in 0..max {
                    let xi = x.get(i).copied().unwrap_or(0);
                    let yi = y.get(i).copied().unwrap_or(0);
                    match xi.cmp(&yi) {
                        std::cmp::Ordering::Equal => continue,
                        ord => return ord,
                    }
                }
                std::cmp::Ordering::Equal
            }
            // 能解析出数字段的版本视为更高，否则回退字典序
            (Some(_), None) => std::cmp::Ordering::Greater,
            (None, Some(_)) => std::cmp::Ordering::Less,
            (None, None) => ta.cmp(tb),
        },
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::{
        domain::{AdapterId, AdapterVersion, BuildId, BuildStatus, GameBuild},
        ports::SystemClock,
        repositories::{
            InMemoryBuildRepository, InMemoryGameRepository, InMemoryModManifestRepository,
            InMemorySnapshotRepository,
        },
    };
    use chrono::Utc;
    use std::sync::Arc;

    fn make_build(
        game_id: &str,
        channel: Option<&str>,
        tag: &str,
        now: chrono::DateTime<chrono::Utc>,
    ) -> GameBuild {
        GameBuild {
            // 系统生成：build_id 由 {game_id}-{channel}-{tag} 规则派生，测试里留空
            build_id: BuildId(String::new()),
            game_id: game_id.to_string(),
            channel: channel.map(|s| s.to_string()),
            adapter_id: AdapterId("adapter".to_string()),
            adapter_version: AdapterVersion::new(0, 1, 0),
            upstream_version: None,
            artifact_uri: Some("reg.example.com".to_string()),
            artifact_image_name: Some("adapter-img".to_string()),
            artifact_image_tag: Some(tag.to_string()),
            status: BuildStatus::Available,
            pinned: false,
            adapter_metadata: None,
            schema_json: None,
            resolved_at: now,
            created_at: now,
            updated_at: now,
        }
    }

    type TestAssetService = AssetService<
        InMemoryBuildRepository,
        InMemorySnapshotRepository,
        InMemoryModManifestRepository,
        SystemClock,
        InMemoryGameRepository,
    >;

    fn new_service() -> TestAssetService {
        AssetService::new(
            Arc::new(InMemoryBuildRepository::default()),
            Arc::new(InMemorySnapshotRepository::default()),
            Arc::new(InMemoryModManifestRepository::default()),
            Arc::new(SystemClock),
            Arc::new(InMemoryGameRepository::default()),
        )
    }

    #[test]
    fn test_parse_numeric_version() {
        assert_eq!(parse_numeric_version("0.2.2"), Some(vec![0, 2, 2]));
        assert_eq!(parse_numeric_version("v1.0.0"), Some(vec![1, 0, 0]));
        assert_eq!(parse_numeric_version("demo-upstream"), None);
        assert_eq!(parse_numeric_version(""), None);
    }

    #[test]
    fn test_compare_image_tags() {
        // 数字段逐位比较：0.2.10 > 0.2.2
        assert!(
            compare_image_tags(&Some("0.2.10".to_string()), &Some("0.2.2".to_string()))
                == std::cmp::Ordering::Greater
        );
        // 1.0.0 > 0.9.9
        assert!(
            compare_image_tags(&Some("1.0.0".to_string()), &Some("0.9.9".to_string()))
                == std::cmp::Ordering::Greater
        );
        // None 视为最低
        assert!(compare_image_tags(&None, &Some("0.2.2".to_string())) == std::cmp::Ordering::Less);
        // 无数字段 vs 有数字段：有数字段更高
        assert!(
            compare_image_tags(
                &Some("demo-upstream".to_string()),
                &Some("0.2.2".to_string())
            ) == std::cmp::Ordering::Less
        );
        // 双方都无数字段：回退字典序
        assert!(
            compare_image_tags(&Some("beta".to_string()), &Some("alpha".to_string()))
                == std::cmp::Ordering::Greater
        );
        // 相等
        assert!(
            compare_image_tags(&Some("0.2.2".to_string()), &Some("0.2.2".to_string()))
                == std::cmp::Ordering::Equal
        );
    }

    #[tokio::test]
    async fn test_register_generates_versioned_build_id() {
        let service = new_service();
        let now = Utc::now();
        let build = service
            .register_game_build(RegisterBuildRequest::new(
                make_build("dst", Some("public"), "0.2.2", now),
            ))
            .await
            .unwrap();
        assert_eq!(build.build_id, BuildId("dst-public-0.2.2".to_string()));
        assert_eq!(build.status, BuildStatus::Available);
    }

    #[tokio::test]
    async fn test_register_deprecates_old_channel_version() {
        let service = new_service();
        let now = Utc::now();

        let v1 = service
            .register_game_build(RegisterBuildRequest::new(
                make_build("dst", Some("public"), "0.2.2", now),
            ))
            .await
            .unwrap();
        assert_eq!(v1.status, BuildStatus::Available);

        let v2 = service
            .register_game_build(RegisterBuildRequest::new(
                make_build("dst", Some("public"), "0.3.0", now),
            ))
            .await
            .unwrap();
        assert_eq!(v2.status, BuildStatus::Available);

        // 旧版本被标为 Deprecated，但记录保留
        let old = service.get_game_build("dst-public-0.2.2").await.unwrap();
        assert_eq!(old.status, BuildStatus::Deprecated);
        // 新版本仍为 Available
        let new = service.get_game_build("dst-public-0.3.0").await.unwrap();
        assert_eq!(new.status, BuildStatus::Available);
    }

    #[tokio::test]
    async fn test_register_same_tag_is_idempotent() {
        let service = new_service();
        let now = Utc::now();

        service
            .register_game_build(RegisterBuildRequest::new(
                make_build("dst", Some("public"), "0.3.0", now),
            ))
            .await
            .unwrap();
        // 同 tag 重复注册：不触发 Deprecated（没有其他 build 被误标）
        service
            .register_game_build(RegisterBuildRequest::new(
                make_build("dst", Some("public"), "0.3.0", now),
            ))
            .await
            .unwrap();

        let builds = service.builds.list_by_game("dst").await.unwrap();
        assert_eq!(builds.len(), 1);
        assert_eq!(builds[0].status, BuildStatus::Available);
    }

    #[tokio::test]
    async fn test_register_different_channel_not_deprecated() {
        let service = new_service();
        let now = Utc::now();

        service
            .register_game_build(RegisterBuildRequest::new(
                make_build("dst", Some("public"), "0.2.2", now),
            ))
            .await
            .unwrap();
        // beta channel 发布不影响 public channel 的可用性
        service
            .register_game_build(RegisterBuildRequest::new(
                make_build("dst", Some("beta"), "0.4.0", now),
            ))
            .await
            .unwrap();

        let public = service.get_game_build("dst-public-0.2.2").await.unwrap();
        assert_eq!(public.status, BuildStatus::Available);
    }

    #[tokio::test]
    async fn test_resolve_channel_returns_highest_tag() {
        let service = new_service();
        let now = Utc::now();

        service
            .register_game_build(RegisterBuildRequest::new(
                make_build("dst", Some("public"), "0.2.2", now),
            ))
            .await
            .unwrap();
        service
            .register_game_build(RegisterBuildRequest::new(
                make_build("dst", Some("public"), "0.10.0", now),
            ))
            .await
            .unwrap();

        let resolved = service
            .resolve_game_build(
                "dst",
                VersionSelector::Channel {
                    channel: "public".to_string(),
                },
            )
            .await
            .unwrap();
        assert_eq!(resolved.build_id, BuildId("dst-public-0.10.0".to_string()));
    }

    #[tokio::test]
    async fn test_resolve_prefers_available_over_deprecated() {
        let service = new_service();
        let now = Utc::now();

        // 先注册旧版本，再注册新版本 → 旧版本变 Deprecated，新版本 Available
        service
            .register_game_build(RegisterBuildRequest::new(
                make_build("dst", Some("public"), "0.2.2", now),
            ))
            .await
            .unwrap();
        service
            .register_game_build(RegisterBuildRequest::new(
                make_build("dst", Some("public"), "0.3.0", now),
            ))
            .await
            .unwrap();

        // 正常情况下返回 Available 的新版本
        let resolved = service
            .resolve_game_build(
                "dst",
                VersionSelector::Channel {
                    channel: "public".to_string(),
                },
            )
            .await
            .unwrap();
        assert_eq!(resolved.build_id, BuildId("dst-public-0.3.0".to_string()));

        // 模拟回滚：把最新版标为 Unavailable，resolve 应回退到 Deprecated 的旧版本
        let mut latest = service.get_game_build("dst-public-0.3.0").await.unwrap();
        latest.status = BuildStatus::Unavailable;
        service.builds.save(&latest).await.unwrap();

        let resolved = service
            .resolve_game_build(
                "dst",
                VersionSelector::Channel {
                    channel: "public".to_string(),
                },
            )
            .await
            .unwrap();
        assert_eq!(resolved.build_id, BuildId("dst-public-0.2.2".to_string()));
        assert_eq!(resolved.status, BuildStatus::Deprecated);
    }

    #[tokio::test]
    async fn test_resolve_ignores_unavailable_and_deleted() {
        let service = new_service();
        let now = Utc::now();

        service
            .register_game_build(RegisterBuildRequest::new(
                make_build("dst", Some("public"), "0.2.2", now),
            ))
            .await
            .unwrap();
        service
            .register_game_build(RegisterBuildRequest::new(
                make_build("dst", Some("public"), "0.3.0", now),
            ))
            .await
            .unwrap();

        // 把 0.3.0 标为 Unavailable（如上游删除），0.2.2 仍是 Deprecated
        let mut latest = service.get_game_build("dst-public-0.3.0").await.unwrap();
        latest.status = BuildStatus::Unavailable;
        service.builds.save(&latest).await.unwrap();

        let resolved = service
            .resolve_game_build(
                "dst",
                VersionSelector::Channel {
                    channel: "public".to_string(),
                },
            )
            .await
            .unwrap();
        assert_eq!(resolved.build_id, BuildId("dst-public-0.2.2".to_string()));
    }

    #[tokio::test]
    async fn test_resolve_without_channel_build_id() {
        let service = new_service();
        let now = Utc::now();

        // 无 channel 的 build：build_id 只含 game_id + tag
        service
            .register_game_build(RegisterBuildRequest::new(
                make_build("custom", None, "1.0.0", now)))
            .await
            .unwrap();
        let build = service.get_game_build("custom-1.0.0").await.unwrap();
        assert_eq!(build.status, BuildStatus::Available);

        // 无 channel 也能注册第二个版本，且互不覆盖
        service
            .register_game_build(RegisterBuildRequest::new(
                make_build("custom", None, "1.1.0", now)))
            .await
            .unwrap();
        let v1 = service.get_game_build("custom-1.0.0").await.unwrap();
        assert_eq!(v1.status, BuildStatus::Deprecated);
        let v2 = service.get_game_build("custom-1.1.0").await.unwrap();
        assert_eq!(v2.status, BuildStatus::Available);
    }

    #[tokio::test]
    async fn test_register_requires_image_tag() {
        let service = new_service();
        let now = Utc::now();
        let mut build = make_build("dst", Some("public"), "0.2.2", now);
        build.artifact_image_tag = None;

        let err = service
            .register_game_build(RegisterBuildRequest::new(build))
            .await
            .unwrap_err();
        assert!(matches!(err, AssetServiceError::InvalidRequest { .. }));
    }

    // 收敛模型：schema 是 adapter_id 的权威来源。build 未显式携带 adapter_id 时
    // 应从 schema 继承，而不是报"不一致"错误（前端只需上传 schema.json）。
    #[tokio::test]
    async fn test_register_derives_adapter_id_from_schema() {
        let service = new_service();
        let now = Utc::now();
        let mut build = make_build("7daystodie", Some("public"), "0.4.0", now);
        build.adapter_id = AdapterId(String::new()); // 前端不上传 adapter_id
        build.schema_json = Some(
            serde_json::json!({
                "adapter_id": "7daystodie",
                "game_id": "7daystodie",
                "settings": [
                    {
                        "key": "ServerName",
                        "type": "string",
                        "control": "player",
                        "apply": "always",
                        "render": "xml_property",
                        "default": "My Server",
                        "min": null,
                        "max": null,
                        "enum_values": null,
                        "secret": false,
                        "label_key": null,
                        "description_key": null,
                        "group_key": null
                    }
                ],
                "render_file": "/data/serverconfig.xml",
                "i18n": {"fallback": "en"}
            })
            .to_string(),
        );

        let registered = service
            .register_game_build(RegisterBuildRequest::new(build))
            .await
            .unwrap();
        assert_eq!(registered.adapter_id, AdapterId("7daystodie".to_string()));
    }

    #[tokio::test]
    async fn test_register_adapter_id_conflict_is_rejected() {
        let service = new_service();
        let now = Utc::now();
        let mut build = make_build("7daystodie", Some("public"), "0.4.0", now);
        build.adapter_id = AdapterId("dst".to_string()); // 与 schema 冲突
        build.schema_json = Some(
            serde_json::json!({
                "adapter_id": "7daystodie",
                "game_id": "7daystodie",
                "settings": [],
                "render_file": null,
                "i18n": {"fallback": "en"}
            })
            .to_string(),
        );

        let err = service
            .register_game_build(RegisterBuildRequest::new(build))
            .await
            .unwrap_err();
        assert!(matches!(err, AssetServiceError::InvalidRequest { .. }));
    }

    // ── 迭代注册：build_id 系统生成 + 未显式字段从 base 继承 ──

    #[tokio::test]
    async fn test_register_rejects_custom_build_id() {
        let service = new_service();
        let now = Utc::now();
        let mut build = make_build("dst", Some("public"), "0.2.2", now);
        build.build_id = BuildId("my-custom-id".to_string()); // 系统生成，不可自定义

        let err = service
            .register_game_build(RegisterBuildRequest::new(build))
            .await
            .unwrap_err();
        assert!(matches!(err, AssetServiceError::InvalidRequest { .. }));
    }

    #[tokio::test]
    async fn test_register_iteration_inherits_from_channel_latest() {
        let service = new_service();
        let now = Utc::now();

        // 基线：携带上游版本 / 镜像名 / 适配器版本 / schema
        let mut base = make_build("7daystodie", Some("public"), "0.4.0", now);
        base.upstream_version = Some("1.0.1".to_string());
        base.artifact_image_name = Some("7daystodie-adapter".to_string());
        base.adapter_version = AdapterVersion::new(0, 4, 0);
        service
            .register_game_build(RegisterBuildRequest::new(base))
            .await
            .unwrap();

        // 迭代：只换镜像 tag，其余字段不重复提供 → 从同 channel 最新 Available 继承
        let mut iterated_build = make_build("7daystodie", Some("public"), "0.4.1", now);
        iterated_build.artifact_image_name = None; // 前端不提供 → 继承 base
        iterated_build.adapter_id = AdapterId(String::new()); // 前端不上传 → 继承 base
        iterated_build.adapter_version = AdapterVersion::new(0, 0, 0); // 未提供 → 继承 base
        let iterated = service
            .register_game_build(RegisterBuildRequest::new(iterated_build))
            .await
            .unwrap();
        assert_eq!(iterated.build_id, BuildId("7daystodie-public-0.4.1".to_string()));
        assert_eq!(iterated.upstream_version.as_deref(), Some("1.0.1"));
        assert_eq!(iterated.artifact_image_name.as_deref(), Some("7daystodie-adapter"));
        assert_eq!(iterated.adapter_version, AdapterVersion::new(0, 4, 0));
    }

    #[tokio::test]
    async fn test_register_iteration_explicit_base_build_id() {
        let service = new_service();
        let now = Utc::now();

        // 两个 channel 各有基线，显式指定 base_build_id 精确迭代
        let mut v1 = make_build("dst", Some("public"), "1.0.0", now);
        v1.upstream_version = Some("up-1".to_string());
        service
            .register_game_build(RegisterBuildRequest::new(v1))
            .await
            .unwrap();
        let mut beta = make_build("dst", Some("beta"), "1.0.0", now);
        beta.upstream_version = Some("up-beta".to_string());
        service
            .register_game_build(RegisterBuildRequest::new(beta))
            .await
            .unwrap();

        let iterated = service
            .register_game_build(RegisterBuildRequest {
                build: make_build("dst", Some("beta"), "1.0.1", now),
                base_build_id: Some("dst-beta-1.0.0".to_string()),
            })
            .await
            .unwrap();
        assert_eq!(iterated.build_id, BuildId("dst-beta-1.0.1".to_string()));
        assert_eq!(iterated.upstream_version.as_deref(), Some("up-beta"));
    }

    #[tokio::test]
    async fn test_register_iteration_cross_channel_rejected() {
        let service = new_service();
        let now = Utc::now();
        let mut base = make_build("dst", Some("public"), "1.0.0", now);
        service
            .register_game_build(RegisterBuildRequest::new(base))
            .await
            .unwrap();

        // 请求 channel 与 base 的 channel 不一致 → 拒绝
        let err = service
            .register_game_build(RegisterBuildRequest {
                build: make_build("dst", Some("beta"), "1.0.1", now),
                base_build_id: Some("dst-public-1.0.0".to_string()),
            })
            .await
            .unwrap_err();
        assert!(matches!(err, AssetServiceError::InvalidRequest { .. }));
    }

    #[tokio::test]
    async fn test_register_iteration_base_not_found() {
        let service = new_service();
        let now = Utc::now();
        let err = service
            .register_game_build(RegisterBuildRequest {
                build: make_build("dst", Some("public"), "1.0.1", now),
                base_build_id: Some("dst-public-0.0.1".to_string()),
            })
            .await
            .unwrap_err();
        assert!(matches!(err, AssetServiceError::BuildNotFound { .. }));
    }

    #[tokio::test]
    async fn test_register_iteration_keeps_base_schema_and_metadata() {
        let service = new_service();
        let now = Utc::now();

        // 基线带 schema + 端口注入元数据
        let mut base = make_build("7daystodie", Some("public"), "0.4.0", now);
        base.adapter_id = AdapterId(String::new()); // adapter_id 从 schema 派生
        base.schema_json = Some(
            serde_json::json!({
                "adapter_id": "7daystodie",
                "game_id": "7daystodie",
                "settings": [],
                "render_file": "/data/serverconfig.xml",
                "i18n": {"fallback": "en"}
            })
            .to_string(),
        );
        base.adapter_metadata = Some(crate::domain::AdapterMetadata {
            port_inject_env: Some("GAME_HOST_PORT".to_string()),
            start_script: "/scripts/start.sh".to_string(),
            save_script: "/scripts/save.sh".to_string(),
            stop_script: "/scripts/stop.sh".to_string(),
            players_script: "/scripts/players.sh".to_string(),
            health_script: "/scripts/health.sh".to_string(),
        });
        service
            .register_game_build(RegisterBuildRequest::new(base))
            .await
            .unwrap();

        // 迭代不重复上传 schema/metadata → 继承
        let mut iterated_build = make_build("7daystodie", Some("public"), "0.4.1", now);
        iterated_build.adapter_id = AdapterId(String::new()); // 前端不上传 → 继承 base
        let iterated = service
            .register_game_build(RegisterBuildRequest::new(iterated_build))
            .await
            .unwrap();
        assert!(iterated.schema_json.is_some());
        assert_eq!(
            iterated.adapter_metadata.as_ref().and_then(|m| m.port_inject_env.as_deref()),
            Some("GAME_HOST_PORT")
        );
    }
}
