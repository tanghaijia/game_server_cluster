mod common;

use std::{net::SocketAddr, sync::Arc};

use node_agent::{
    clients::{
        AssetServiceGrpcClient, DockerContainerClient, RealSystemInfoProvider, S3ObjectStore,
        SqliteDockerInstanceRepository, SqliteGameInstanceRepository, SqliteOperationRepository,
    },
    domain::{ImageRepository, ImageRepositoryCredentials},
    proto::node_agent::node_agent_service_server::NodeAgentServiceServer,
    providers::{
        FakeAssetServiceFace, FakeImageClient, FakeInstanceRuntime, FakeSystemInfoProvider,
        InMemoryObjectStore, InMemoryOperationRepository,
    },
    rpc::GrpcNodeAgentServer,
    service::{BackgroundWorker, NodeAgentService, TaskContext, init_backend, start_all_workers},
};
use tonic::transport::Server;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    if cfg!(debug_assertions) {
        let addr: SocketAddr = std::env::var("NODE_AGENT_ADDR")
            .unwrap_or_else(|_| "127.0.0.1:50052".to_string())
            .parse()?;

        // 1. 创建具体依赖（NodeAgentService 需要具体类型）
        let concrete_instance = Arc::new(FakeInstanceRuntime::default());
        let concrete_ops = Arc::new(InMemoryOperationRepository::default());
        let concrete_sysinfo = Arc::new(FakeSystemInfoProvider::default());
        let concrete_asset = Arc::new(FakeAssetServiceFace);
        let concrete_image = Arc::new(FakeImageClient::default());
        let concrete_object_store = Arc::new(InMemoryObjectStore::new());

        // 2. 构造 NodeAgentService（具体泛型）
        let node_agent_service = Arc::new(NodeAgentService::new(
            concrete_instance.clone(),
            concrete_sysinfo.clone(),
            concrete_asset.clone(),
            concrete_image.clone(),
            concrete_object_store.clone(),
        ));

        // 3. 初始化 apalis SQLite backend
        let pool = init_backend().await?;

        // 4. 构造 TaskContext（cast 到 Arc<dyn Trait>）
        let task_ctx = Arc::new(TaskContext::new(
            node_agent_service.clone() as Arc<dyn BackgroundWorker>,
            concrete_instance.clone() as Arc<dyn node_agent::ports::GameInstanceRepository>,
            concrete_ops.clone() as Arc<dyn node_agent::ports::OperationRepository>,
            concrete_asset as Arc<dyn node_agent::ports::AssetServiceFace>,
            concrete_image as Arc<dyn node_agent::ports::ContainerClient>,
        ));

        // 5. 启动后台 worker
        let _worker_handles = start_all_workers(pool.clone(), task_ctx);

        // 6. gRPC server（传入 pool + operations 用于投递后台任务）
        let grpc =
            GrpcNodeAgentServer::new(node_agent_service, pool, concrete_ops, concrete_instance);

        println!("node-agent listening on {}", addr);
        Server::builder()
            .add_service(NodeAgentServiceServer::new(grpc))
            .serve(addr)
            .await?;
    } else {
        // ================================================================
        // 生产环境：使用真实实现
        // ================================================================

        let addr: SocketAddr = std::env::var("NODE_AGENT_ADDR")
            .unwrap_or_else(|_| "0.0.0.0:50052".to_string())
            .parse()?;

        let node_id = std::env::var("NODE_ID").unwrap_or_else(|_| "node-unknown".to_string());

        let asset_service_addr = std::env::var("ASSET_SERVICE_ADDR")
            .unwrap_or_else(|_| "http://127.0.0.1:50053".to_string());

        let registry_addr =
            std::env::var("REGISTRY_ADDR").unwrap_or_else(|_| "127.0.0.1".to_string());

        let registry_port: i64 = std::env::var("REGISTRY_PORT")
            .unwrap_or_else(|_| "5000".to_string())
            .parse()?;

        // 1. 初始化 SQLite backend
        let pool = init_backend().await?;
        let pool_arc = Arc::new(pool.clone());

        // 2. SQLite repositories
        let sqlite_ops = Arc::new(SqliteOperationRepository::new(pool_arc.clone()).await?);
        let sqlite_game_instances =
            Arc::new(SqliteGameInstanceRepository::new(pool_arc.clone()).await?);
        let sqlite_docker_instances =
            Arc::new(SqliteDockerInstanceRepository::new(pool_arc.clone()).await?);

        // 3. 外部服务客户端
        let asset_client = Arc::new(AssetServiceGrpcClient::connect(&asset_service_addr).await?);
        let docker_client = Arc::new(DockerContainerClient::new(
            ImageRepository {
                id: "default".to_string(),
                address: registry_addr,
                port: registry_port,
                image_repository_credentials: ImageRepositoryCredentials {
                    username: std::env::var("REGISTRY_USERNAME").ok(),
                    password: std::env::var("REGISTRY_PASSWORD").ok(),
                    serveraddress: std::env::var("REGISTRY_SERVER_ADDRESS").ok(),
                    identitytoken: None,
                    auth: None,
                    email: None,
                    registrytoken: None,
                },
            },
            sqlite_docker_instances.clone(),
        ));

        // 4. 系统信息提供者
        let system_info = Arc::new(RealSystemInfoProvider::new(node_id.clone()));

        // 5. 对象存储
        let s3_config = aws_config::load_from_env().await;
        let s3_client = aws_sdk_s3::Client::new(&s3_config);
        let object_store = Arc::new(S3ObjectStore::new(s3_client));

        // 6. NodeAgentService
        let node_agent_service = Arc::new(NodeAgentService::new(
            sqlite_game_instances.clone(),
            system_info.clone(),
            asset_client.clone(),
            docker_client.clone(),
            object_store.clone(),
        ));

        // 7. TaskContext
        let task_ctx = Arc::new(TaskContext::new(
            node_agent_service.clone() as Arc<dyn BackgroundWorker>,
            sqlite_game_instances.clone() as Arc<dyn node_agent::ports::GameInstanceRepository>,
            sqlite_ops.clone() as Arc<dyn node_agent::ports::OperationRepository>,
            asset_client as Arc<dyn node_agent::ports::AssetServiceFace>,
            docker_client as Arc<dyn node_agent::ports::ContainerClient>,
        ));

        // 8. 后台 worker + gRPC server
        let _worker_handles = start_all_workers(pool.clone(), task_ctx);
        let grpc =
            GrpcNodeAgentServer::new(node_agent_service, pool, sqlite_ops, sqlite_game_instances);

        println!("node-agent (production) listening on {}", addr);
        Server::builder()
            .add_service(NodeAgentServiceServer::new(grpc))
            .serve(addr)
            .await?;
    }

    Ok(())
}
