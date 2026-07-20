use async_trait::async_trait;
use sqlx::PgPool;

use crate::{
    error::AssetServiceError,
    ports::{DepotManifest, SteamBranch, SteamBranchRepository},
};

pub struct SqlSteamBranchRepository {
    pool: PgPool,
}

impl SqlSteamBranchRepository {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl SteamBranchRepository for SqlSteamBranchRepository {
    async fn save_branches(
        &self,
        game_id: &str,
        branches: &[SteamBranch],
    ) -> Result<(), AssetServiceError> {
        let mut tx = self
            .pool
            .begin()
            .await
            .map_err(|e| AssetServiceError::Internal {
                message: format!("failed to begin transaction: {e}"),
            })?;

        // Delete old branches (t_asset_service_depot_manifests cascade-deleted)
        sqlx::query("DELETE FROM t_asset_service_steam_branches WHERE game_id = $1")
            .bind(game_id)
            .execute(&mut *tx)
            .await
            .map_err(|e| AssetServiceError::Internal {
                message: format!("failed to clear steam branches: {e}"),
            })?;

        for branch in branches {
            let branch_id: i32 = sqlx::query_scalar(
                r#"
                INSERT INTO t_asset_service_steam_branches (game_id, name, build_id, description, app_id)
                VALUES ($1, $2, $3, $4, $5)
                RETURNING id
                "#,
            )
            .bind(game_id)
            .bind(&branch.name)
            .bind(branch.build_id as i64)
            .bind(&branch.description)
            .bind(&branch.app_id)
            .fetch_one(&mut *tx)
            .await
            .map_err(|e| AssetServiceError::Internal {
                message: format!("failed to insert steam branch: {e}"),
            })?;

            for manifest in &branch.manifests {
                sqlx::query(
                    "INSERT INTO t_asset_service_depot_manifests (branch_id, depot_id, manifest_gid) VALUES ($1, $2, $3)",
                )
                .bind(branch_id)
                .bind(manifest.depot_id as i32)
                .bind(manifest.manifest_gid as i64)
                .execute(&mut *tx)
                .await
                .map_err(|e| AssetServiceError::Internal {
                    message: format!("failed to insert depot manifest: {e}"),
                })?;
            }
        }

        tx.commit()
            .await
            .map_err(|e| AssetServiceError::Internal {
                message: format!("failed to commit transaction: {e}"),
            })?;

        Ok(())
    }

    async fn get_branches(
        &self,
        game_id: &str,
    ) -> Result<Vec<SteamBranch>, AssetServiceError> {
        let branch_rows = sqlx::query_as::<_, SteamBranchRow>(
            r#"
            SELECT id, game_id, name, build_id, description, app_id
            FROM t_asset_service_steam_branches
            WHERE game_id = $1
            ORDER BY name
            "#,
        )
        .bind(game_id)
        .fetch_all(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to get steam branches: {e}"),
        })?;

        let mut result = Vec::with_capacity(branch_rows.len());
        for branch_row in branch_rows {
            let depot_rows = sqlx::query_as::<_, DepotManifestRow>(
                "SELECT depot_id, manifest_gid FROM t_asset_service_depot_manifests WHERE branch_id = $1 ORDER BY id",
            )
            .bind(branch_row.id)
            .fetch_all(&self.pool)
            .await
            .map_err(|e| AssetServiceError::Internal {
                message: format!("failed to get depot manifests: {e}"),
            })?;

            result.push(branch_row.into_domain(depot_rows));
        }

        Ok(result)
    }

    async fn get_branch(
        &self,
        game_id: &str,
        branch_name: &str,
    ) -> Result<Option<SteamBranch>, AssetServiceError> {
        let branch_row = sqlx::query_as::<_, SteamBranchRow>(
            r#"
            SELECT id, game_id, name, build_id, description, app_id
            FROM t_asset_service_steam_branches
            WHERE game_id = $1 AND name = $2
            "#,
        )
        .bind(game_id)
        .bind(branch_name)
        .fetch_optional(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to get steam branch: {e}"),
        })?;

        let Some(branch_row) = branch_row else {
            return Ok(None);
        };

        let depot_rows = sqlx::query_as::<_, DepotManifestRow>(
            "SELECT depot_id, manifest_gid FROM t_asset_service_depot_manifests WHERE branch_id = $1 ORDER BY id",
        )
        .bind(branch_row.id)
        .fetch_all(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to get depot manifests: {e}"),
        })?;

        Ok(Some(branch_row.into_domain(depot_rows)))
    }
}

#[derive(sqlx::FromRow)]
struct SteamBranchRow {
    id: i32,
    game_id: String,
    name: String,
    build_id: i64,
    description: Option<String>,
    app_id: String,
}

impl SteamBranchRow {
    fn into_domain(self, depots: Vec<DepotManifestRow>) -> SteamBranch {
        SteamBranch {
            name: self.name,
            build_id: self.build_id as u64,
            description: self.description,
            app_id: self.app_id,
            manifests: depots.into_iter().map(|d| d.into_domain()).collect(),
        }
    }
}

#[derive(sqlx::FromRow)]
struct DepotManifestRow {
    depot_id: i32,
    manifest_gid: i64,
}

impl DepotManifestRow {
    fn into_domain(self) -> DepotManifest {
        DepotManifest {
            depot_id: self.depot_id as u32,
            manifest_gid: self.manifest_gid as u64,
        }
    }
}
