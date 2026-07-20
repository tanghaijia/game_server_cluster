use sqlx::PgPool;

/// Create a PgPool from a database URL.
pub async fn create_pool(database_url: &str) -> Result<PgPool, sqlx::Error> {
    PgPool::connect(database_url).await
}

/// Run embedded migrations from the `migrations/` directory.
pub async fn run_migrations(pool: &PgPool) -> Result<(), sqlx::migrate::MigrateError> {
    sqlx::migrate!("./migrations").run(pool).await
}

// ── BuildStatus ──────────────────────────────────────────────────────────────

pub(crate) fn build_status_to_str(s: &crate::domain::BuildStatus) -> &'static str {
    use crate::domain::BuildStatus;
    match s {
        BuildStatus::Discovered => "discovered",
        BuildStatus::Resolving => "resolving",
        BuildStatus::Available => "available",
        BuildStatus::Deprecated => "deprecated",
        BuildStatus::Unavailable => "unavailable",
        BuildStatus::Deleted => "deleted",
    }
}

pub(crate) fn str_to_build_status(s: &str) -> Result<crate::domain::BuildStatus, crate::error::AssetServiceError> {
    match s {
        "discovered" => Ok(crate::domain::BuildStatus::Discovered),
        "resolving" => Ok(crate::domain::BuildStatus::Resolving),
        "available" => Ok(crate::domain::BuildStatus::Available),
        "deprecated" => Ok(crate::domain::BuildStatus::Deprecated),
        "unavailable" => Ok(crate::domain::BuildStatus::Unavailable),
        "deleted" => Ok(crate::domain::BuildStatus::Deleted),
        other => Err(crate::error::AssetServiceError::Internal {
            message: format!("invalid build status string: {other}"),
        }),
    }
}

// ── SnapshotType ─────────────────────────────────────────────────────────────

pub(crate) fn snapshot_type_to_str(t: &crate::domain::SnapshotType) -> &'static str {
    use crate::domain::SnapshotType;
    match t {
        SnapshotType::Manual => "manual",
        SnapshotType::Scheduled => "scheduled",
        SnapshotType::PreUpgrade => "pre_upgrade",
        SnapshotType::FinalStop => "final_stop",
    }
}

pub(crate) fn str_to_snapshot_type(s: &str) -> Result<crate::domain::SnapshotType, crate::error::AssetServiceError> {
    match s {
        "manual" => Ok(crate::domain::SnapshotType::Manual),
        "scheduled" => Ok(crate::domain::SnapshotType::Scheduled),
        "pre_upgrade" => Ok(crate::domain::SnapshotType::PreUpgrade),
        "final_stop" => Ok(crate::domain::SnapshotType::FinalStop),
        other => Err(crate::error::AssetServiceError::Internal {
            message: format!("invalid snapshot type string: {other}"),
        }),
    }
}

// ── SnapshotStatus ───────────────────────────────────────────────────────────

pub(crate) fn snapshot_status_to_str(s: &crate::domain::SnapshotStatus) -> &'static str {
    use crate::domain::SnapshotStatus;
    match s {
        SnapshotStatus::Pending => "pending",
        SnapshotStatus::Running => "running",
        SnapshotStatus::Uploading => "uploading",
        SnapshotStatus::Completed => "completed",
        SnapshotStatus::Failed => "failed",
        SnapshotStatus::Expired => "expired",
    }
}

pub(crate) fn str_to_snapshot_status(s: &str) -> Result<crate::domain::SnapshotStatus, crate::error::AssetServiceError> {
    match s {
        "pending" => Ok(crate::domain::SnapshotStatus::Pending),
        "running" => Ok(crate::domain::SnapshotStatus::Running),
        "uploading" => Ok(crate::domain::SnapshotStatus::Uploading),
        "completed" => Ok(crate::domain::SnapshotStatus::Completed),
        "failed" => Ok(crate::domain::SnapshotStatus::Failed),
        "expired" => Ok(crate::domain::SnapshotStatus::Expired),
        other => Err(crate::error::AssetServiceError::Internal {
            message: format!("invalid snapshot status string: {other}"),
        }),
    }
}
