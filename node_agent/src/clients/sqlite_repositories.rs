use std::sync::Arc;

use async_trait::async_trait;
use sqlx::SqlitePool;

use crate::domain::{
    GameCache, GameContainer, GameInstance, LocalGameBuild, NodeOperation, OperationId,
    OperationKind, OperationStatus,
};
use crate::error::NodeAgentError;
use crate::ports::{
    DockerInstanceRepository, GameCacheRepository, GameInstanceRepository,
    LocalGameBuildRepository, OperationRepository,
};

// ============================================================
// 表名常量
// ============================================================
const TABLE_OPERATION: &str = "node_operation_store";
const TABLE_GAME_INSTANCE: &str = "game_instance_store";
const TABLE_DOCKER_INSTANCE: &str = "docker_instance_store";
const TABLE_GAME_CACHE: &str = "game_cache_store";
const TABLE_LOCAL_GAME_BUILD: &str = "local_game_build_store";

// ============================================================
// 建表
// ============================================================

async fn ensure_tables(pool: &SqlitePool) -> Result<(), NodeAgentError> {
    for table in [
        TABLE_OPERATION,
        TABLE_GAME_INSTANCE,
        TABLE_DOCKER_INSTANCE,
        TABLE_GAME_CACHE,
        TABLE_LOCAL_GAME_BUILD,
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

    async fn find_active(
        &self,
        kind: OperationKind,
        key: &str,
    ) -> Result<Option<NodeOperation>, NodeAgentError> {
        let rows: Vec<(String,)> = sqlx::query_as(&format!(
            "SELECT value FROM {TABLE_OPERATION}"
        ))
        .fetch_all(&*self.pool)
        .await
        .map_err(|e| NodeAgentError::DBOperationFail {
            message: format!("find active operation failed: {e}"),
        })?;

        for (json,) in rows {
            let op: NodeOperation = match serde_json::from_str(&json) {
                Ok(op) => op,
                Err(e) => {
                    log::error!("deserialize operation failed: {e}");
                    continue;
                }
            };
            // 仅匹配进行中(PENDING/RUNNING)的同类操作
            if op.kind != kind
                || op.status == OperationStatus::Succeeded
                || op.status == OperationStatus::Failed
            {
                continue;
            }
            let matches = op.instance_id.as_ref().is_some_and(|id| id.0 == key)
                || op.build_id.as_ref().is_some_and(|b| b == key);
            if matches {
                return Ok(Some(op));
            }
        }
        Ok(None)
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

    async fn insert_if_absent(&self, game_cache: &GameCache) -> anyhow::Result<bool> {
        let json = serde_json::to_string(game_cache)?;
        let key = format!("{}:{}", game_cache.game_id, game_cache.branch_name);
        let result = sqlx::query(&format!(
            "INSERT OR IGNORE INTO {TABLE_GAME_CACHE} (key, value) VALUES (?1, ?2)"
        ))
        .bind(&key)
        .bind(&json)
        .execute(&*self.pool)
        .await?;
        // rows_affected == 1 表示本次真正插入(key 原本不存在);0 表示 key 已存在
        Ok(result.rows_affected() == 1)
    }

    async fn delete(&self, game_id: &String, branch_name: &String) -> anyhow::Result<()> {
        let key = format!("{}:{}", game_id, branch_name);
        sqlx::query(&format!("DELETE FROM {TABLE_GAME_CACHE} WHERE key = ?1"))
            .bind(&key)
            .execute(&*self.pool)
            .await?;
        Ok(())
    }
}

// ============================================================
// SqliteLocalGameBuildRepository
// ============================================================

pub struct SqliteLocalGameBuildRepository {
    pool: Arc<SqlitePool>,
}

impl SqliteLocalGameBuildRepository {
    pub async fn new(pool: Arc<SqlitePool>) -> Result<Self, NodeAgentError> {
        ensure_tables(&pool).await?;
        Ok(Self { pool })
    }
}

#[async_trait]
impl LocalGameBuildRepository for SqliteLocalGameBuildRepository {
    async fn save(&self, local_game_build: &LocalGameBuild) -> Result<(), NodeAgentError> {
        let json = serde_json::to_string(local_game_build).map_err(|e| NodeAgentError::Internal {
            message: format!("serialize local_game_build failed: {e}"),
        })?;
        // 幂等：build_id 已存在则覆盖（刷新本地构建状态）
        sqlx::query(&format!(
            "INSERT OR REPLACE INTO {TABLE_LOCAL_GAME_BUILD} (key, value) VALUES (?1, ?2)"
        ))
        .bind(&local_game_build.build_id)
        .bind(&json)
        .execute(&*self.pool)
        .await
        .map_err(|e| NodeAgentError::DBOperationFail {
            message: format!("save local_game_build failed: {e}"),
        })?;
        Ok(())
    }

    async fn get(&self, build_id: String) -> Result<LocalGameBuild, NodeAgentError> {
        let row: Option<(String,)> = sqlx::query_as(&format!(
            "SELECT value FROM {TABLE_LOCAL_GAME_BUILD} WHERE key = ?1"
        ))
        .bind(&build_id)
        .fetch_optional(&*self.pool)
        .await
        .map_err(|e| NodeAgentError::DBOperationFail {
            message: format!("get local_game_build failed: {e}"),
        })?;

        match row {
            Some((json,)) => {
                let local: LocalGameBuild =
                    serde_json::from_str(&json).map_err(|e| NodeAgentError::Internal {
                        message: format!("deserialize local_game_build failed: {e}"),
                    })?;
                Ok(local)
            }
            None => Err(NodeAgentError::DBOperationFail {
                message: format!("没找到game_build, build id: {}", build_id),
            }),
        }
    }
}
