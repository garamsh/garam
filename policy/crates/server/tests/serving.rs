//! What `serve` listens on and what `policy serve` refuses to start
//! without. This suite reads no database: a refused token never reaches
//! a store, and the pool below points at nothing.

#![cfg(test)]

mod caller_tokens;
mod listening;

use std::fs;
use std::net::SocketAddr;
use std::os::unix::fs::PermissionsExt;
use std::path::{Path, PathBuf};
use std::process::{Command, Output};

use caller_tokens::*;
use listening::{Pair, bound, trusting};
use server::proto::policy_service_client::PolicyServiceClient;
use server::proto::{Action, EvaluateRequest, Resource, Subject};
use server::{Error, ProbeError, Verifier, probe, serve};
use sqlx::PgPool;
use sqlx::postgres::PgPoolOptions;
use tonic::Code;
use tonic::transport::Channel;

fn unreachable_pool() -> PgPool {
    PgPoolOptions::new()
        .connect_lazy("postgres://nobody@127.0.0.1:1/none?sslmode=disable")
        .unwrap()
}

fn verifier() -> Verifier {
    Verifier::from_jwks(&jwks(&[signer()]), ISSUER).unwrap()
}

fn question() -> EvaluateRequest {
    EvaluateRequest {
        subject: Some(Subject {
            r#type: "User".into(),
            id: "grn:acme:default:user:lee".into(),
        }),
        action: Some(Action {
            name: "agent:read".into(),
        }),
        resource: Some(Resource {
            r#type: "Agent".into(),
            id: "grn:acme:default:agent:bot".into(),
            properties: None,
        }),
        context: None,
        read: 0,
    }
}

/// `serve` over a fresh pair, with the database out of reach.
async fn serving() -> (Pair, SocketAddr, SocketAddr) {
    let pair = Pair::new();
    let bound = bound().await;
    tokio::spawn(serve(
        unreachable_pool(),
        verifier(),
        pair.identity(),
        bound.listeners,
        std::future::pending(),
    ));
    (pair, bound.contract, bound.probes)
}

#[tokio::test]
async fn the_contract_answers_over_tls_and_never_in_plaintext() {
    let (pair, contract, _) = serving().await;

    let plaintext = match Channel::from_shared(format!("http://{contract}"))
        .unwrap()
        .connect()
        .await
    {
        Ok(channel) => PolicyServiceClient::new(channel).evaluate(question()).await,
        Err(error) => Err(tonic::Status::unavailable(error.to_string())),
    };
    assert_ne!(plaintext.unwrap_err().code(), Code::Unauthenticated);

    let mut over_tls = PolicyServiceClient::new(trusting(&pair.cert, contract).await);
    let answered = over_tls.evaluate(question()).await;
    assert_eq!(answered.unwrap_err().code(), Code::Unauthenticated);
}

#[tokio::test]
async fn a_key_that_is_not_the_certificates_stops_the_service() {
    let certificate = Pair::new();
    let another = Pair::new();
    let bound = bound().await;

    let stopped = serve(
        unreachable_pool(),
        verifier(),
        tonic::transport::Identity::from_pem(&certificate.cert, &another.key),
        bound.listeners,
        std::future::pending(),
    )
    .await;

    assert!(matches!(stopped, Err(Error::Transport(_))), "{stopped:?}");
}

#[tokio::test]
async fn health_reads_nothing_outside_the_process_and_ready_reads_the_database() {
    let (_, _, probes) = serving().await;

    let health = probe(&format!("http://{probes}/healthz")).await;
    assert!(health.is_ok(), "{health:?}");

    let ready = probe(&format!("http://{probes}/readyz")).await;
    assert!(
        matches!(&ready, Err(ProbeError::Status { status, .. }) if status.contains("503")),
        "{ready:?}"
    );
}

#[tokio::test]
async fn a_probe_of_nothing_or_of_no_url_fails() {
    let unreachable = probe("http://127.0.0.1:1/healthz").await;
    assert!(matches!(unreachable, Err(ProbeError::Unreachable { .. })));

    let no_url = probe("https://localhost/healthz").await;
    assert!(matches!(no_url, Err(ProbeError::Url(_))));
}

/// A folder of this test's own, holding a JWKS and a pair.
fn files(test: &str) -> PathBuf {
    let folder = std::env::temp_dir().join(format!("policy-{test}-{}", std::process::id()));
    fs::create_dir_all(&folder).unwrap();
    let pair = Pair::new();
    fs::write(folder.join("jwks.json"), jwks(&[signer()])).unwrap();
    fs::write(folder.join("tls.crt"), pair.cert).unwrap();
    fs::write(folder.join("tls.key"), pair.key).unwrap();
    fs::set_permissions(folder.join("tls.key"), fs::Permissions::from_mode(0o600)).unwrap();
    folder
}

/// `policy serve` with every setting but `unset`, on a clean
/// environment.
fn policy_serve(folder: &Path, unset: &[&str]) -> Output {
    let path = |file: &str| folder.join(file).display().to_string();
    let settings = [
        ("POLICY_ADDR", "127.0.0.1:0".to_owned()),
        ("POLICY_HEALTH_ADDR", "127.0.0.1:0".to_owned()),
        (
            "POLICY_DATABASE_URL",
            "postgres://nobody@127.0.0.1:1/none?sslmode=none".to_owned(),
        ),
        ("POLICY_TOKEN_ISSUER", ISSUER.to_owned()),
        ("POLICY_JWKS_FILE", path("jwks.json")),
        ("POLICY_TLS_CERT_FILE", path("tls.crt")),
        ("POLICY_TLS_KEY_FILE", path("tls.key")),
    ];
    Command::new(env!("CARGO_BIN_EXE_policy"))
        .arg("serve")
        .env_clear()
        .envs(
            settings
                .into_iter()
                .filter(|(name, _)| !unset.contains(name)),
        )
        .output()
        .unwrap()
}

fn stderr(output: &Output) -> String {
    String::from_utf8_lossy(&output.stderr).into_owned()
}

#[test]
fn serve_refuses_to_start_without_either_half_of_the_pair() {
    let folder = files("unset");

    for name in ["POLICY_TLS_CERT_FILE", "POLICY_TLS_KEY_FILE"] {
        let refused = policy_serve(&folder, &[name]);
        assert!(!refused.status.success());
        assert!(stderr(&refused).contains(name), "{}", stderr(&refused));
    }

    // With the pair set it passes the pair and stops at the database,
    // whose URL names no sslmode there is.
    let control = policy_serve(&folder, &[]);
    assert!(
        stderr(&control).contains("Database"),
        "{}",
        stderr(&control)
    );
    fs::remove_dir_all(folder).unwrap();
}

#[test]
fn serve_refuses_a_key_file_others_can_read() {
    let folder = files("exposed");
    let key = folder.join("tls.key");
    fs::set_permissions(&key, fs::Permissions::from_mode(0o644)).unwrap();

    let refused = policy_serve(&folder, &[]);

    assert!(!refused.status.success());
    assert!(
        stderr(&refused).contains("KeyExposed"),
        "{}",
        stderr(&refused)
    );
    fs::remove_dir_all(folder).unwrap();
}
