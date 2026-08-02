use std::sync::Arc;

use apalis_core::{
    backend::TaskSink, error::BoxDynError, task::data::Data, worker::builder::WorkerBuilder,
    worker::context::WorkerContext,
};
use apalis_sqlite::{SqlitePool, SqliteStorage};
use chrono::Utc;
use log::error;
use serde::{Deserialize, Serialize};

use crate::domain::{BuildPreparation, GameInstance, GameInstanceStatus};
use crate::ports::GameInstanceRepository;
use crate::service::BackgroundWorker;
use crate::{
    domain::{
        InstanceId, NodeOperation, OperationId, OperationKind, OperationStatus,
        SnapshotCaptureRequest, SnapshotRestoreRequest, StartInstanceArgument,
    },
    error::NodeAgentError,
    ports::{AssetServiceFace, ContainerClient, OperationRepository, Snapshot_manager},
};
// ============================================================
// TaskContext — 所有 handler 共享的依赖集合
// ============================================================

/// 使用 trait object 避免泛型传播到每个 handler 签名
pub struct TaskContext {
    pub node_agent_service: Arc<dyn BackgroundWorker>,
    pub game_instance_repos: Arc<dyn GameInstanceRepository>,
    pub operations: Arc<dyn OperationRepository>,
    pub asset_service: Arc<dyn AssetServiceFace>,
    pub image_client: Arc<dyn ContainerClient>,
}

