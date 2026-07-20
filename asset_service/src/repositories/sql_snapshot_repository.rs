use async_trait::async_trait;
use sqlx::PgPool;

use crate::{
    domain::{BuildId, SnapshotId, SnapshotRecord},
    error::AssetServiceError,
    ports::SnapshotRepository,
};

pub struct SqlSnapshotRepository {
    pool: PgPool,
}

impl SqlSnapshotRepository {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[async_trait]
impl SnapshotRepository for SqlSnapshotRepository {
    async fn save(&self, snapshot: &SnapshotRecord) -> Result<(), AssetServiceError> {
        sqlx::query(
            r#"
            INSERT INTO t_asset_service_snapshot_records (
                snapshot_id, instance_id, build_id, snapshot_type,
                instance_data_path, storage_uri, manifest_uri, checksum,
                status, source_node, created_at, completed_at, failure_message,
                bucket, key, host, host_port
            ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
            ON CONFLICT (snapshot_id) DO UPDATE SET
                instance_id         = EXCLUDED.instance_id,
                build_id            = EXCLUDED.build_id,
                snapshot_type       = EXCLUDED.snapshot_type,
                instance_data_path  = EXCLUDED.instance_data_path,
                storage_uri         = EXCLUDED.storage_uri,
                manifest_uri        = EXCLUDED.manifest_uri,
                checksum            = EXCLUDED.checksum,
                status              = EXCLUDED.status,
                source_node         = EXCLUDED.source_node,
                completed_at        = EXCLUDED.completed_at,
                failure_message     = EXCLUDED.failure_message,
                bucket              = EXCLUDED.bucket,
                key                 = EXCLUDED.key,
                host                = EXCLUDED.host,
                host_port           = EXCLUDED.host_port
            "#,
        )
        .bind(&snapshot.snapshot_id.0)
        .bind(&snapshot.instance_id)
        .bind(&snapshot.build_id.as_ref().map(|b| b.0.clone()))
        .bind(super::sql_helpers::snapshot_type_to_str(&snapshot.snapshot_type))
        .bind(&snapshot.instance_data_path)
        .bind(&snapshot.storage_uri)
        .bind(&snapshot.manifest_uri)
        .bind(&snapshot.checksum)
        .bind(super::sql_helpers::snapshot_status_to_str(&snapshot.status))
        .bind(&snapshot.source_node)
        .bind(snapshot.created_at)
        .bind(snapshot.completed_at)
        .bind(&snapshot.failure_message)
        .bind(&snapshot.bucket)
        .bind(&snapshot.key)
        .bind(&snapshot.host)
        .bind(snapshot.host_port)
        .execute(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to save snapshot: {e}"),
        })?;
        Ok(())
    }

    async fn get(
        &self,
        snapshot_id: &SnapshotId,
    ) -> Result<Option<SnapshotRecord>, AssetServiceError> {
        let row = sqlx::query_as::<_, SnapshotRow>(
            r#"
            SELECT
                snapshot_id, instance_id, build_id, snapshot_type,
                instance_data_path, storage_uri, manifest_uri, checksum,
                status, source_node, created_at, completed_at, failure_message,
                bucket, key, host, host_port
            FROM t_asset_service_snapshot_records
            WHERE snapshot_id = $1
            "#,
        )
        .bind(&snapshot_id.0)
        .fetch_optional(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to get snapshot: {e}"),
        })?;

        row.map(|r| r.try_into_domain()).transpose()
    }

    async fn list_by_instance(
        &self,
        instance_id: &str,
    ) -> Result<Vec<SnapshotRecord>, AssetServiceError> {
        let rows = sqlx::query_as::<_, SnapshotRow>(
            r#"
            SELECT
                snapshot_id, instance_id, build_id, snapshot_type,
                instance_data_path, storage_uri, manifest_uri, checksum,
                status, source_node, created_at, completed_at, failure_message,
                bucket, key, host, host_port
            FROM t_asset_service_snapshot_records
            WHERE instance_id = $1
            ORDER BY created_at DESC
            "#,
        )
        .bind(instance_id)
        .fetch_all(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to list snapshots by instance: {e}"),
        })?;

