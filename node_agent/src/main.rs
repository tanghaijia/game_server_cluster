use std::{net::SocketAddr, sync::Arc};

use node_agent::{
    proto::node_agent::node_agent_service_server::NodeAgentServiceServer,
    providers::{
        FakeBuildRuntime, FakeInstanceRuntime, FakeSnapshotRuntime, FakeSystemInfoProvider,
        InMemoryOperationRepository,
    },
    rpc::GrpcNodeAgentServer,
    service::NodeAgentService,
};
use tonic::transport::Server;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let addr: SocketAddr = std::env::var("NODE_AGENT_ADDR")
        .unwrap_or_else(|_| "127.0.0.1:50052".to_string())
        .parse()?;

    let service = Arc::new(NodeAgentService::new(
        Arc::new(FakeBuildRuntime::default()),
        Arc::new(FakeInstanceRuntime::default()),
        Arc::new(FakeSnapshotRuntime),
        Arc::new(InMemoryOperationRepository::default()),
        Arc::new(FakeSystemInfoProvider::default()),
    ));
    let grpc = GrpcNodeAgentServer::new(service);

    println!("node-agent listening on {}", addr);
    Server::builder()
        .add_service(NodeAgentServiceServer::new(grpc))
        .serve(addr)
        .await?;

    Ok(())
}
