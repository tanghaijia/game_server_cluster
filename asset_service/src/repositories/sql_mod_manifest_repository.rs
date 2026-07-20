use async_trait::async_trait;
use sqlx::PgPool;

use crate::{
    domain::{ModEntry, ModManifest, ModManifestId},
    error::AssetServiceError,
    ports::ModManifestRepository,
};

pub struct SqlModManifestRepository {
    pool: PgPool,
}

impl SqlModManifestRepository {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl ModManifestRepository for SqlModManifestRepository {
    async fn save(&self, manifest: &ModManifest) -> Result<(), AssetServiceError> {
        let mut tx = self
            .pool
            .begin()
            .await
            .map_err(|e| AssetServiceError::Internal {
                message: format!("failed to begin transaction: {e}"),
            })?;

        sqlx::query(
            r#"
            INSERT INTO t_asset_service_mod_manifests (manifest_id, game_id, config_hash, compatibility_note, created_at)
            VALUES ($1, $2, $3, $4, $5)
            ON CONFLICT (manifest_id) DO UPDATE SET
                game_id            = EXCLUDED.game_id,
                config_hash        = EXCLUDED.config_hash,
                compatibility_note = EXCLUDED.compatibility_note
            "#,
        )
        .bind(&manifest.manifest_id.0)
        .bind(&manifest.game_id)
        .bind(&manifest.config_hash)
        .bind(&manifest.compatibility_note)
        .bind(manifest.created_at)
        .execute(&mut *tx)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to save mod manifest: {e}"),
        })?;

        // Replace mod entries: delete old, insert new
        sqlx::query("DELETE FROM t_asset_service_mod_entries WHERE manifest_id = $1")
            .bind(&manifest.manifest_id.0)
            .execute(&mut *tx)
            .await
            .map_err(|e| AssetServiceError::Internal {
                message: format!("failed to clear mod entries: {e}"),
            })?;

        for entry in &manifest.mods {
            sqlx::query(
                "INSERT INTO t_asset_service_mod_entries (manifest_id, mod_id, version, required) VALUES ($1, $2, $3, $4)",
            )
            .bind(&manifest.manifest_id.0)
            .bind(&entry.mod_id)
            .bind(&entry.version)
            .bind(entry.required)
            .execute(&mut *tx)
            .await
            .map_err(|e| AssetServiceError::Internal {
                message: format!("failed to insert mod entry: {e}"),
            })?;
        }

        tx.commit()
            .await
            .map_err(|e| AssetServiceError::Internal {
                message: format!("failed to commit transaction: {e}"),
            })?;

        Ok(())
    }

    async fn get(
        &self,
        manifest_id: &ModManifestId,
    ) -> Result<Option<ModManifest>, AssetServiceError> {
        let manifest_row = sqlx::query_as::<_, ModManifestRow>(
            r#"
            SELECT manifest_id, game_id, config_hash, compatibility_note, created_at
            FROM t_asset_service_mod_manifests
            WHERE manifest_id = $1
            "#,
        )
        .bind(&manifest_id.0)
        .fetch_optional(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to get mod manifest: {e}"),
        })?;

        let Some(manifest_row) = manifest_row else {
            return Ok(None);
        };

        let entries = sqlx::query_as::<_, ModEntryRow>(
            "SELECT mod_id, version, required FROM t_asset_service_mod_entries WHERE manifest_id = $1 ORDER BY id",
        )
        .bind(&manifest_id.0)
        .fetch_all(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to get mod entries: {e}"),
        })?;

        Ok(Some(manifest_row.into_domain(entries)))
    }
}

#[derive(sqlx::FromRow)]
struct ModManifestRow {
    manifest_id: String,
    game_id: String,
    config_hash: String,
    compatibility_note: Option<String>,
    created_at: chrono::DateTime<chrono::Utc>,
}

impl ModManifestRow {
    fn into_domain(self, entries: Vec<ModEntryRow>) -> ModManifest {
        ModManifest {
            manifest_id: ModManifestId(self.manifest_id),
            game_id: self.game_id,
            mods: entries.into_iter().map(|e| e.into_domain()).collect(),
            config_hash: self.config_hash,
            compatibility_note: self.compatibility_note,
            created_at: self.created_at,
        }
    }
}

#[derive(sqlx::FromRow)]
struct ModEntryRow {
    mod_id: String,
    version: String,
    required: bool,
}

impl ModEntryRow {
    fn into_domain(self) -> ModEntry {
        ModEntry {
            mod_id: self.mod_id,
            version: self.version,
            required: self.required,
        }
    }
}
