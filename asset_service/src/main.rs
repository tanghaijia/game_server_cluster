use std::{net::SocketAddr, sync::Arc, time::Duration};

use asset_service::{
    clients::SteamServiceHttp,
    domain::{AdapterId, AdapterVersion, BuildId, BuildStatus, Game, GameBuild},
    ports::{GameRepository, SystemClock},
    proto::asset_service::{
        asset_service_server::AssetServiceServer, business_service_server::BusinessServiceServer,
    },
    repositories::{
        InMemoryBuildRepository, InMemoryGameRepository, InMemoryModManifestRepository,
        InMemoryNodeAgentRepository, InMemoryNodeRepository, InMemorySnapshotRepository,
        InMemorySteamBranchRepository, SqlBuildRepository, SqlGameRepository,
        SqlModManifestRepository, SqlNodeAgentRepository, SqlNodeRepository, SqlSnapshotRepository,
        SqlSteamBranchRepository, create_pool, run_migrations,
    },
    rpc::{GrpcAssetService, GrpcBusinessService},
    service::{AssetService, RegisterBuildRequest, SteamBranchSync},
};
use chrono::Utc;
use tonic::transport::Server;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    env_logger::init();
    let addr: SocketAddr = std::env::var("ASSET_SERVICE_ADDR")
        .unwrap_or_else(|_| "127.0.0.1:50053".to_string())
        .parse()?;

    let database_url = std::env::var("DATABASE_URL").ok();

    match &database_url {
        Some(url) => run_with_sql(url, addr).await?,
        None => run_with_in_memory(addr).await?,
    }

    Ok(())
}

// ── SQL (PostgreSQL) 模式 ─────────────────────────────────────────────────────

async fn run_with_sql(
    database_url: &str,
    addr: SocketAddr,
) -> Result<(), Box<dyn std::error::Error>> {
    println!("[asset-service] using PostgreSQL storage");

    let pool = create_pool(database_url).await?;
    run_migrations(&pool).await?;
    println!("[asset-service] database migrations applied");

    let game_repo = Arc::new(SqlGameRepository::new(pool.clone()));
    let build_repo = Arc::new(SqlBuildRepository::new(pool.clone()));
    let snapshot_repo = Arc::new(SqlSnapshotRepository::new(pool.clone()));
    let manifest_repo = Arc::new(SqlModManifestRepository::new(pool.clone()));
    let steam_branch_repo = Arc::new(SqlSteamBranchRepository::new(pool.clone()));
    let node_repo = Arc::new(SqlNodeRepository::new(pool.clone()));
    let node_agent_repo = Arc::new(SqlNodeAgentRepository::new(pool.clone()));

    let service = Arc::new(AssetService::new(
        build_repo,
        snapshot_repo,
        manifest_repo,
        Arc::new(SystemClock),
        game_repo.clone(),
    ));

    let sync = SteamBranchSync::new(
        Arc::new(SteamServiceHttp::new()),
        steam_branch_repo.clone(),
        game_repo.clone(),
        Duration::from_secs(15 * 60),
    );
    tokio::spawn(async move {
        sync.run().await;
    });

    let business =
        GrpcBusinessService::new(game_repo, node_repo, node_agent_repo, steam_branch_repo);
    let grpc = GrpcAssetService::new(service);

    println!("asset-service listening on {}", addr);
    Server::builder()
        .add_service(AssetServiceServer::new(grpc))
        .add_service(BusinessServiceServer::new(business))
        .serve(addr)
        .await?;

    Ok(())
}

// ── In-Memory 模式（开发/演示用）───────────────────────────────────────────────

async fn run_with_in_memory(addr: SocketAddr) -> Result<(), Box<dyn std::error::Error>> {
    println!("[asset-service] using in-memory storage");

    let game_repo = Arc::new(InMemoryGameRepository::default());
    let demo_game_repo = Arc::new(InMemoryGameRepository::default());

    let service = Arc::new(AssetService::new(
        Arc::new(InMemoryBuildRepository::default()),
        Arc::new(InMemorySnapshotRepository::default()),
        Arc::new(InMemoryModManifestRepository::default()),
        Arc::new(SystemClock),
        demo_game_repo.clone(),
    ));

    seed_demo_builds(service.clone(), demo_game_repo).await?;

    let steam_branch_repo = Arc::new(InMemorySteamBranchRepository::default());

    let sync = SteamBranchSync::new(
        Arc::new(SteamServiceHttp::new()),
        steam_branch_repo.clone(),
        game_repo.clone(),
        Duration::from_secs(15 * 60),
    );
    tokio::spawn(async move {
        sync.run().await;
    });

    let business = GrpcBusinessService::new(
        game_repo,
        Arc::new(InMemoryNodeRepository::default()),
        Arc::new(InMemoryNodeAgentRepository::default()),
        steam_branch_repo,
    );

    let grpc = GrpcAssetService::new(service);

    println!("asset-service listening on {}", addr);
    Server::builder()
        .add_service(AssetServiceServer::new(grpc))
        .add_service(BusinessServiceServer::new(business))
        .serve(addr)
        .await?;

    Ok(())
}

// ── Demo 数据播种 ──────────────────────────────────────────────────────────────

async fn seed_demo_builds(
    service: Arc<
        AssetService<
            InMemoryBuildRepository,
            InMemorySnapshotRepository,
            InMemoryModManifestRepository,
            SystemClock,
            InMemoryGameRepository,
        >,
    >,
    game_repository: Arc<InMemoryGameRepository>,
) -> Result<(), Box<dyn std::error::Error>> {
    let now = Utc::now();
    let game_id = "343050".to_string();
    game_repository
        .save(&Game {
            id: game_id.clone(),
            name: "Dont stave together".to_string(),
            app_id: "343050".to_string(),
        })
        .await?;
    service
        .register_game_build(RegisterBuildRequest {
            build: GameBuild {
                build_id: BuildId("dst-public-demo-build".to_string()),
                game_id,
                channel: Some("public".to_string()),
                adapter_id: AdapterId("dst".to_string()),
                adapter_version: AdapterVersion::new(0, 1, 0),
                upstream_version: Some("demo-upstream".to_string()),
                artifact_uri: Some("ccr.ccs.tencentyun.com/cluster_game_server".to_string()),
                artifact_image_name: Some("dst-adapter".to_string()),
                artifact_image_tag: Some("0.2.2".to_string()),
                status: BuildStatus::Available,
                pinned: true,
                resolved_at: now,
                created_at: now,
                updated_at: now,
            },
        })
        .await?;

    // 7 Days to Die 专用服务器（AppID 294420）
    let game_id_7dtd = "294420".to_string();
    game_repository
        .save(&Game {
            id: game_id_7dtd.clone(),
            name: "7 Days to Die".to_string(),
            app_id: "294420".to_string(),
        })
        .await?;
    service
        .register_game_build(RegisterBuildRequest {
            build: GameBuild {
                build_id: BuildId("7dtd-public-demo-build".to_string()),
                game_id: game_id_7dtd,
                channel: Some("public".to_string()),
                adapter_id: AdapterId("7daystodie".to_string()),
                adapter_version: AdapterVersion::new(0, 1, 0),
                upstream_version: Some("demo-upstream".to_string()),
                artifact_uri: Some("ccr.ccs.tencentyun.com/cluster_game_server".to_string()),
                artifact_image_name: Some("7daystodie-adapter".to_string()),
                artifact_image_tag: Some("0.1.0".to_string()),
                status: BuildStatus::Available,
                pinned: true,
                resolved_at: now,
                created_at: now,
                updated_at: now,
            },
        })
        .await?;
    Ok(())
}