impl TaskContext {
    pub fn new(
        node_agent_service: Arc<dyn BackgroundWorker>,
        game_instance_repos: Arc<dyn GameInstanceRepository>,
        operations: Arc<dyn OperationRepository>,
        asset_service: Arc<dyn AssetServiceFace>,
        image_client: Arc<dyn ContainerClient>,
    ) -> Self {
        Self {
            node_agent_service,
            game_instance_repos,
            operations,
            asset_service,
            image_client,
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
    pub operation_id: String,
    pub instance_id: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RestartInstanceJob {
    pub operation_id: String,
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
    pub operation: NodeOperation,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CleanInstanceJob {
    pub operation_id: String,
    pub instance_id: String,
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
    let op = ctx
        .operations
        .get(&op_id)
        .await?
        .expect("can not find operation");
    running_operation(&ctx.operations, op.clone()).await;
    if let Err(err) = ctx
        .node_agent_service
        .prepare_game_build(job.build_preparation, &op_id)
        .await
    {
        error!(
            "prepare game build service fail, operation id: {}, error: {}.",
            op_id.0, err
        );
        fail_operation(&ctx.operations, op, &err.to_string()).await;
    } else {
        succeed_operation(
            &ctx.operations,
            op,
            format!("prepare game build success, operation id: {}.", op_id.0).as_str(),
        )
        .await;
    }

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
    if let Err(err) = ctx.node_agent_service.start_instance(job.spec).await {
        error!("start game instance fail, operation id: {}, error: {}.", op_id.0, err);
        fail_operation(&ctx.operations, op.clone(), &err.to_string()).await;

        // 将 GameInstance 状态标记为 Failed
        let Ok(mut game_instance) = ctx.game_instance_repos.get(instance_id.clone()).await else {
            return Ok(());
        };
        game_instance.status = GameInstanceStatus::Failed;
        if let Err(save_err) = ctx.game_instance_repos.save(&game_instance).await {
            error!(
                "failed to update instance {} status to Failed after start failure: {}",
                instance_id, save_err
            );
        }
    } else {
        succeed_operation(
            &ctx.operations,
            op,
            format!("start instance {} success", instance_id).as_str(),
        )
        .await;
    }

    Ok(())
}

async fn handle_stop_instance(
    job: StopInstanceJob,
    ctx: Data<Arc<TaskContext>>,
    _worker_ctx: WorkerContext,
) -> Result<(), BoxDynError> {
    let op_id = OperationId(job.operation_id);
    let instance_id = InstanceId(job.instance_id.clone());

    let op = ctx
        .operations
        .get(&op_id)
        .await?
        .expect("can not find operation");
    running_operation(&ctx.operations, op.clone()).await;
    match ctx
        .node_agent_service
        .stop_instance(instance_id.clone())
        .await
    {
        Ok(()) => {
            succeed_operation(
                &ctx.operations,
                op,
                format!(
                    "stop instance {} success, operation id: {}",
                    instance_id.0, op_id.0
                )
                .as_str(),
            )
            .await;
        }
        Err(err) => {
            fail_operation(&ctx.operations, op.clone(), &err.to_string()).await;
        }
    };

    Ok(())
}

async fn handle_restart_instance(
    job: RestartInstanceJob,
    ctx: Data<Arc<TaskContext>>,
    _worker_ctx: WorkerContext,
) -> Result<(), BoxDynError> {
    let op_id = OperationId(job.operation_id);
    let instance_id = InstanceId(job.instance_id.clone());

    let op = ctx
        .operations
        .get(&op_id)
        .await?
        .expect("can not find operation");
    running_operation(&ctx.operations, op.clone()).await;
    match ctx
        .node_agent_service
        .restart_instance(instance_id.clone())
        .await
    {
        Ok(_runtime) => {
            succeed_operation(
                &ctx.operations,
                op,
                format!(
                    "restart instance {} success, operation id: {}",
                    instance_id.0, op_id.0
                )
                .as_str(),
            )
            .await;
        }
        Err(err) => {
            fail_operation(&ctx.operations, op.clone(), &err.to_string()).await;
        }
    };

    Ok(())
}

async fn handle_create_snapshot(
    job: CreateSnapshotJob,
    ctx: Data<Arc<TaskContext>>,
    _worker_ctx: WorkerContext,
) -> Result<(), BoxDynError> {
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

async fn handle_clean_instance(
    job: CleanInstanceJob,
    ctx: Data<Arc<TaskContext>>,
    _worker_ctx: WorkerContext,
) -> Result<(), BoxDynError> {
    let op_id = OperationId(job.operation_id);
    let instance_id = InstanceId(job.instance_id.clone());

    let op = ctx
        .operations
        .get(&op_id)
        .await?
        .expect("can not find operation");
    running_operation(&ctx.operations, op.clone()).await;
    match ctx
        .node_agent_service
        .clean_instance(instance_id)
        .await
    {
        Ok(_) => {
            succeed_operation(&ctx.operations, op, "clean instance finish").await;
        }
        Err(e) => {
            fail_operation(&ctx.operations, op, &e.to_string()).await;
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

    // --- RestartInstance Worker ---
    {
        let storage = SqliteStorage::new(&pool);
        let ctx = Arc::clone(&task_ctx);
        handles.push(tokio::spawn(async move {
            WorkerBuilder::new("restart-instance-worker")
                .backend(storage)
                .data(ctx)
                .build(handle_restart_instance)
                .run()
                .await
                .expect("restart-instance worker crashed");
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

    // --- CleanInstance Worker ---
    {
        let storage = SqliteStorage::new(&pool);
        let ctx = Arc::clone(&task_ctx);
        handles.push(tokio::spawn(async move {
            WorkerBuilder::new("clean-instance-worker")
                .backend(storage)
                .data(ctx)
                .build(handle_clean_instance)
                .run()
                .await
                .expect("clean-instance worker crashed");
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
        "sqlite://jobs.db?mode=rwc"
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
        build_id: Some(prep.build_id.clone()),
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
    game_instance_repository: &Arc<dyn GameInstanceRepository>,
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

    let game_instance = GameInstance::new(
        argument.instance_id.0.clone(),
        crate::domain::GameInstanceStatus::Pedding,
        None,
        argument.build.build_id.clone(),
    );
    if let Err(err) = game_instance_repository.save(&game_instance).await {
        fail_operation(ops, op.clone(), &err.to_string().as_str()).await;
        return op;
    }

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

pub async fn enqueue_stop_instance(
    pool: &SqlitePool,
    ops: &Arc<dyn OperationRepository>,
    instance_id: &str,
) -> NodeOperation {
    let op = NodeOperation {
        operation_id: OperationId::new(),
        kind: OperationKind::StopInstance,
        status: OperationStatus::Pending,
        instance_id: Some(InstanceId(instance_id.to_string())),
        build_id: None,
        message: Some("instance stop queued".to_string()),
        started_at: Utc::now(),
        finished_at: None,
    };
    let _ = ops.save(&op).await;

    let job_op_id = op.operation_id.0.clone();
    let mut storage = SqliteStorage::new(pool);
    let _ = storage
        .push(StopInstanceJob {
            operation_id: job_op_id,
            instance_id: instance_id.to_string(),
        })
        .await;

    op
}

pub async fn enqueue_restart_instance(
    pool: &SqlitePool,
    ops: &Arc<dyn OperationRepository>,
    instance_id: &str,
) -> NodeOperation {
    let op = NodeOperation {
        operation_id: OperationId::new(),
        kind: OperationKind::RestartInstance,
        status: OperationStatus::Pending,
        instance_id: Some(InstanceId(instance_id.to_string())),
        build_id: None,
        message: Some("instance restart queued".to_string()),
        started_at: Utc::now(),
        finished_at: None,
    };
    let _ = ops.save(&op).await;

    let job_op_id = op.operation_id.0.clone();
    let mut storage = SqliteStorage::new(pool);
    let _ = storage
        .push(RestartInstanceJob {
            operation_id: job_op_id,
            instance_id: instance_id.to_string(),
        })
        .await;

    op
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
    ops: &Arc<dyn OperationRepository>,
    instance_id: &str,
    snapshot_id: &str,
    operation: NodeOperation,
) {
    // 入队时先持久化 Pending operation，保证 get_operation 能立刻查到
    let _ = ops.save(&operation).await;
    let mut storage = SqliteStorage::new(pool);
    let _ = storage
        .push(RestoreSnapshotJob {
            instance_id: instance_id.to_string(),
            snapshot_id: snapshot_id.to_string(),
            operation,
        })
        .await;
}

pub async fn enqueue_clean_instance(
    pool: &SqlitePool,
    ops: &Arc<dyn OperationRepository>,
    instance_id: &str,
) -> NodeOperation {
    let op = NodeOperation {
        operation_id: OperationId::new(),
        kind: OperationKind::CleanInstance,
        status: OperationStatus::Pending,
        instance_id: Some(InstanceId(instance_id.to_string())),
        build_id: None,
        message: Some("instance clean queued".to_string()),
        started_at: Utc::now(),
        finished_at: None,
    };
    let _ = ops.save(&op).await;

    let job_op_id = op.operation_id.0.clone();
    let mut storage = SqliteStorage::new(pool);
    let _ = storage
        .push(CleanInstanceJob {
            operation_id: job_op_id,
            instance_id: instance_id.to_string(),
        })
        .await;

    op
}
