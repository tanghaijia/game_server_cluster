use std::sync::Arc;

use async_trait::async_trait;
use sqlx::SqlitePool;

use crate::domain::{GameCache, GameContainer, GameInstance, NodeOperation, OperationId};
use crate::error::NodeAgentError;
use crate::ports::{
    DockerInstanceRepository, GameCacheRepository, GameInstanceRepository, OperationRepository,
};

// ============================================================
// 表名常量
// ============================================================
const TABLE_OPERATION: &str = "node_operation_store";
const TABLE_GAME_INSTANCE: &str = "game_instance_store";
const TABLE_DOCKER_INSTANCE: &str = "docker_instance_store";
const TABLE_GAME_CACHE: &str = "game_cache_store";

// ============================================================
// 建表
// ============================================================

async fn ensure_tables(pool: &SqlitePool) -> Result<(), NodeAgentError> {
    for table in [
        TABLE_OPERATION,
        TABLE_GAME_INSTANCE,
        TABLE_DOCKER_INSTANCE,
        TABLE_GAME_CACHE,
    ] {
        sqlx::query(&format!(
            "CREATE TABLE IF NOT EXISTS {table} (
                key   TEXT PRIMARY KEY,
                value TEXT NOT NULL
            )"
        ))
        .execute(pool)
        .await
        .map_err(|e| NodeAgentError::DBOperationFail {
            message: format!("failed to create table {table}: {e}"),
        })?;
    }
    Ok(())
}

// ============================================================
// SqliteOperationRepository
// ============================================================

pub struct SqliteOperationRepository {
    pool: Arc<SqlitePool>,
}

impl SqliteOperationRepository {
    pub async fn new(pool: Arc<SqlitePool>) -> Result<Self, NodeAgentError> {
        ensure_tables(&pool).await?;
        Ok(Self { pool })
    }
}

#[async_trait]
impl OperationRepository for SqliteOperationRepository {
    async fn save(&self, operation: &NodeOperation) -> Result<(), NodeAgentError> {
        let json = serde_json::to_string(operation).map_err(|e| NodeAgentError::Internal {
            message: format!("serialize operation failed: {e}"),
        })?;
        sqlx::query(&format!(
            "INSERT OR REPLACE INTO {TABLE_OPERATION} (key, value) VALUES (?1, ?2)"
        ))
        .bind(&operation.operation_id.0)
        .bind(&json)
        .execute(&*self.pool)
        .await
        .map_err(|e| NodeAgentError::DBOperationFail {
            message: format!("save operation failed: {e}"),
        })?;
        Ok(())
    }

    async fn get(
        &self,
        operation_id: &OperationId,
    ) -> Result<Option<NodeOperation>, NodeAgentError> {
        let row: Option<(String,)> = sqlx::query_as(&format!(
            "SELECT value FROM {TABLE_OPERATION} WHERE key = ?1"
        ))
        .bind(&operation_id.0)
        .fetch_optional(&*self.pool)
        .await
        .map_err(|e| NodeAgentError::DBOperationFail {
            message: format!("get operation failed: {e}"),
        })?;

        match row {
            Some((json,)) => {
                let op: NodeOperation =
                    serde_json::from_str(&json).map_err(|e| NodeAgentError::Internal {
                        message: format!("deserialize operation failed: {e}"),
                    })?;
                Ok(Some(op))
            }
            None => Ok(None),
        }
    }
}

// ============================================================
// SqliteGameInstanceRepository
// ============================================================

pub struct SqliteGameInstanceRepository {
    pool: Arc<SqlitePool>,
}

impl SqliteGameInstanceRepository {
    pub async fn new(pool: Arc<SqlitePool>) -> Result<Self, NodeAgentError> {
        ensure_tables(&pool).await?;
        Ok(Self { pool })
    }
}

#[async_trait]
impl GameInstanceRepository for SqliteGameInstanceRepository {
    async fn save(&self, game_instance: &GameInstance) -> Result<(), NodeAgentError> {
        let json = serde_json::to_string(game_instance).map_err(|e| NodeAgentError::Internal {
            message: format!("serialize game_instance failed: {e}"),
        })?;
        sqlx::query(&format!(
            "INSERT OR REPLACE INTO {TABLE_GAME_INSTANCE} (key, value) VALUES (?1, ?2)"
        ))
        .bind(&game_instance.id)
        .bind(&json)
        .execute(&*self.pool)
        .await
        .map_err(|e| NodeAgentError::DBOperationFail {
            message: format!("save game_instance failed: {e}"),
        })?;
        Ok(())
    }

    async fn get(&self, game_instance_id: String) -> Result<GameInstance, NodeAgentError> {
        let row: Option<(String,)> = sqlx::query_as(&format!(
            "SELECT value FROM {TABLE_GAME_INSTANCE} WHERE key = ?1"
        ))
        .bind(&game_instance_id)
        .fetch_optional(&*self.pool)
        .await
        .map_err(|e| NodeAgentError::DBOperationFail {
            message: format!("get game_instance failed: {e}"),
        })?;

        match row {
            Some((json,)) => {
                let instance: GameInstance =
                    serde_json::from_str(&json).map_err(|e| NodeAgentError::Internal {
                        message: format!("deserialize game_instance failed: {e}"),
                    })?;
                Ok(instance)
            }
            None => Err(NodeAgentError::InstanceNotFound {
                instance_id: game_instance_id,
            }),
        }
    }

