pub mod node_agent {
    tonic::include_proto!("nodeagent.v1");
}

pub mod google {
    pub mod rpc {
        tonic::include_proto!("google.rpc");
    }
}

pub mod asset_service {
    tonic::include_proto!("assetservice.v1");
}