        rows.into_iter().map(|r| r.try_into_domain()).collect()
    }

    async fn set_latest(
        &self,
        instance_id: &str,
        snapshot_id: &SnapshotId,
    ) -> Result<(), AssetServiceError> {
        // Verify snapshot exists
        let exists: bool = sqlx::query_scalar(
            "SELECT EXISTS(SELECT 1 FROM t_asset_service_snapshot_records WHERE snapshot_id = $1)",
        )
        .bind(&snapshot_id.0)
        .fetch_one(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to check snapshot existence: {e}"),
        })?;

        if !exists {
            return Err(AssetServiceError::SnapshotNotFound {
                snapshot_id: snapshot_id.0.clone(),
            });
        }

        sqlx::query(
            r#"
            INSERT INTO t_asset_service_snapshot_latest (instance_id, snapshot_id)
            VALUES ($1, $2)
            ON CONFLICT (instance_id) DO UPDATE SET snapshot_id = EXCLUDED.snapshot_id
            "#,
        )
        .bind(instance_id)
        .bind(&snapshot_id.0)
        .execute(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to set latest snapshot: {e}"),
        })?;

        Ok(())
    }

    async fn get_latest(
        &self,
        instance_id: &str,
    ) -> Result<Option<SnapshotRecord>, AssetServiceError> {
        let row = sqlx::query_as::<_, SnapshotRow>(
            r#"
            SELECT
                sr.snapshot_id, sr.instance_id, sr.build_id, sr.snapshot_type,
                sr.instance_data_path, sr.storage_uri, sr.manifest_uri, sr.checksum,
                sr.status, sr.source_node, sr.created_at, sr.completed_at, sr.failure_message,
                sr.bucket, sr.key, sr.host, sr.host_port
            FROM t_asset_service_snapshot_records sr
            INNER JOIN t_asset_service_snapshot_latest sl ON sl.snapshot_id = sr.snapshot_id
            WHERE sl.instance_id = $1
            "#,
        )
        .bind(instance_id)
        .fetch_optional(&self.pool)
        .await
        .map_err(|e| AssetServiceError::Internal {
            message: format!("failed to get latest snapshot: {e}"),
        })?;

        row.map(|r| r.try_into_domain()).transpose()
    }
}

#[derive(sqlx::FromRow)]
struct SnapshotRow {
    snapshot_id: String,
    instance_id: String,
    build_id: Option<String>,
    snapshot_type: String,
    instance_data_path: String,
    storage_uri: Option<String>,
    manifest_uri: Option<String>,
    checksum: Option<String>,
    status: String,
    source_node: Option<String>,
    created_at: chrono::DateTime<chrono::Utc>,
    completed_at: Option<chrono::DateTime<chrono::Utc>>,
    failure_message: Option<String>,
    bucket: String,
    key: String,
    host: String,
    host_port: i32,
}

impl SnapshotRow {
    fn try_into_domain(self) -> Result<SnapshotRecord, AssetServiceError> {
        Ok(SnapshotRecord {
            snapshot_id: SnapshotId(self.snapshot_id),
            instance_id: self.instance_id,
            build_id: self.build_id.map(BuildId),
            snapshot_type: super::sql_helpers::str_to_snapshot_type(&self.snapshot_type)?,
            instance_data_path: self.instance_data_path,
            storage_uri: self.storage_uri,
            manifest_uri: self.manifest_uri,
            checksum: self.checksum,
            status: super::sql_helpers::str_to_snapshot_status(&self.status)?,
            source_node: self.source_node,
            created_at: self.created_at,
            completed_at: self.completed_at,
            failure_message: self.failure_message,
            bucket: self.bucket,
            key: self.key,
            host: self.host,
            host_port: self.host_port,
        })
    }
}
