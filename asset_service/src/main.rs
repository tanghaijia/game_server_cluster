use std::{net::SocketAddr, sync::Arc};

use asset_service::{
    domain::{BuildId, BuildStatus, GameBuild, GameKind},
    ports::SystemClock,
    proto::asset_service::asset_service_server::AssetServiceServer,
    repositories::{
        InMemoryBuildRepository, InMemoryModManifestRepository, InMemorySnapshotRepository,
    },
    rpc::GrpcAssetService,
    service::{AssetService, RegisterBuildRequest},
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
            game: GameKind::Dst,
            channel: Some("public".to_string()),
            adapter_version: Some("adapter-demo-v1".to_string()),
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
