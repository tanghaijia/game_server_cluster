use std::{net::SocketAddr, sync::Arc, time::Duration};

use asset_service::{
    domain::{AdapterId, AdapterVersion, BuildId, BuildStatus, GameBuild},
    ports::SystemClock,
    proto::asset_service::asset_service_server::AssetServiceServer,
    repositories::{
        FakeSteamService, InMemoryBuildRepository, InMemoryGameRepository,
        InMemoryModManifestRepository, InMemorySnapshotRepository,
        InMemorySteamBranchRepository,
    },
    rpc::GrpcAssetService,
    service::{AssetService, RegisterBuildRequest, SteamBranchSync},
};
use chrono::Utc;
use tonic::transport::Server;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let addr: SocketAddr = std::env::var("ASSET_SERVICE_ADDR")
        .unwrap_or_else(|_| "127.0.0.1:50053".to_string())
        .parse()?;

    let service = Arc::new(AssetService::new(
        Arc::new(InMemoryBuildRepository::default()),
        Arc::new(InMemorySnapshotRepository::default()),
        Arc::new(InMemoryModManifestRepository::default()),
        Arc::new(SystemClock),
    ));

    seed_demo_builds(service.clone()).await?;

    // 启动 Steam 分支定期同步（每 15 分钟）
    let sync = SteamBranchSync::new(
        Arc::new(FakeSteamService),
        Arc::new(InMemorySteamBranchRepository::default()),
        Arc::new(InMemoryGameRepository::default()),
        Duration::from_secs(15 * 60),
    );
    tokio::spawn(async move {
        sync.run().await;
    });

    let grpc = GrpcAssetService::new(service);

    println!("asset-service listening on {}", addr);
    Server::builder()
        .add_service(AssetServiceServer::new(grpc))
        .serve(addr)
        .await?;

    Ok(())
}

async fn seed_demo_builds(service: Arc<AssetService<InMemoryBuildRepository, InMemorySnapshotRepository, InMemoryModManifestRepository, SystemClock>>) -> Result<(), Box<dyn std::error::Error>> {
    let now = Utc::now();
    service.register_game_build(RegisterBuildRequest {
        build: GameBuild {
            build_id: BuildId("dst-public-demo-build".to_string()),
            game_id: "dst".to_string(),
            channel: Some("public".to_string()),
            adapter_id: AdapterId("dst".to_string()),
            adapter_version: AdapterVersion::new(0, 1, 0),
            upstream_version: Some("demo-upstream".to_string()),
            artifact_uri: Some("memory://builds/dst-public-demo-build.tar.zst".to_string()),
            checksum: Some("sha256:dst-public-demo-build".to_string()),
            status: BuildStatus::Available,
            pinned: true,
            resolved_at: now,
            created_at: now,
            updated_at: now,
        },
    }).await?;
    Ok(())
}
