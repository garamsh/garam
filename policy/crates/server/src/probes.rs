use std::future::IntoFuture;
use std::time::Duration;

use audit::{Counts, Recorder};
use axum::Router;
use axum::extract::State;
use axum::http::StatusCode;
use axum::http::header::CONTENT_TYPE;
use axum::routing::get;
use sqlx::PgPool;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::{TcpListener, TcpStream};

/// How long a readiness check waits on the database.
const READY_WITHIN: Duration = Duration::from_secs(2);

/// Answers `/healthz`, which reads nothing outside the process,
/// `/readyz`, which answers while the database does, and `/metrics`,
/// the recorder's counts in the Prometheus text format. A credential
/// rides none of them, and `/metrics` holds counts and no name, so
/// this port needs no TLS.
pub(crate) async fn answer(
    listener: TcpListener,
    pool: PgPool,
    recorder: Recorder,
) -> Result<(), std::io::Error> {
    let routes = Router::new()
        .route("/healthz", get(|| async { StatusCode::OK }))
        .route("/readyz", get(ready).with_state(pool))
        .route(
            "/metrics",
            get(|| async move { ([(CONTENT_TYPE, METRICS_TYPE)], render(recorder.counts())) }),
        );
    axum::serve(listener, routes).into_future().await
}

/// The Prometheus text format's media type.
const METRICS_TYPE: &str = "text/plain; version=0.0.4; charset=utf-8";

/// The counts as `/metrics` answers them. Each name carries the
/// service, as the platform's do (`garam_policy_feed_outbox_marks`).
fn render(counts: Counts) -> String {
    let counters = [
        (
            "garam_policy_denials_lost_total",
            "Denials not recorded: the record failed or passed its bound, the queue was full, or the service stopped with them queued.",
            counts.lost,
        ),
        (
            "garam_policy_denials_omitted_total",
            "Denials left unrecorded: no organization the service holds, or a field past its bound.",
            counts.omitted,
        ),
        (
            "garam_policy_denials_overflowed_total",
            "Denials not stored because their organization was at its cap, and counted in its overflow record.",
            counts.overflowed,
        ),
        (
            "garam_policy_denial_queue_full_total",
            "Denials offered to a full queue, a part of the lost ones.",
            counts.queue_full,
        ),
    ];
    let mut text = String::new();
    for (name, help, value) in counters {
        text.push_str(&format!(
            "# HELP {name} {help}\n# TYPE {name} counter\n{name} {value}\n"
        ));
    }
    text.push_str(&format!(
        "# HELP garam_policy_denial_queue_depth Denials waiting for their record.\n\
         # TYPE garam_policy_denial_queue_depth gauge\n\
         garam_policy_denial_queue_depth {}\n",
        counts.queued
    ));
    text
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

#[cfg(test)]
mod tests {
    use std::time::Duration;

    use audit::{Denial, Noted};
    use sqlx::postgres::PgPoolOptions;

    use super::*;

    async fn body_of(address: &str, path: &str) -> String {
        let mut stream = TcpStream::connect(address).await.unwrap();
        let request = format!("GET {path} HTTP/1.0\r\nHost: {address}\r\n\r\n");
        stream.write_all(request.as_bytes()).await.unwrap();
        let mut response = String::new();
        stream.read_to_string(&mut response).await.unwrap();
        response
    }

    #[tokio::test]
    async fn metrics_read_the_counts_of_the_denials_the_recorder_could_not_write() {
        // A pool pointing at nothing: every record fails at its bound.
        let pool = PgPoolOptions::new()
            .connect_lazy("postgres://nobody@127.0.0.1:1/none?sslmode=disable")
            .unwrap();
        let recorder = Recorder::new(pool.clone()).bounded(Duration::from_millis(50));
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let address = listener.local_addr().unwrap().to_string();
        tokio::spawn(answer(listener, pool, recorder.clone()));

        let before = body_of(&address, "/metrics").await;
        assert!(before.starts_with("HTTP/1.0 200"), "{before}");
        assert!(
            before.contains("garam_policy_denials_lost_total 0\n"),
            "{before}"
        );

        let denial = Denial {
            service: "platform",
            subject: "grn:acme/iam/user/lee",
            action: "iam:agent.read",
            resource: "grn:acme/iam/agent/bot",
            policies: &[],
        };
        assert_eq!(recorder.note(&denial), Noted::Queued);
        assert!(recorder.settled(Duration::from_secs(10)).await);

        let after = body_of(&address, "/metrics").await;
        assert!(
            after.contains("garam_policy_denials_lost_total 1\n"),
            "{after}"
        );
        assert!(after.contains("# TYPE garam_policy_denials_lost_total counter\n"));
        assert!(after.contains("garam_policy_denials_omitted_total 0\n"));
        assert!(after.contains("garam_policy_denials_overflowed_total 0\n"));
        assert!(after.contains("garam_policy_denial_queue_full_total 0\n"));
        assert!(after.contains("garam_policy_denial_queue_depth 0\n"));
    }
}
