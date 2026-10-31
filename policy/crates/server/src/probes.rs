use std::future::IntoFuture;
use std::time::Duration;

use axum::Router;
use axum::extract::State;
use axum::http::StatusCode;
use axum::routing::get;
use sqlx::PgPool;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::{TcpListener, TcpStream};

/// How long a readiness check waits on the database.
const READY_WITHIN: Duration = Duration::from_secs(2);

/// Answers `/healthz`, which reads nothing outside the process, and
/// `/readyz`, which answers while the database does. A credential
/// rides neither, so this port needs no TLS.
pub(crate) async fn answer(listener: TcpListener, pool: PgPool) -> Result<(), std::io::Error> {
    let routes = Router::new()
        .route("/healthz", get(|| async { StatusCode::OK }))
        .route("/readyz", get(ready))
        .with_state(pool);
    axum::serve(listener, routes).into_future().await
}

async fn ready(State(pool): State<PgPool>) -> StatusCode {
    let answered = tokio::time::timeout(READY_WITHIN, sqlx::query("SELECT 1").execute(&pool));
    match answered.await {
        Ok(Ok(_)) => return StatusCode::OK,
        Ok(Err(error)) => tracing::warn!(%error, "not ready"),
        Err(_) => tracing::warn!("not ready: the database did not answer in time"),
    }
    StatusCode::SERVICE_UNAVAILABLE
}

/// Why `policy probe` failed.
#[derive(Debug, thiserror::Error)]
pub enum ProbeError {
    /// The argument is no `http://<host:port>/<path>`.
    #[error("{0} is not an http://<host:port>/<path> URL")]
    Url(String),
    /// Nothing answered.
    #[error("{url} did not answer")]
    Unreachable {
        /// What was asked.
        url: String,
        /// Why.
        #[source]
        source: std::io::Error,
    },
    /// Something answered other than 200.
    #[error("{url} answered {status}")]
    Status {
        /// What was asked.
        url: String,
        /// The response's status line.
        status: String,
    },
}

/// Asks `url` and fails unless it answers 200. The image is scratch,
/// so a healthcheck has this and nothing else to run.
pub async fn probe(url: &str) -> Result<(), ProbeError> {
    let (address, path) = url
        .strip_prefix("http://")
        .and_then(|rest| rest.split_once('/'))
        .ok_or_else(|| ProbeError::Url(url.to_owned()))?;
    let unreachable = |source| ProbeError::Unreachable {
        url: url.to_owned(),
        source,
    };
    let mut stream = TcpStream::connect(address).await.map_err(unreachable)?;
    let request = format!("GET /{path} HTTP/1.0\r\nHost: {address}\r\n\r\n");
    stream
        .write_all(request.as_bytes())
        .await
        .map_err(unreachable)?;
    let mut response = String::new();
    stream
        .read_to_string(&mut response)
        .await
        .map_err(unreachable)?;
    let status = response.lines().next().unwrap_or_default();
    if status.split_whitespace().nth(1) == Some("200") {
        return Ok(());
    }
    Err(ProbeError::Status {
        url: url.to_owned(),
        status: status.to_owned(),
    })
}
