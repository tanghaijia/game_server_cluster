use std::sync::Arc;

use apalis_core::{
    backend::TaskSink, error::BoxDynError, task::data::Data, worker::builder::WorkerBuilder,
    worker::context::WorkerContext,
};
use apalis_sqlite::{SqlitePool, SqliteStorage};
use chrono::Utc;
use serde::{Deserialize, Serialize};
use tokio::sync::Mutex as AsyncMutex;

use crate::domain::BuildPreparation;
use crate::ports::GameInstanceRepository;
use crate::service::BackgroundWorker;
use crate::{
    domain::{
        InstanceId, LocalGameBuildManager, NodeOperation, OperationId, OperationKind,
        OperationStatus, SnapshotCaptureRequest, SnapshotRestoreRequest, StartInstanceArgument,
    },
    error::NodeAgentError,
    ports::{
        AssetServiceFace, ContainerClient, InstanceRuntime, OperationRepository, SnapshotRuntime,
    },
};
// ============================================================
// TaskContext — 所有 handler 共享的依赖集合
// ============================================================

/// 使用 trait object 避免泛型传播到每个 handler 签名
pub struct TaskContext {
    pub node_agent_service: Arc<dyn BackgroundWorker>,
    pub game_instance_repos: Arc<dyn GameInstanceRepository>,
    pub snapshot_runtime: Arc<dyn SnapshotRuntime>,
    pub operations: Arc<dyn OperationRepository>,
    pub asset_service: Arc<dyn AssetServiceFace>,
    pub image_client: Arc<dyn ContainerClient>,
    pub local_game_build_manager: Arc<AsyncMutex<LocalGameBuildManager>>,
}

impl TaskContext {
    pub fn new(
        node_agent_service: Arc<dyn BackgroundWorker>,
        game_instance_repos: Arc<dyn GameInstanceRepository>,
        snapshot_runtime: Arc<dyn SnapshotRuntime>,
        operations: Arc<dyn OperationRepository>,
        asset_service: Arc<dyn AssetServiceFace>,
        image_client: Arc<dyn ContainerClient>,
    ) -> Self {
        Self {
            node_agent_service,
            game_instance_repos,
            snapshot_runtime,
            operations,
            asset_service,
            image_client,
            local_game_build_manager: Arc::new(AsyncMutex::new(LocalGameBuildManager::new())),
        }
    }
}

