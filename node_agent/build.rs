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
                "proto/node_agent.proto",
                "proto/asset_service.proto",
                "proto/business_service.proto",
            ],
            &["proto"],
        )?;

    println!("cargo:rerun-if-changed=proto/node_agent.proto");
    println!("cargo:rerun-if-changed=proto/asset_service.proto");
    println!("cargo:rerun-if-changed=proto/business_service.proto");
    Ok(())
}
