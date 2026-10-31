//! The listeners `serve` takes and a client trusting what it presents,
//! with a certificate each test generates.

#![allow(dead_code)]

use std::net::SocketAddr;

use rcgen::{CertifiedKey, generate_simple_self_signed};
use server::Listeners;
use tokio::net::TcpListener;
use tonic::transport::{Certificate, Channel, ClientTlsConfig, Identity};

pub struct Pair {
    pub cert: String,
    pub key: String,
}

impl Pair {
    pub fn new() -> Self {
        let CertifiedKey { cert, signing_key } =
            generate_simple_self_signed(["localhost".to_owned()]).unwrap();
        Self {
            cert: cert.pem(),
            key: signing_key.serialize_pem(),
        }
    }

    pub fn identity(&self) -> Identity {
        Identity::from_pem(&self.cert, &self.key)
    }
}

pub struct Bound {
    pub listeners: Listeners,
    pub contract: SocketAddr,
    pub probes: SocketAddr,
}

pub async fn bound() -> Bound {
    let contract = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let probes = TcpListener::bind("127.0.0.1:0").await.unwrap();
    Bound {
        contract: contract.local_addr().unwrap(),
        probes: probes.local_addr().unwrap(),
        listeners: Listeners { contract, probes },
    }
}

/// A channel trusting `cert` alone, by the name the certificate holds.
pub async fn trusting(cert: &str, contract: SocketAddr) -> Channel {
    let tls = ClientTlsConfig::new()
        .ca_certificate(Certificate::from_pem(cert))
        .domain_name("localhost");
    Channel::from_shared(format!("https://localhost:{}", contract.port()))
        .unwrap()
        .tls_config(tls)
        .unwrap()
        .connect()
        .await
        .unwrap()
}
