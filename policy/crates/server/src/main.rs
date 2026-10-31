use std::error::Error;

use server::{
    Config, Listeners, Verifier, connect, database_url, load_identity, migrate, probe, serve,
};
use tokio::net::TcpListener;

#[tokio::main]
async fn main() -> Result<(), Box<dyn Error>> {
    tracing_subscriber::fmt::init();
    let mut arguments = std::env::args().skip(1);
    match (arguments.next().as_deref(), arguments.next()) {
        (Some("serve"), None) => run_service().await,
        (Some("migrate"), None) => run_migrations().await,
        (Some("probe"), Some(url)) => Ok(probe(&url).await?),
        _ => Err("usage: policy serve | policy migrate | policy probe <url>".into()),
    }
}

async fn run_service() -> Result<(), Box<dyn Error>> {
    let config = Config::from_env()?;
    let verifier = Verifier::load(&config.jwks_file, &config.token_issuer)?;
    let identity = load_identity(&config.tls_cert_file, &config.tls_key_file)?;
    let pool = connect(&config.database_url).await?;
    let listeners = Listeners {
        contract: TcpListener::bind(&config.addr).await?,
        probes: TcpListener::bind(&config.health_addr).await?,
    };
    tracing::info!(addr = %config.addr, health_addr = %config.health_addr, "serving");
    serve(pool, verifier, identity, listeners, shutdown()).await?;
    Ok(())
}

/// Applies the migrations and exits, listening on nothing.
async fn run_migrations() -> Result<(), Box<dyn Error>> {
    let pool = connect(&database_url()?).await?;
    migrate(&pool).await?;
    tracing::info!("migrated");
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
