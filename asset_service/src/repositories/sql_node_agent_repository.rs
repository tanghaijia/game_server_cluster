use async_trait::async_trait;
use sqlx::PgPool;

use crate::{
    domain::NodeAgent,
    error::AssetServiceError,
    ports::NodeAgentRepository,
};

pub struct SqlNodeAgentRepository {
    pool: PgPool,
}

impl SqlNodeAgentRepository {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl NodeAgentRepository for SqlNodeAgentRepository {
    async fn save(&self, agent: &NodeAgent) -> Result<(), AssetServiceError> {
        sqlx::query(
            r#"
            INSERT INTO t_asset_service_node_agents (node_id, endpoint, status, last_heartbeat_at)
            VALUES ($1, $2, $3, $4)
            ON CONFLICT (node_id) DO UPDATE SET
                endpoint          = EXCLUDED.endpoint,
                status            = EXCLUDED.status,
                last_heartbeat_at = EXCLUDED.last_heartbeat_at
            "#,
        )
        .bind(&agent.node_id)
        .bind(&agent.endpoint)
        .bind(&agent.status)
        .bind(agent.last_heartbeat_at)
        .execute(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to save node agent: {e}"),
        })?;
        Ok(())
    }

    async fn get(&self, node_id: &str) -> Result<Option<NodeAgent>, AssetServiceError> {
        let row = sqlx::query_as::<_, NodeAgentRow>(
            "SELECT node_id, endpoint, status, last_heartbeat_at FROM t_asset_service_node_agents WHERE node_id = $1",
        )
        .bind(node_id)
        .fetch_optional(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to get node agent: {e}"),
        })?;

        Ok(row.map(|r| r.into_domain()))
    }

    async fn list(&self) -> Result<Vec<NodeAgent>, AssetServiceError> {
        let rows = sqlx::query_as::<_, NodeAgentRow>(
            "SELECT node_id, endpoint, status, last_heartbeat_at FROM t_asset_service_node_agents ORDER BY node_id",
        )
        .fetch_all(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to list node agents: {e}"),
        })?;

        Ok(rows.into_iter().map(|r| r.into_domain()).collect())
    }

    async fn delete(&self, node_id: &str) -> Result<(), AssetServiceError> {
        sqlx::query("DELETE FROM t_asset_service_node_agents WHERE node_id = $1")
            .bind(node_id)
            .execute(&self.pool)
            .await
            .map_err(|e| AssetServiceError::Internal {
                message: format!("failed to delete node agent: {e}"),
            })?;
        Ok(())
    }
}

#[derive(sqlx::FromRow)]
struct NodeAgentRow {
    node_id: String,
    endpoint: String,
    status: String,
    last_heartbeat_at: i64,
}

impl NodeAgentRow {
    fn into_domain(self) -> NodeAgent {
        NodeAgent {
            node_id: self.node_id,
            endpoint: self.endpoint,
            status: self.status,
            last_heartbeat_at: self.last_heartbeat_at,
        }
    }
}
