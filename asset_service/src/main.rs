use std::{net::SocketAddr, sync::Arc, time::Duration};

use asset_service::{
    domain::{AdapterId, AdapterVersion, BuildId, BuildStatus, Game, GameBuild},
    ports::{GameRepository, SystemClock},
    proto::asset_service::{
        asset_service_server::AssetServiceServer, business_service_server::BusinessServiceServer,
    },
    repositories::{
        FakeSteamService, InMemoryBuildRepository, InMemoryGameRepository,
        InMemoryModManifestRepository, InMemoryNodeAgentRepository, InMemoryNodeRepository,
        InMemorySnapshotRepository, InMemorySteamBranchRepository,
    },
    rpc::{GrpcAssetService, GrpcBusinessService},
    service::{AssetService, RegisterBuildRequest, SteamBranchSync},
};
use chrono::Utc;
use tonic::transport::Server;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let addr: SocketAddr = std::env::var("ASSET_SERVICE_ADDR")
        .unwrap_or_else(|_| "127.0.0.1:50053".to_string())
        .parse()?;

    let game_repo = Arc::new(InMemoryGameRepository::default());

    let fake_game_repos = Arc::new(InMemoryGameRepository::default());
    let service = Arc::new(AssetService::new(
        Arc::new(InMemoryBuildRepository::default()),
        Arc::new(InMemorySnapshotRepository::default()),
        Arc::new(InMemoryModManifestRepository::default()),
        Arc::new(SystemClock),
        fake_game_repos.clone(),
    ));

    seed_demo_builds(service.clone(), fake_game_repos.clone()).await?;

    // 启动 Steam 分支定期同步（每 15 分钟）
    let sync = SteamBranchSync::new(
        Arc::new(FakeSteamService),
        Arc::new(InMemorySteamBranchRepository::default()),
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
    let game_id = "dst".to_string();
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
                game_id: game_id,
                channel: Some("public".to_string()),
                adapter_id: AdapterId("dst".to_string()),
                adapter_version: AdapterVersion::new(0, 1, 0),
                upstream_version: Some("demo-upstream".to_string()),
                artifact_uri: Some("localhost:5000".to_string()),
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
    Ok(())
}
