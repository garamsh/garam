//! Generates the gRPC service and client from the contract. protox
//! compiles it in Rust, so no build needs a `protoc` binary.

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let proto = "../../proto/garam/policy/v1/policy.proto";
    println!("cargo:rerun-if-changed={proto}");
    let descriptors = protox::compile([proto], ["../../proto"])?;
    tonic_prost_build::configure().compile_fds(descriptors)?;
    Ok(())
}
