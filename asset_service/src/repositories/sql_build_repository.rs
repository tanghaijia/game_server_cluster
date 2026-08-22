use async_trait::async_trait;
use sqlx::PgPool;

use crate::{
    domain::{AdapterVersion, BuildId, GameBuild},
    error::AssetServiceError,
    ports::BuildRepository,
};

pub struct SqlBuildRepository {
    pool: PgPool,
}

impl SqlBuildRepository {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl BuildRepository for SqlBuildRepository {
    async fn save(&self, build: &GameBuild) -> Result<(), AssetServiceError> {
        let metadata_json = build
            .adapter_metadata
            .as_ref()
            .map(|m| serde_json::to_string(m))
            .transpose()
            .map_err(|e| AssetServiceError::Internal {
                message: format!("serialize adapter metadata: {e}"),
            })?
            .unwrap_or_else(|| "{}".to_string());

        sqlx::query(
            r#"
            INSERT INTO t_asset_service_game_builds (
                build_id, game_id, channel,
                adapter_id, adapter_version_major, adapter_version_minor, adapter_version_patch,
                upstream_version, artifact_uri, artifact_image_name, artifact_image_tag,
                status, pinned, resolved_at, created_at, updated_at,
                metadata_json, schema_json
            ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
            ON CONFLICT (build_id) DO UPDATE SET
                game_id               = EXCLUDED.game_id,
                channel               = EXCLUDED.channel,
                adapter_id            = EXCLUDED.adapter_id,
                adapter_version_major = EXCLUDED.adapter_version_major,
                adapter_version_minor = EXCLUDED.adapter_version_minor,
                adapter_version_patch = EXCLUDED.adapter_version_patch,
                upstream_version      = EXCLUDED.upstream_version,
                artifact_uri          = EXCLUDED.artifact_uri,
                artifact_image_name   = EXCLUDED.artifact_image_name,
                artifact_image_tag    = EXCLUDED.artifact_image_tag,
                status                = EXCLUDED.status,
                pinned                = EXCLUDED.pinned,
                resolved_at           = EXCLUDED.resolved_at,
                updated_at            = EXCLUDED.updated_at,
                metadata_json         = EXCLUDED.metadata_json,
                schema_json           = EXCLUDED.schema_json
            "#,
        )
        .bind(&build.build_id.0)
        .bind(&build.game_id)
        .bind(&build.channel)
        .bind(&build.adapter_id.0)
        .bind(build.adapter_version.major as i32)
        .bind(build.adapter_version.minor as i32)
        .bind(build.adapter_version.patch as i32)
        .bind(&build.upstream_version)
        .bind(&build.artifact_uri)
        .bind(&build.artifact_image_name)
        .bind(&build.artifact_image_tag)
        .bind(super::sql_helpers::build_status_to_str(&build.status))
        .bind(build.pinned)
        .bind(build.resolved_at)
        .bind(build.created_at)
        .bind(build.updated_at)
        .bind(metadata_json)
        .bind(&build.schema_json)
        .execute(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to save build: {e}"),
        })?;
        Ok(())
    }

    async fn get(&self, build_id: &BuildId) -> Result<Option<GameBuild>, AssetServiceError> {
        let row = sqlx::query_as::<_, GameBuildRow>(
            r#"
            SELECT
                build_id, game_id, channel,
                adapter_id, adapter_version_major, adapter_version_minor, adapter_version_patch,
                upstream_version, artifact_uri, artifact_image_name, artifact_image_tag,
                status, pinned, resolved_at, created_at, updated_at,
                metadata_json, schema_json
            FROM t_asset_service_game_builds
            WHERE build_id = $1
            "#,
        )
        .bind(&build_id.0)
        .fetch_optional(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to get build: {e}"),
        })?;

        row.map(|r| r.try_into_domain()).transpose()
    }

    async fn list_by_game(&self, game_id: &str) -> Result<Vec<GameBuild>, AssetServiceError> {
        let rows = sqlx::query_as::<_, GameBuildRow>(
            r#"
            SELECT
                build_id, game_id, channel,
                adapter_id, adapter_version_major, adapter_version_minor, adapter_version_patch,
                upstream_version, artifact_uri, artifact_image_name, artifact_image_tag,
                status, pinned, resolved_at, created_at, updated_at,
                metadata_json, schema_json
            FROM t_asset_service_game_builds
            WHERE game_id = $1
            ORDER BY created_at DESC
            "#,
        )
        .bind(game_id)
        .fetch_all(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to list builds by game: {e}"),
        })?;

        rows.into_iter().map(|r| r.try_into_domain()).collect()
    }
}

#[derive(sqlx::FromRow)]
struct GameBuildRow {
    build_id: String,
    game_id: String,
    channel: Option<String>,
    adapter_id: String,
    adapter_version_major: i32,
    adapter_version_minor: i32,
    adapter_version_patch: i32,
    upstream_version: Option<String>,
    artifact_uri: Option<String>,
    artifact_image_name: Option<String>,
    artifact_image_tag: Option<String>,
    status: String,
    pinned: bool,
    resolved_at: chrono::DateTime<chrono::Utc>,
    created_at: chrono::DateTime<chrono::Utc>,
    updated_at: chrono::DateTime<chrono::Utc>,
    metadata_json: String,
    schema_json: Option<String>,
}

impl GameBuildRow {
    fn try_into_domain(self) -> Result<GameBuild, AssetServiceError> {
        let adapter_metadata = if self.metadata_json.is_empty() || self.metadata_json == "{}" {
            None
        } else {
            Some(serde_json::from_str(&self.metadata_json).map_err(|e| {
                AssetServiceError::Internal {
                    message: format!("deserialize adapter metadata: {e}"),
                }
            })?)
        };
        Ok(GameBuild {
            build_id: BuildId(self.build_id),
            game_id: self.game_id,
            channel: self.channel,
            adapter_id: crate::domain::AdapterId(self.adapter_id),
            adapter_version: AdapterVersion {
                major: self.adapter_version_major as u32,
                minor: self.adapter_version_minor as u32,
                patch: self.adapter_version_patch as u32,
            },
            upstream_version: self.upstream_version,
            artifact_uri: self.artifact_uri,
            artifact_image_name: self.artifact_image_name,
            artifact_image_tag: self.artifact_image_tag,
            status: super::sql_helpers::str_to_build_status(&self.status)?,
            pinned: self.pinned,
            // 适配器元数据/schema 随构建存储（无需二次查询 adapter 表）
            adapter_metadata,
            schema_json: self.schema_json,
            resolved_at: self.resolved_at,
            created_at: self.created_at,
            updated_at: self.updated_at,
        })
    }
}
