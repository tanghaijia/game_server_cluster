fn main() -> Result<(), Box<dyn std::error::Error>> {
    let protoc = protoc_bin_vendored::protoc_bin_path()?;
    unsafe {
        std::env::set_var("PROTOC", protoc);
    }

    tonic_build::configure()
        .build_server(true)
        .build_client(true)
        .compile_protos(
            &[
                "proto/nodeagent/v1/node_agent.proto",
                "proto/assetservice/v1/asset_service.proto",
                "proto/assetservice/v1/business_service.proto",
            ],
            &["proto"],
        )?;

    println!("cargo:rerun-if-changed=proto/nodeagent/v1/node_agent.proto");
    println!("cargo:rerun-if-changed=proto/nodeagent/v1/port_mapping.proto");
    println!("cargo:rerun-if-changed=proto/nodeagent/v1/game_cache.proto");
    println!("cargo:rerun-if-changed=proto/assetservice/v1/asset_service.proto");
    println!("cargo:rerun-if-changed=proto/assetservice/v1/business_service.proto");
    Ok(())
}
