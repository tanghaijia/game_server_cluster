use std::{net::SocketAddr, sync::Arc};

use asset_service::{
    ports::SystemClock,
    proto::asset_service::asset_service_server::AssetServiceServer,
    repositories::{
        InMemoryBuildRepository, InMemoryModManifestRepository, InMemorySnapshotRepository,
    },
    rpc::GrpcAssetService,
    service::AssetService,
};
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
    let grpc = GrpcAssetService::new(service);

    println!("asset-service listening on {}", addr);
    Server::builder()
        .add_service(AssetServiceServer::new(grpc))
        .serve(addr)
        .await?;

    Ok(())
}
