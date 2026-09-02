mod common;

use std::{net::SocketAddr, path::PathBuf, sync::Arc};

use node_agent::{
    clients::{
        AssetServiceGrpcClient, DockerContainerClient, RealSystemInfoProvider, S3ObjectStore,
        SqliteDockerInstanceRepository, SqliteGameCacheRepository, SqliteGameInstanceRepository,
        SqliteLocalGameBuildRepository, SqliteOperationRepository, SteamServiceClient,
    },
    domain::{ImageRepository, ImageRepositoryCredentials},
    proto::node_agent::node_agent_service_server::NodeAgentServiceServer,
    providers::{
        FakeAssetServiceFace, FakeImageClient, FakeInstanceRuntime, FakeSteamService,
        FakeSystemInfoProvider, InMemoryGameCacheRepository, InMemoryLocalGameBuildRepository,
        InMemoryObjectStore, InMemoryOperationRepository,
    },
    rpc::GrpcNodeAgentServer,
    service::{
        BackendContainerChecker, BackgroundWorker, DirectoryUploadDownloadService,
        NodeAgentService, RuntimeProbeService, TaskContext, init_backend, start_all_workers,
    },
};
use tonic::transport::Server;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    // P1：双输出日志（stderr + 滚动文件 <NODE_AGENT_LOG_DIR>/node-agent.log），见 docs/node-agent-logging-design.md
    let log_dir = std::env::var("NODE_AGENT_LOG_DIR").unwrap_or_else(|_| "./logs".to_string());
    node_agent::logging::init_logging(&log_dir);
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
        let directory_service =
            Arc::new(DirectoryUploadDownloadService::new(concrete_object_store.clone()));

        // 2. 初始化 apalis SQLite backend
        let pool = init_backend().await?;

        // 3. GameCache + Steam 依赖
        let game_cache_repos = Arc::new(InMemoryGameCacheRepository::default());
        let steam_service = Arc::new(FakeSteamService);
        let local_game_build_repos = Arc::new(InMemoryLocalGameBuildRepository::default());

        // 4. 构造 NodeAgentService（具体泛型）
        let node_agent_service = Arc::new(NodeAgentService::new(
            concrete_instance.clone(),
            concrete_sysinfo.clone(),
            concrete_asset.clone(),
            concrete_image.clone(),
            local_game_build_repos.clone(),
            directory_service.clone(),
            game_cache_repos.clone(),
            steam_service.clone(),
        ));

        // 4.5. 运行时探针（B-04/P1-1）：在 concrete_image/concrete_instance 被 move 前 clone
        let mut runtime_probe =
            RuntimeProbeService::new(concrete_image.clone(), concrete_instance.clone());

        // 5. 构造 TaskContext（cast 到 Arc<dyn Trait>）
        let task_ctx = Arc::new(TaskContext::new(
            node_agent_service.clone() as Arc<dyn BackgroundWorker>,
            concrete_instance.clone() as Arc<dyn node_agent::ports::GameInstanceRepository>,
            concrete_ops.clone() as Arc<dyn node_agent::ports::OperationRepository>,
            concrete_asset as Arc<dyn node_agent::ports::AssetServiceFace>,
            concrete_image as Arc<dyn node_agent::ports::ContainerClient>,
        ));

        // 6. 启动后台 worker
        let _worker_handles = start_all_workers(pool.clone(), task_ctx);

        // 7. 文件服务（M1，见 docs/file-manager-design.md）+ agent 日志 tail（P2）
        let file_secret =
            std::env::var("NODE_AGENT_FILE_SECRET").unwrap_or_else(|_| "dev-file-secret-change-me".to_string());
        let file_data_override =
            std::env::var("FILE_DATA_ROOT_OVERRIDE").ok().map(PathBuf::from);
        let log_dir = Some(PathBuf::from(&log_dir));
        let file_server = Arc::new(node_agent::file_server::FileServer::new(
            concrete_instance.clone() as Arc<dyn node_agent::ports::GameInstanceRepository>,
            file_secret.into_bytes(),
            file_data_override,
            log_dir,
        ));
        let file_addr: SocketAddr = std::env::var("FILE_SERVER_ADDR")
            .unwrap_or_else(|_| "127.0.0.1:50054".to_string())
            .parse()?;
        tokio::spawn(async move {
            let app = node_agent::file_server::FileServer::router(file_server);
            let listener = tokio::net::TcpListener::bind(file_addr)
                .await
                .expect("bind file server");
            println!("file server listening on {file_addr}");
            axum::serve(listener, app).await.expect("serve file server");
        });

        // 8. gRPC server（附加运行时探针：B-04/P1-1，debug 走 fake 实现，无实际探测）
        runtime_probe.start(std::time::Duration::from_secs(20)).await;
        let grpc = GrpcNodeAgentServer::new(node_agent_service, pool, concrete_ops, concrete_instance)
            .with_runtime_probe(Arc::new(runtime_probe));

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
        let sdk_config = aws_config::load_from_env().await;
        let s3_endpoint = std::env::var("S3_ENDPOINT").ok();
        let s3_client = if let Some(endpoint) = &s3_endpoint {
            // 使用自定义 endpoint（如 MinIO / Cloudflare R2）：这些 S3 兼容服务需要 path-style 寻址
            let s3_config = aws_sdk_s3::config::Builder::from(&sdk_config)
                .endpoint_url(endpoint)
                .force_path_style(true)
                .build();
            aws_sdk_s3::Client::from_conf(s3_config)
        } else {
            aws_sdk_s3::Client::new(&sdk_config)
        };
        let object_store = Arc::new(S3ObjectStore::new(s3_client));
        let directory_service = Arc::new(DirectoryUploadDownloadService::new(object_store.clone()));

        // 6. GameCache + Steam + 本地构建仓库 依赖
        let game_cache_repos = Arc::new(SqliteGameCacheRepository::new(pool_arc.clone()).await?);
        let local_game_build_repos =
            Arc::new(SqliteLocalGameBuildRepository::new(pool_arc.clone()).await?);
        let ssc = SteamServiceClient::new(game_cache_repos.clone());
        if let Err(e) = ssc.program_init().await {
            log::error!("SteamServiceClient program_init fail: {e}");
            return Err(e.into());
        }
        let steam_service = Arc::new(ssc);

        // 7. NodeAgentService
        let node_agent_service = Arc::new(NodeAgentService::new(
            sqlite_game_instances.clone(),
            system_info.clone(),
            asset_client.clone(),
            docker_client.clone(),
            local_game_build_repos.clone(),
            directory_service.clone(),
            game_cache_repos.clone(),
            steam_service.clone(),
        ));

        // P2-D：启动时回收孤儿缓存版本（上次崩溃残留的 Removed/半成品，释放磁盘）
        node_agent_service.gc_all_orphans().await;

        // 8. TaskContext
        let task_ctx = Arc::new(TaskContext::new(
            node_agent_service.clone() as Arc<dyn BackgroundWorker>,
            sqlite_game_instances.clone() as Arc<dyn node_agent::ports::GameInstanceRepository>,
            sqlite_ops.clone() as Arc<dyn node_agent::ports::OperationRepository>,
            asset_client as Arc<dyn node_agent::ports::AssetServiceFace>,
            docker_client.clone() as Arc<dyn node_agent::ports::ContainerClient>,
        ));

        // 9. 文件服务（M1，见 docs/file-manager-design.md）+ agent 日志 tail（P2）
        let file_secret =
            std::env::var("NODE_AGENT_FILE_SECRET").unwrap_or_else(|_| "dev-file-secret-change-me".to_string());
        let file_data_override =
            std::env::var("FILE_DATA_ROOT_OVERRIDE").ok().map(PathBuf::from);
        let log_dir = Some(PathBuf::from(&log_dir));
        let file_server = Arc::new(node_agent::file_server::FileServer::new(
            sqlite_game_instances.clone() as Arc<dyn node_agent::ports::GameInstanceRepository>,
            file_secret.into_bytes(),
            file_data_override,
            log_dir,
        ));
        let file_addr: SocketAddr = std::env::var("FILE_SERVER_ADDR")
            .unwrap_or_else(|_| "0.0.0.0:50054".to_string())
            .parse()?;
        tokio::spawn(async move {
            let app = node_agent::file_server::FileServer::router(file_server);
            let listener = tokio::net::TcpListener::bind(file_addr)
                .await
                .expect("bind file server");
            println!("file server listening on {file_addr}");
            axum::serve(listener, app).await.expect("serve file server");
        });

        // 10. 后台 worker + gRPC server
        let _worker_handles = start_all_workers(pool.clone(), task_ctx);

        // B-04/P1-1：运行时探针（脚本后端）——后台 exec health.sh/players.sh，心跳携带结果
        let probe_interval = std::env::var("RUNTIME_PROBE_INTERVAL_SEC")
            .ok()
            .and_then(|s| s.parse::<u64>().ok())
            .unwrap_or(20);
        let mut runtime_probe = RuntimeProbeService::new(
            docker_client.clone(),
            sqlite_game_instances.clone(),
        );
        runtime_probe.start(std::time::Duration::from_secs(probe_interval)).await;

        let grpc = GrpcNodeAgentServer::new(
            node_agent_service,
            pool,
            sqlite_ops,
            sqlite_game_instances.clone(),
        )
        .with_runtime_probe(Arc::new(runtime_probe));

        // 9. container checker
        let mut conatiner_checker =
            BackendContainerChecker::new(docker_client, sqlite_game_instances);
        conatiner_checker.start_check().await;

        println!("node-agent (production) listening on {}", addr);

        // 10. graceful shutdown
        let signal = async {
            tokio::signal::ctrl_c().await.ok();
            log::info!("received SIGINT, starting graceful shutdown...");
            log::info!("press Ctrl+C again to force exit");

            #[cfg(unix)]
            tokio::select! {
                _ = tokio::signal::ctrl_c() => {
                    log::warn!("force exit on second SIGINT");
                    std::process::exit(1);
                }
                _ = tokio::time::sleep(std::time::Duration::from_secs(30)) => {}
            }

            #[cfg(windows)]
            tokio::time::sleep(std::time::Duration::from_secs(30)).await;
        };

        Server::builder()
            .add_service(NodeAgentServiceServer::new(grpc))
            .serve_with_shutdown(addr, signal)
            .await?;

        // 11. stop background checker
        conatiner_checker.stop_check().await;
        log::info!("node-agent (production) shutdown complete");
    }

    Ok(())
}
