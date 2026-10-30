use std::error::Error;

use server::{Config, Verifier, database, serve};
use tokio::net::TcpListener;

#[tokio::main]
async fn main() -> Result<(), Box<dyn Error>> {
    tracing_subscriber::fmt::init();
    let config = Config::from_env()?;
    let verifier = Verifier::load(&config.jwks_file, &config.token_issuer)?;
    let pool = database(&config.database_url).await?;
    let listener = TcpListener::bind(&config.addr).await?;
    tracing::info!(addr = %config.addr, "serving");
    serve(pool, verifier, listener, shutdown()).await?;
    Ok(())
}

/// Resolves on the signal an orchestrator stops the process with, or
/// on an interrupt.
async fn shutdown() {
    let terminate = async {
        match tokio::signal::unix::signal(tokio::signal::unix::SignalKind::terminate()) {
            Ok(mut signal) => {
                signal.recv().await;
            }
            Err(error) => {
                tracing::error!(%error, "no terminate handler");
                std::future::pending::<()>().await;
            }
        }
    };
    tokio::select! {
        _ = terminate => {}
        _ = tokio::signal::ctrl_c() => {}
    }
}