// ============================================================
// Job 类型定义（每种操作一个 job struct，需要 Serialize + Deserialize）
// ============================================================

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PrepareBuildJob {
    pub operation_id: String,
    pub build_preparation: BuildPreparation,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StartInstanceJob {
    pub operation_id: String,
    pub spec: StartInstanceArgument,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StopInstanceJob {
    pub instance_id: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CreateSnapshotJob {
    pub instance_id: String,
    pub snapshot_id: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RestoreSnapshotJob {
    pub instance_id: String,
    pub snapshot_id: String,
    pub storage_uri: String,
    pub manifest_uri: Option<String>,
    pub checksum: Option<String>,
    pub operation: NodeOperation,
}

// ============================================================
// Operation 辅助函数
// ============================================================

async fn create_operation(
    ops: &Arc<dyn OperationRepository>,
    kind: OperationKind,
    instance_id: Option<&str>,
    build_id: Option<&str>,
    message: &str,
) -> Result<NodeOperation, NodeAgentError> {
    let op = NodeOperation {
        operation_id: OperationId::new(),
        kind,
        status: OperationStatus::Running,
        instance_id: instance_id.map(|s| InstanceId(s.to_string())),
        build_id: build_id.map(|s| s.to_string()),
        message: Some(message.to_string()),
        started_at: Utc::now(),
        finished_at: None,
    };
    ops.save(&op).await?;
    Ok(op)
}

async fn succeed_operation(ops: &Arc<dyn OperationRepository>, mut op: NodeOperation, msg: &str) {
    op.status = OperationStatus::Succeeded;
    op.finished_at = Some(Utc::now());
    op.message = Some(msg.to_string());
    let _ = ops.save(&op).await;
}

async fn fail_operation(ops: &Arc<dyn OperationRepository>, mut op: NodeOperation, err: &str) {
    op.status = OperationStatus::Failed;
    op.finished_at = Some(Utc::now());
    op.message = Some(err.to_string());
    let _ = ops.save(&op).await;
}

async fn running_operation(ops: &Arc<dyn OperationRepository>, mut op: NodeOperation) {
    op.status = OperationStatus::Running;
    let _ = ops.save(&op).await;
}

// ============================================================
// Handler 回调函数（apalis 在每个 worker 中消费 job 时的回调）
// ============================================================
//
// 签名规则：
//   第 1 个参数 = Job 类型（apalis 自动从 storage 反序列化）
//   后续参数通过 FromRequest 注入：
//     Data<T>          — WorkerBuilder::data() 传入的共享状态
//     WorkerContext    — worker 名称、运行计数等元信息
//   返回值  = Result<(), BoxDynError>

async fn handle_prepare_build(
    job: PrepareBuildJob,
    ctx: Data<Arc<TaskContext>>,
    _worker_ctx: WorkerContext,
) -> Result<(), BoxDynError> {
    let op_id = OperationId(job.operation_id);
    ctx.node_agent_service
        .prepare_game_build(job.build_preparation, &op_id)
        .await?;

    Ok(())
}

async fn handle_start_instance(
    job: StartInstanceJob,
    ctx: Data<Arc<TaskContext>>,
    _worker_ctx: WorkerContext,
) -> Result<(), BoxDynError> {
    let op_id = OperationId(job.operation_id);
    let instance_id = job.spec.instance_id.0.clone();
    let op = ctx
        .operations
        .get(&op_id)
        .await?
        .expect("can not find operation");
    running_operation(&ctx.operations, op.clone()).await;
    ctx.node_agent_service.start_instance(job.spec).await?;

    succeed_operation(
        &ctx.operations,
        op,
        format!("start instance {} success", instance_id).as_str(),
    )
    .await;
    Ok(())
}

async fn handle_stop_instance(
    job: StopInstanceJob,
    ctx: Data<Arc<TaskContext>>,
    _worker_ctx: WorkerContext,
) -> Result<(), BoxDynError> {
    let instance_id = InstanceId(job.instance_id.clone());

    let op = create_operation(
        &ctx.operations,
        OperationKind::StopInstance,
        Some(&job.instance_id),
        None,
        "stopping instance in background",
    )
    .await?;

    todo!()
}

async fn handle_create_snapshot(
    job: CreateSnapshotJob,
    ctx: Data<Arc<TaskContext>>,
    _worker_ctx: WorkerContext,
) -> Result<(), BoxDynError> {
    let instance_id = InstanceId(job.instance_id.clone());

    let op = create_operation(
        &ctx.operations,
        OperationKind::CreateSnapshot,
        Some(&job.instance_id),
        None,
        "creating snapshot in background",
    )
    .await?;

    let request = SnapshotCaptureRequest {
        instance_id,
        snapshot_id: job.snapshot_id.clone(),
    };

    match ctx.snapshot_runtime.create_snapshot(request).await {
        Ok(_snapshot) => {
            succeed_operation(&ctx.operations, op, "snapshot created").await;
        }
        Err(e) => {
            fail_operation(&ctx.operations, op, &e.to_string()).await;
            return Err(e.into());
        }
    }

    Ok(())
}

async fn handle_restore_snapshot(
    job: RestoreSnapshotJob,
    ctx: Data<Arc<TaskContext>>,
    _worker_ctx: WorkerContext,
) -> Result<(), BoxDynError> {
    let instance_id = InstanceId(job.instance_id.clone());
    let operation = job.operation;
    running_operation(&ctx.operations, operation.clone()).await;

    let request = SnapshotRestoreRequest {
        instance_id,
        snapshot_id: job.snapshot_id.clone(),
        storage_uri: job.storage_uri.clone(),
        manifest_uri: job.manifest_uri.clone(),
        checksum: job.checksum.clone(),
    };

    match ctx.node_agent_service.restore_snapshot(request).await {
        Ok(_result) => {
            succeed_operation(&ctx.operations, operation, "snapshot restored").await;
        }
        Err(e) => {
            fail_operation(&ctx.operations, operation, &e.to_string()).await;
            return Err(e.into());
        }
    }

    Ok(())
}

// ============================================================
// 启动 Worker（每个 job 类型一个独立 worker）
// ============================================================

/// 启动所有后台任务 worker。
///
/// 每个 worker 对应一种 job 类型，共享同一个 context。
/// Worker 通过 tokio::spawn 并发运行，挂到主 tokio runtime 上。
pub fn start_all_workers(
    pool: SqlitePool,
    task_ctx: Arc<TaskContext>,
) -> Vec<tokio::task::JoinHandle<()>> {
    let mut handles = Vec::new();

    // --- PrepareBuild Worker ---
    {
        let storage = SqliteStorage::new(&pool);
        let ctx = Arc::clone(&task_ctx);
        handles.push(tokio::spawn(async move {
            WorkerBuilder::new("prepare-build-worker")
                .backend(storage)
                .data(ctx)
                .build(handle_prepare_build)
                .run()
                .await
                .expect("prepare-build worker crashed");
        }));
    }

    // --- StartInstance Worker ---
    {
        let storage = SqliteStorage::new(&pool);
        let ctx = Arc::clone(&task_ctx);
        handles.push(tokio::spawn(async move {
            WorkerBuilder::new("start-instance-worker")
                .backend(storage)
                .data(ctx)
                .build(handle_start_instance)
                .run()
                .await
                .expect("start-instance worker crashed");
        }));
    }

    // --- StopInstance Worker ---
    {
        let storage = SqliteStorage::new(&pool);
        let ctx = Arc::clone(&task_ctx);
        handles.push(tokio::spawn(async move {
            WorkerBuilder::new("stop-instance-worker")
                .backend(storage)
                .data(ctx)
                .build(handle_stop_instance)
                .run()
                .await
                .expect("stop-instance worker crashed");
        }));
    }

    // --- CreateSnapshot Worker ---
    {
        let storage = SqliteStorage::new(&pool);
        let ctx = Arc::clone(&task_ctx);
        handles.push(tokio::spawn(async move {
            WorkerBuilder::new("create-snapshot-worker")
                .backend(storage)
                .data(ctx)
                .build(handle_create_snapshot)
                .run()
                .await
                .expect("create-snapshot worker crashed");
        }));
    }

    // --- RestoreSnapshot Worker ---
    {
        let storage = SqliteStorage::new(&pool);
        let ctx = Arc::clone(&task_ctx);
        handles.push(tokio::spawn(async move {
            WorkerBuilder::new("restore-snapshot-worker")
                .backend(storage)
                .data(ctx)
                .build(handle_restore_snapshot)
                .run()
                .await
                .expect("restore-snapshot worker crashed");
        }));
    }

    handles
}

// ============================================================
// 初始化 backend（建表）
// ============================================================

pub async fn init_backend() -> Result<SqlitePool, sqlx::Error> {
    let db_url = if cfg!(debug_assertions) {
        "file:dev_mem_db?mode=memory&cache=shared"
    } else {
        "sqlite://jobs.db?mode=rwc&busy_timeout=5000"
    };

    let pool = SqlitePool::connect(db_url).await?;

    SqliteStorage::<(), (), ()>::setup(&pool)
        .await
        .expect("Apalis 建表失败！");

    Ok(pool)
}

// ============================================================
// 投递任务的辅助函数（在 gRPC handler 中调用，把操作丢到队列）
// ============================================================

pub async fn enqueue_prepare_build(
    pool: &SqlitePool,
    ops: &Arc<dyn OperationRepository>,
    prep: BuildPreparation,
) -> NodeOperation {
    let op = NodeOperation {
        operation_id: OperationId::new(),
        kind: OperationKind::PrepareBuild,
        status: OperationStatus::Pending,
        instance_id: None,
        build_id: Some(prep.build.build_id.clone()),
        message: Some("build preparation queued".to_string()),
        started_at: Utc::now(),
        finished_at: None,
    };
    let _ = ops.save(&op).await;

    let job_op_id = op.operation_id.0.clone();
    let mut storage = SqliteStorage::new(pool);
    let _ = storage
        .push(PrepareBuildJob {
            operation_id: job_op_id,
            build_preparation: prep,
        })
        .await;

    op
}

pub async fn enqueue_start_instance(
    pool: &SqlitePool,
    ops: &Arc<dyn OperationRepository>,
    argument: StartInstanceArgument,
) -> NodeOperation {
    let op = NodeOperation {
        operation_id: OperationId::new(),
        kind: OperationKind::StartInstance,
        status: OperationStatus::Pending,
        instance_id: Some(argument.instance_id.clone()),
        build_id: Some(argument.build.build_id.clone()),
        message: Some("instance start queued".to_string()),
        started_at: Utc::now(),
        finished_at: None,
    };
    let _ = ops.save(&op).await;

    let job_op_id = op.operation_id.0.clone();
    let mut storage = SqliteStorage::new(pool);
    let _ = storage
        .push(StartInstanceJob {
            operation_id: job_op_id,
            spec: argument,
        })
        .await;

    op
}

pub async fn enqueue_stop_instance(pool: &SqlitePool, instance_id: &str) {
    let mut storage = SqliteStorage::new(pool);
    let _ = storage
        .push(StopInstanceJob {
            instance_id: instance_id.to_string(),
        })
        .await;
}

pub async fn enqueue_create_snapshot(pool: &SqlitePool, instance_id: &str, snapshot_id: &str) {
    let mut storage = SqliteStorage::new(pool);
    let _ = storage
        .push(CreateSnapshotJob {
            instance_id: instance_id.to_string(),
            snapshot_id: snapshot_id.to_string(),
        })
        .await;
}

pub async fn enqueue_restore_snapshot(
    pool: &SqlitePool,
    instance_id: &str,
    snapshot_id: &str,
    storage_uri: &str,
    manifest_uri: Option<&str>,
    checksum: Option<&str>,
    operation: NodeOperation,
) {
    let mut storage = SqliteStorage::new(pool);
    let _ = storage
        .push(RestoreSnapshotJob {
            instance_id: instance_id.to_string(),
            snapshot_id: snapshot_id.to_string(),
            storage_uri: storage_uri.to_string(),
            manifest_uri: manifest_uri.map(|s| s.to_string()),
            checksum: checksum.map(|s| s.to_string()),
            operation,
        })
        .await;
}
