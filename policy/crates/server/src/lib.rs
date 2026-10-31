//! The policy service: serves `garam.policy.v1`, authenticates its
//! callers, and is the only crate joining adapters to ports.

mod caller;
mod config;
mod policy_service;
mod status;

use std::future::Future;

use decision::{Engine, EntitySource, PolicySource};
use entity::EntityStore;
use grant::PolicyStore;
use sqlx::PgPool;
use sqlx::postgres::PgPoolOptions;
use tokio::net::TcpListener;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::Server;

pub use caller::{Caller, KeysError, Refusal, Verifier};
pub use config::{Config, ConfigError, database_url};
pub use policy_service::Service;

/// The generated contract.
pub mod proto {
    tonic::include_proto!("garam.policy.v1");
}

use proto::policy_service_server::PolicyServiceServer;

/// Why the service stopped or did not start.
#[derive(Debug, thiserror::Error)]
pub enum Error {
    /// The engine does not load.
    #[error(transparent)]
    Engine(decision::Error),
    /// The database cannot be reached.
    #[error("the database cannot be reached")]
    Database(#[source] sqlx::Error),
    /// The migrations do not apply.
    #[error("the migrations do not apply")]
    Migration(#[source] sqlx::migrate::MigrateError),
    /// The server failed.
    #[error("the server failed")]
    Transport(#[source] tonic::transport::Error),
}

/// Opens the service's database. It applies nothing: `migrate` does,
/// as a job of its own.
pub async fn connect(url: &str) -> Result<PgPool, Error> {
    PgPoolOptions::new()
        .connect(url)
        .await
        .map_err(Error::Database)
}

/// Applies every pending migration. `sqlx` runs each in a transaction
/// under an advisory lock, and refuses a database an earlier run left
/// dirty rather than clearing it.
pub async fn migrate(pool: &PgPool) -> Result<(), Error> {
    sqlx::migrate!("../../migrations")
        .run(pool)
        .await
        .map_err(Error::Migration)
}

/// The contract behind caller authentication.
pub fn authenticated<P, E>(
    service: Service<P, E>,
    verifier: Verifier,
) -> InterceptedService<PolicyServiceServer<Service<P, E>>, Verifier>
where
    P: PolicySource + Send + Sync + 'static,
    E: EntitySource + Send + Sync + 'static,
{
    PolicyServiceServer::with_interceptor(service, verifier)
}

/// The shipped graph: the Postgres stores behind the contract, served
/// on `listener` until `shutdown` resolves.
pub async fn serve(
    pool: PgPool,
    verifier: Verifier,
    listener: TcpListener,
    shutdown: impl Future<Output = ()>,
) -> Result<(), Error> {
    let engine = Engine::load().map_err(Error::Engine)?;
    let service = Service::new(
        engine.clone(),
        PolicyStore::new(pool.clone()),
        EntityStore::new(pool, engine),
    );
    Server::builder()
        .add_service(authenticated(service, verifier))
        .serve_with_incoming_shutdown(TcpListenerStream::new(listener), shutdown)
        .await
        .map_err(Error::Transport)
}
