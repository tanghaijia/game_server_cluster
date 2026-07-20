use async_trait::async_trait;
use sqlx::PgPool;

use crate::{
    domain::Node,
    error::AssetServiceError,
    ports::NodeRepository,
};

pub struct SqlNodeRepository {
    pool: PgPool,
}

impl SqlNodeRepository {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl NodeRepository for SqlNodeRepository {
    async fn save(&self, node: &Node) -> Result<(), AssetServiceError> {
        sqlx::query(
            r#"
            INSERT INTO t_asset_service_nodes (id, ip, core_num, core_frequency, memory_size, storage_size, location, service_provider, status)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
            ON CONFLICT (id) DO UPDATE SET
                ip                = EXCLUDED.ip,
                core_num          = EXCLUDED.core_num,
                core_frequency    = EXCLUDED.core_frequency,
                memory_size       = EXCLUDED.memory_size,
                storage_size      = EXCLUDED.storage_size,
                location          = EXCLUDED.location,
                service_provider  = EXCLUDED.service_provider,
                status            = EXCLUDED.status
            "#,
        )
        .bind(&node.id)
        .bind(&node.ip)
        .bind(node.core_num)
        .bind(node.core_frequency)
        .bind(node.memory_size)
        .bind(node.storage_size)
        .bind(&node.location)
        .bind(&node.service_provider)
        .bind(&node.status)
        .execute(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to save node: {e}"),
        })?;
        Ok(())
    }

    async fn get(&self, node_id: &str) -> Result<Option<Node>, AssetServiceError> {
        let row = sqlx::query_as::<_, NodeRow>(
            r#"
            SELECT id, ip, core_num, core_frequency, memory_size, storage_size, location, service_provider, status
            FROM t_asset_service_nodes WHERE id = $1
            "#,
        )
        .bind(node_id)
        .fetch_optional(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to get node: {e}"),
        })?;

        Ok(row.map(|r| r.into_domain()))
    }

    async fn list(&self) -> Result<Vec<Node>, AssetServiceError> {
        let rows = sqlx::query_as::<_, NodeRow>(
            r#"
            SELECT id, ip, core_num, core_frequency, memory_size, storage_size, location, service_provider, status
            FROM t_asset_service_nodes ORDER BY id
            "#,
        )
        .fetch_all(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to list t_asset_service_nodes: {e}"),
        })?;

        Ok(rows.into_iter().map(|r| r.into_domain()).collect())
    }

    async fn delete(&self, node_id: &str) -> Result<(), AssetServiceError> {
        sqlx::query("DELETE FROM t_asset_service_nodes WHERE id = $1")
            .bind(node_id)
            .execute(&self.pool)
            .await
            .map_err(|e| AssetServiceError::Internal {
                message: format!("failed to delete node: {e}"),
            })?;
        Ok(())
    }
}

#[derive(sqlx::FromRow)]
struct NodeRow {
    id: String,
    ip: String,
    core_num: i32,
    core_frequency: f64,
    memory_size: i64,
    storage_size: i64,
    location: String,
    service_provider: String,
    status: String,
}

impl NodeRow {
    fn into_domain(self) -> Node {
        Node {
            id: self.id,
            ip: self.ip,
            core_num: self.core_num,
            core_frequency: self.core_frequency,
            memory_size: self.memory_size,
            storage_size: self.storage_size,
            location: self.location,
            service_provider: self.service_provider,
            status: self.status,
        }
    }
}
