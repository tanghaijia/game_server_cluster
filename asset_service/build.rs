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
                "proto/assetservice/v1/asset_service.proto",
                "proto/assetservice/v1/business_service.proto",
            ],
            &["proto"],
        )?;

    println!("cargo:rerun-if-changed=proto/assetservice/v1/asset_service.proto");
    println!("cargo:rerun-if-changed=proto/assetservice/v1/business_service.proto");
    Ok(())
}
