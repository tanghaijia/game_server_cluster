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
                "proto/controller.proto",
                "../asset_service/proto/asset_service.proto",
                "../node_agent/proto/node_agent.proto",
            ],
            &["proto", "../asset_service/proto", "../node_agent/proto"],
        )?;

    println!("cargo:rerun-if-changed=proto/controller.proto");
    println!("cargo:rerun-if-changed=../asset_service/proto/asset_service.proto");
    println!("cargo:rerun-if-changed=../node_agent/proto/node_agent.proto");
    Ok(())
}
