//! The policy service: serves `garam.policy.v1` and AuthZEN's HTTP
//! binding, authenticates its callers, and is the only crate joining
//! adapters to ports.

mod caller;
mod config;
mod entity_changes;
mod entity_sink;
mod http;
mod policy_service;
mod policy_write;
mod probes;
mod status;
mod tls;

use std::future::Future;
use std::time::Duration;

use audit::Recorder;
use decision::{Engine, EntitySource, PolicySource};
use entity::EntityStore;
use grant::PolicyStore;
use sqlx::PgPool;
use sqlx::postgres::PgPoolOptions;
use tls::Incoming;
use tokio::net::TcpListener;
use tonic::service::Routes;
use tonic::service::interceptor::InterceptedService;
use tonic::transport::Server;

pub use caller::{Caller, KeysError, Principal, Refusal, Verifier};
pub use config::{Config, ConfigError, database_url};
pub use entity_sink::EntitySink;
pub use policy_service::Service;
pub use probes::{ProbeError, probe};
pub use tls::{Identity, TlsError, load_identity};

/// The generated contract. The size of its `Operation` is the
/// generator's, and one write is one message either way.
#[allow(clippy::large_enum_variant)]
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
    /// The certificate and key do not make a listener's identity.
    #[error(transparent)]
    Tls(TlsError),
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
    /// `/healthz`, `/readyz` and `/metrics`.
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
    E: EntitySource + EntitySink + Send + Sync + 'static,
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
    let recorder = Recorder::new(pool.clone());
    let service = Service::new(
        engine.clone(),
        PolicyStore::new(pool.clone()),
        EntityStore::new(pool.clone(), engine),
    )
    .writing_to(PolicyStore::new(pool.clone()))
    .recording_to(recorder.clone());
    let incoming = Incoming::new(listeners.contract, identity.acceptor().map_err(Error::Tls)?);
    // The binding's routes join the contract's beside it: one listener,
    // one TLS, and the same service and verifier behind both.
    let routes = Routes::new(authenticated(service.clone(), verifier.clone()))
        .into_axum_router()
        .merge(http::router(service, verifier));
    let mut server = Server::builder().accept_http1(true);
    let contract = server
        .add_routes(Routes::from(routes))
        .serve_with_incoming_shutdown(incoming, shutdown);
    let stopped = tokio::select! {
        served = contract => served.map_err(Error::Transport),
        answered = probes::answer(listeners.probes, pool, recorder.clone()) => answered.map_err(Error::Probes),
        () = sweeping(recorder.clone()) => Ok(()),
    };
    // What is still queued is given this long to be recorded; the rest
    // is counted as lost and logged.
    recorder.finish(QUEUE_DRAINS_WITHIN).await;
    stopped
}

/// How long `serve` waits at shutdown for the denials still queued to
/// be recorded, so a stop is not held by a database that does not
/// answer.
const QUEUE_DRAINS_WITHIN: Duration = Duration::from_secs(5);

/// How often the denials past their retention are removed. The read
/// answers none of them in between (ADR-0148 item 2).
const SWEEP_EVERY: Duration = Duration::from_secs(60 * 60);

/// Removes the denials past their retention at start and then every
/// [`SWEEP_EVERY`], and never returns. A failed sweep is logged and
/// tried again at the next one: the read does not depend on it.
async fn sweeping(recorder: Recorder) {
    let mut tick = tokio::time::interval(SWEEP_EVERY);
    loop {
        tick.tick().await;
        match recorder.sweep().await {
            Ok(removed) => tracing::info!(removed, "swept the denials past their retention"),
            Err(error) => tracing::error!(error = ?error, "the denial sweep failed"),
        }
    }
}
