//! The policy service: serves `garam.policy.v1`, authenticates its
//! callers, and is the only crate joining adapters to ports.

mod caller;
mod config;
mod policy_service;
mod probes;
mod status;
mod tls;

use std::future::Future;

use decision::{Engine, EntitySource, PolicySource};
use entity::EntityStore;
use grant::PolicyStore;
use sqlx::PgPool;
use sqlx::postgres::PgPoolOptions;
use tokio::net::TcpListener;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::{Identity, Server, ServerTlsConfig};

pub use caller::{Caller, KeysError, Refusal, Verifier};
pub use config::{Config, ConfigError, database_url};
pub use policy_service::Service;
pub use probes::{ProbeError, probe};
pub use tls::{TlsError, load_identity};

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
    /// The probes' listener failed.
    #[error("the probes' listener failed")]
    Probes(#[source] std::io::Error),
}

/// What `serve` listens on.
#[derive(Debug)]
pub struct Listeners {
    /// The contract, over TLS.
    pub contract: TcpListener,
    /// `/healthz` and `/readyz`.
    pub probes: TcpListener,
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
/// over TLS under `identity`, with the probes beside it, until
/// `shutdown` resolves. No path serves the contract in plaintext: a
/// bearer token rides it (ADR-0138).
pub async fn serve(
    pool: PgPool,
    verifier: Verifier,
    identity: Identity,
    listeners: Listeners,
    shutdown: impl Future<Output = ()>,
) -> Result<(), Error> {
    let engine = Engine::load().map_err(Error::Engine)?;
    let service = Service::new(
        engine.clone(),
        PolicyStore::new(pool.clone()),
        EntityStore::new(pool.clone(), engine),
    );
    let contract = Server::builder()
        .tls_config(ServerTlsConfig::new().identity(identity))
        .map_err(Error::Transport)?
        .add_service(authenticated(service, verifier))
        .serve_with_incoming_shutdown(TcpListenerStream::new(listeners.contract), shutdown);
    tokio::select! {
        served = contract => served.map_err(Error::Transport),
        answered = probes::answer(listeners.probes, pool) => answered.map_err(Error::Probes),
    }
}
