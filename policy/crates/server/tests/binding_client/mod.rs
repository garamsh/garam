//! An HTTP/1.1 client over TLS that offers `http/1.1` alone, as a
//! client with no HTTP/2 does, against a listener `serve` started.

#![allow(dead_code)]

use std::net::SocketAddr;
use std::sync::Arc;

use rustls::ClientConfig;
use rustls::RootCertStore;
use rustls::crypto::ring::default_provider;
use rustls::pki_types::pem::PemObject;
use rustls::pki_types::{CertificateDer, ServerName};
use serde_json::Value;
use server::{Verifier, serve};
use sqlx::PgPool;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use tokio_rustls::TlsConnector;

use crate::caller_tokens::{Claims, ISSUER, claims, jwks, signer};
use crate::listening::{Pair, bound};

pub struct Reply {
    pub status: u16,
    pub headers: Vec<(String, String)>,
    pub body: String,
}

impl Reply {
    pub fn header(&self, name: &str) -> Option<&str> {
        self.headers
            .iter()
            .find(|(key, _)| key.eq_ignore_ascii_case(name))
            .map(|(_, value)| value.as_str())
    }

    pub fn json(&self) -> Value {
        serde_json::from_str(&self.body).unwrap_or_else(|_| panic!("not JSON: {}", self.body))
    }
}

pub struct Listening {
    pub cert: String,
    pub address: SocketAddr,
}

/// The listener `serve` opens over `pool`, and the pair it presents.
pub async fn listening(pool: PgPool) -> Listening {
    let pair = Pair::new();
    let bound = bound().await;
    let verifier = Verifier::from_jwks(&jwks(&[signer()]), ISSUER).unwrap();
    tokio::spawn(serve(
        pool,
        verifier,
        pair.identity(),
        bound.listeners,
        std::future::pending(),
    ));
    Listening {
        cert: pair.cert,
        address: bound.contract,
    }
}

/// A token of the audience this service verifies, for `sub`.
pub fn token_of(sub: &str) -> String {
    signer().sign(&Claims {
        sub: sub.to_owned(),
        ..claims()
    })
}

impl Listening {
    /// One request, answered and closed. `token` is sent as a bearer.
    pub async fn call(
        &self,
        method: &str,
        path: &str,
        token: Option<&str>,
        headers: &[(&str, &str)],
        body: Option<&Value>,
    ) -> Reply {
        let mut roots = RootCertStore::empty();
        roots
            .add(CertificateDer::from_pem_slice(self.cert.as_bytes()).unwrap())
            .unwrap();
        let mut config = ClientConfig::builder_with_provider(Arc::new(default_provider()))
            .with_safe_default_protocol_versions()
            .unwrap()
            .with_root_certificates(roots)
            .with_no_client_auth();
        config.alpn_protocols = vec![b"http/1.1".to_vec()];
        let tcp = TcpStream::connect(self.address).await.unwrap();
        let mut stream = TlsConnector::from(Arc::new(config))
            .connect(ServerName::try_from("localhost").unwrap(), tcp)
            .await
            .unwrap();
        // The setup is what the test asks for: HTTP/1.1 was negotiated.
        assert_eq!(stream.get_ref().1.alpn_protocol(), Some(&b"http/1.1"[..]));

        let payload = body.map(Value::to_string).unwrap_or_default();
        let mut request = format!(
            "{method} {path} HTTP/1.1\r\nHost: localhost:{}\r\nConnection: close\r\n",
            self.address.port()
        );
        if let Some(token) = token {
            request.push_str(&format!("Authorization: Bearer {token}\r\n"));
        }
        for (name, value) in headers {
            request.push_str(&format!("{name}: {value}\r\n"));
        }
        if body.is_some() {
            request.push_str("Content-Type: application/json\r\n");
        }
        request.push_str(&format!(
            "Content-Length: {}\r\n\r\n{payload}",
            payload.len()
        ));
        stream.write_all(request.as_bytes()).await.unwrap();

        let mut raw = Vec::new();
        // A close without close_notify ends the read with an error
        // after the answer arrived.
        let _ = stream.read_to_end(&mut raw).await;
        parse(&String::from_utf8(raw).unwrap())
    }
}

fn parse(raw: &str) -> Reply {
    let (head, body) = raw.split_once("\r\n\r\n").expect("a complete answer");
    let mut lines = head.lines();
    let status = lines
        .next()
        .and_then(|line| line.split_whitespace().nth(1))
        .and_then(|code| code.parse().ok())
        .expect("a status line");
    let headers = lines
        .filter_map(|line| line.split_once(':'))
        .map(|(name, value)| (name.trim().to_owned(), value.trim().to_owned()))
        .collect();
    Reply {
        status,
        headers,
        body: body.to_owned(),
    }
}
