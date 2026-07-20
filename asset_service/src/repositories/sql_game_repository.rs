use async_trait::async_trait;
use sqlx::PgPool;

use crate::{
    domain::Game,
    error::AssetServiceError,
    ports::GameRepository,
};

pub struct SqlGameRepository {
    pool: PgPool,
}

impl SqlGameRepository {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl GameRepository for SqlGameRepository {
    async fn save(&self, game: &Game) -> Result<(), AssetServiceError> {
        sqlx::query(
            r#"
            INSERT INTO t_asset_service_games (id, name, app_id)
            VALUES ($1, $2, $3)
            ON CONFLICT (id) DO UPDATE SET
                name    = EXCLUDED.name,
                app_id  = EXCLUDED.app_id
            "#,
        )
        .bind(&game.id)
        .bind(&game.name)
        .bind(&game.app_id)
        .execute(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to save game: {e}"),
        })?;
        Ok(())
    }

    async fn get(&self, game_id: &str) -> Result<Option<Game>, AssetServiceError> {
        let row = sqlx::query_as::<_, GameRow>(
            "SELECT id, name, app_id FROM t_asset_service_games WHERE id = $1",
        )
        .bind(game_id)
        .fetch_optional(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to get game: {e}"),
        })?;

        Ok(row.map(|r| r.into_domain()))
    }

    async fn list(&self) -> Result<Vec<Game>, AssetServiceError> {
        let rows = sqlx::query_as::<_, GameRow>("SELECT id, name, app_id FROM t_asset_service_games ORDER BY id")
            .fetch_all(&self.pool)
            .await
            .map_err(|e| AssetServiceError::Internal {
                message: format!("failed to list t_asset_service_games: {e}"),
            })?;

        Ok(rows.into_iter().map(|r| r.into_domain()).collect())
    }

    async fn delete(&self, game_id: &str) -> Result<(), AssetServiceError> {
        sqlx::query("DELETE FROM t_asset_service_games WHERE id = $1")
            .bind(game_id)
            .execute(&self.pool)
            .await
            .map_err(|e| AssetServiceError::Internal {
                message: format!("failed to delete game: {e}"),
            })?;
        Ok(())
    }
}

#[derive(sqlx::FromRow)]
struct GameRow {
    id: String,
    name: String,
    app_id: String,
}

impl GameRow {
    fn into_domain(self) -> Game {
        Game {
            id: self.id,
            name: self.name,
            app_id: self.app_id,
        }
    }
}
