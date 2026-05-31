use std::{net::SocketAddr, sync::Arc};

use controller::{
    implementations::{
        FakeBuildResolver, FakeNodeAgentClient, FakeScheduler, FakeSnapshotService,
        InMemoryInstanceRepository,
    },
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

    let repository = Arc::new(InMemoryInstanceRepository::default());
    let build_resolver = Arc::new(FakeBuildResolver);
    let scheduler = Arc::new(FakeScheduler::default());
    let node_agent = Arc::new(FakeNodeAgentClient::default());
    let snapshots = Arc::new(FakeSnapshotService::default());
    let clock = Arc::new(SystemClock);

    let service = Arc::new(ControllerService::new(
        repository,
        build_resolver,
        scheduler,
        node_agent,
        snapshots,
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
