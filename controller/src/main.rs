use std::{net::SocketAddr, sync::Arc};

use controller::{
    clients::{AssetServiceGrpcClient, NodeAgentGrpcClient},
    implementations::{FakeScheduler, InMemoryInstanceRepository},
    ports::SystemClock,
    proto::controller::controller_service_server::ControllerServiceServer,
    rpc::GrpcControllerServer,
    service::ControllerService,
};
use tonic::transport::Server;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let addr: SocketAddr = std::env::var("CONTROLLER_ADDR")
        .unwrap_or_else(|_| "127.0.0.1:50051".to_string())
        .parse()?;
    let node_agent_endpoint = std::env::var("NODE_AGENT_ENDPOINT")
        .unwrap_or_else(|_| "http://127.0.0.1:50052".to_string());
    let asset_service_endpoint = std::env::var("ASSET_SERVICE_ENDPOINT")
        .unwrap_or_else(|_| "http://127.0.0.1:50053".to_string());

    let repository = Arc::new(InMemoryInstanceRepository::default());
    let scheduler = Arc::new(FakeScheduler::default());
    let asset_client = Arc::new(AssetServiceGrpcClient::connect(asset_service_endpoint).await?);
    let node_agent = Arc::new(NodeAgentGrpcClient::connect(node_agent_endpoint).await?);
    let clock = Arc::new(SystemClock);

    let service = Arc::new(ControllerService::new(
        repository,
        asset_client.clone(),
        scheduler,
        node_agent,
        asset_client,
        clock,
    ));
    let grpc = GrpcControllerServer::new(service);

    println!("controller listening on {}", addr);
    Server::builder()
        .add_service(ControllerServiceServer::new(grpc))
        .serve(addr)
        .await?;

    Ok(())
}