    async fn get_all(&self) -> Result<Vec<GameInstance>, NodeAgentError> {
        let rows: Vec<(String,)> =
            sqlx::query_as(&format!("SELECT value FROM {TABLE_GAME_INSTANCE}"))
                .fetch_all(&*self.pool)
                .await
                .map_err(|e| NodeAgentError::DBOperationFail {
                    message: format!("get_all game_instances failed: {e}"),
                })?;

        let instances: Result<Vec<_>, _> = rows
            .into_iter()
            .map(|(json,)| {
                serde_json::from_str::<GameInstance>(&json).map_err(|e| NodeAgentError::Internal {
                    message: format!("deserialize game_instance failed: {e}"),
                })
            })
            .collect();
        instances
    }
}

// ============================================================
// SqliteDockerInstanceRepository
// ============================================================

pub struct SqliteDockerInstanceRepository {
    pool: Arc<SqlitePool>,
}

impl SqliteDockerInstanceRepository {
    pub async fn new(pool: Arc<SqlitePool>) -> Result<Self, NodeAgentError> {
        ensure_tables(&pool).await?;
        Ok(Self { pool })
    }
}

#[async_trait]
impl DockerInstanceRepository for SqliteDockerInstanceRepository {
    async fn save(&self, container: &GameContainer) -> Result<(), NodeAgentError> {
        let json = serde_json::to_string(container).map_err(|e| NodeAgentError::Internal {
            message: format!("serialize container failed: {e}"),
        })?;
        sqlx::query(&format!(
            "INSERT OR REPLACE INTO {TABLE_DOCKER_INSTANCE} (key, value) VALUES (?1, ?2)"
        ))
        .bind(&container.id)
        .bind(&json)
        .execute(&*self.pool)
        .await
        .map_err(|e| NodeAgentError::DBOperationFail {
            message: format!("save container failed: {e}"),
        })?;
        Ok(())
    }

    async fn get(&self, container_id: &str) -> Result<Option<GameContainer>, NodeAgentError> {
        let row: Option<(String,)> = sqlx::query_as(&format!(
            "SELECT value FROM {TABLE_DOCKER_INSTANCE} WHERE key = ?1"
        ))
        .bind(container_id)
        .fetch_optional(&*self.pool)
        .await
        .map_err(|e| NodeAgentError::DBOperationFail {
            message: format!("get container failed: {e}"),
        })?;

        match row {
            Some((json,)) => {
                let container: GameContainer =
                    serde_json::from_str(&json).map_err(|e| NodeAgentError::Internal {
                        message: format!("deserialize container failed: {e}"),
                    })?;
                Ok(Some(container))
            }
            None => Ok(None),
        }
    }

    async fn delete(&self, container_id: &str) -> Result<(), NodeAgentError> {
        sqlx::query(&format!(
            "DELETE FROM {TABLE_DOCKER_INSTANCE} WHERE key = ?1"
        ))
        .bind(container_id)
        .execute(&*self.pool)
        .await
        .map_err(|e| NodeAgentError::DBOperationFail {
            message: format!("delete container failed: {e}"),
        })?;
        Ok(())
    }
}

// ============================================================
// SqliteGameCacheRepository
// ============================================================

pub struct SqliteGameCacheRepository {
    pool: Arc<SqlitePool>,
}

impl SqliteGameCacheRepository {
    pub async fn new(pool: Arc<SqlitePool>) -> Result<Self, NodeAgentError> {
        ensure_tables(&pool).await?;
        Ok(Self { pool })
    }
}

#[async_trait]
impl GameCacheRepository for SqliteGameCacheRepository {
    async fn save(&self, game_cache: &GameCache) -> anyhow::Result<()> {
        let json = serde_json::to_string(game_cache)?;
        let key = format!("{}:{}", game_cache.game_id, game_cache.branch_name);
        sqlx::query(&format!(
            "INSERT OR REPLACE INTO {TABLE_GAME_CACHE} (key, value) VALUES (?1, ?2)"
        ))
        .bind(&key)
        .bind(&json)
        .execute(&*self.pool)
        .await?;
        Ok(())
    }

    async fn get(
        &self,
        game_id: &String,
        branch_name: &String,
    ) -> anyhow::Result<Option<GameCache>> {
        let key = format!("{}:{}", game_id, branch_name);
        let row: Option<(String,)> = sqlx::query_as(&format!(
            "SELECT value FROM {TABLE_GAME_CACHE} WHERE key = ?1"
        ))
        .bind(&key)
        .fetch_optional(&*self.pool)
        .await?;

        match row {
            Some((json,)) => {
                let cache: GameCache = serde_json::from_str(&json)?;
                Ok(Some(cache))
            }
            None => Ok(None),
        }
    }

    async fn get_all(&self) -> anyhow::Result<Vec<GameCache>> {
        let rows: Vec<(String,)> = sqlx::query_as(&format!(
            "SELECT value FROM {TABLE_GAME_CACHE}"
        ))
        .fetch_all(&*self.pool)
        .await?;

        rows.iter()
            .map(|(json,)| serde_json::from_str::<GameCache>(json).map_err(Into::into))
            .collect()
    }
}
