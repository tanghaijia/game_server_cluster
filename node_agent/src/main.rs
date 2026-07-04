mod common;

use std::{net::SocketAddr, sync::Arc};

use node_agent::{
    proto::node_agent::node_agent_service_server::NodeAgentServiceServer,
    providers::{
        FakeAssetServiceFace, FakeImageClient, FakeInstanceRuntime, FakeSnapshotRuntime,
        FakeSystemInfoProvider, InMemoryOperationRepository,
    },
    rpc::GrpcNodeAgentServer,
    service::{
        init_backend, start_all_workers, BackgroundWorker, NodeAgentService, TaskContext,
    },
};
use tonic::transport::Server;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let addr: SocketAddr = std::env::var("NODE_AGENT_ADDR")
        .unwrap_or_else(|_| "127.0.0.1:50052".to_string())
        .parse()?;

    // 1. 创建具体依赖（NodeAgentService 需要具体类型）
    let concrete_instance = Arc::new(FakeInstanceRuntime::default());
    let concrete_ops = Arc::new(InMemoryOperationRepository::default());
    let concrete_sysinfo = Arc::new(FakeSystemInfoProvider::default());
    let concrete_asset = Arc::new(FakeAssetServiceFace);
    let concrete_image = Arc::new(FakeImageClient);
    let s3_config = aws_sdk_s3::Config::builder()
        .region(aws_sdk_s3::config::Region::new("us-east-1"))
        .behavior_version_latest()
        .build();
    let s3_client = Arc::new(aws_sdk_s3::Client::from_conf(s3_config));
    let concrete_snapshot = Arc::new(FakeSnapshotRuntime);

    // 2. 构造 NodeAgentService（具体泛型）
    let node_agent_service = Arc::new(NodeAgentService::new(
        concrete_instance.clone(),
        concrete_sysinfo.clone(),
        concrete_asset.clone(),
        concrete_image.clone(),
        s3_client.clone(),
    ));

    // 3. 初始化 apalis SQLite backend
    let pool = init_backend().await?;

    // 4. 构造 TaskContext（cast 到 Arc<dyn Trait>）
    let task_ctx = Arc::new(TaskContext::new(
        node_agent_service.clone() as Arc<dyn BackgroundWorker>,
        concrete_instance as Arc<dyn node_agent::ports::InstanceRuntime>,
        concrete_snapshot as Arc<dyn node_agent::ports::SnapshotRuntime>,
        concrete_ops.clone() as Arc<dyn node_agent::ports::OperationRepository>,
        concrete_asset as Arc<dyn node_agent::ports::AssetServiceFace>,
        concrete_image as Arc<dyn node_agent::ports::ImageClient>,
    ));

    // 5. 启动后台 worker
    let _worker_handles = start_all_workers(pool.clone(), task_ctx);

    // 6. gRPC server（传入 pool + operations 用于投递后台任务）
    let grpc = GrpcNodeAgentServer::new(
        node_agent_service,
        pool,
        concrete_ops as Arc<dyn node_agent::ports::OperationRepository>,
    );

    println!("node-agent listening on {}", addr);
    Server::builder()
        .add_service(NodeAgentServiceServer::new(grpc))
        .serve(addr)
        .await?;

    Ok(())
}
