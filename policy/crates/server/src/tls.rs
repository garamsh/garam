use std::fs;
use std::io;
use std::os::unix::fs::MetadataExt;
use std::path::Path;
use std::pin::Pin;
use std::sync::Arc;
use std::task::{Context, Poll};
use std::time::Duration;

use rustls::ServerConfig;
use rustls::crypto::ring::default_provider;
use rustls::pki_types::pem::PemObject;
use rustls::pki_types::{CertificateDer, PrivateKeyDer};
use tokio::net::{TcpListener, TcpStream};
use tokio::sync::mpsc;
use tokio::task::{JoinHandle, JoinSet};
use tokio_rustls::TlsAcceptor;
use tokio_rustls::server::TlsStream;
use tokio_stream::Stream;

/// How long a handshake may take before its connection is dropped.
const HANDSHAKE_TIMEOUT: Duration = Duration::from_secs(10);
/// The protocols the listener negotiates: the gRPC contract's and the
/// AuthZEN binding's.
const ALPN: [&[u8]; 2] = [b"h2", b"http/1.1"];

/// Why the listener's certificate and key do not load.
#[derive(Debug, thiserror::Error)]
pub enum TlsError {
    /// A file cannot be read.
    #[error("{path} cannot be read")]
    Unreadable {
        /// The file.
        path: String,
        /// Why.
        #[source]
        source: std::io::Error,
    },
    /// The key file is open to a group or to others.
    #[error("{0} is readable beyond its owner")]
    KeyExposed(String),
    /// The key file belongs to another user.
    #[error("{0} is not owned by this process's user")]
    KeyForeign(String),
    /// The certificate or the key is not PEM, or the key is not the
    /// certificate's.
    #[error("the certificate and key do not form a listener's identity")]
    Identity(#[source] Box<dyn std::error::Error + Send + Sync>),
}

/// A listener's certificate chain and private key, in PEM.
#[derive(Clone)]
pub struct Identity {
    cert: Vec<u8>,
    key: Vec<u8>,
}

impl std::fmt::Debug for Identity {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        formatter.debug_struct("Identity").finish_non_exhaustive()
    }
}

impl Identity {
    /// Holds a pair, read as PEM when the listener starts.
    pub fn from_pem(cert: impl AsRef<[u8]>, key: impl AsRef<[u8]>) -> Self {
        Self {
            cert: cert.as_ref().to_vec(),
            key: key.as_ref().to_vec(),
        }
    }

    /// Negotiates `h2` and `http/1.1`: the gRPC contract and the
    /// AuthZEN binding share the listener, and a client offering only
    /// `http/1.1` would otherwise be refused at the handshake.
    pub(crate) fn acceptor(&self) -> Result<TlsAcceptor, TlsError> {
        let chain = CertificateDer::pem_slice_iter(&self.cert)
            .collect::<Result<Vec<_>, _>>()
            .map_err(identity)?;
        let key = PrivateKeyDer::from_pem_slice(&self.key).map_err(identity)?;
        let mut config = ServerConfig::builder_with_provider(Arc::new(default_provider()))
            .with_safe_default_protocol_versions()
            .map_err(identity)?
            .with_no_client_auth()
            .with_single_cert(chain, key)
            .map_err(identity)?;
        config.alpn_protocols = ALPN.iter().map(|protocol| protocol.to_vec()).collect();
        Ok(TlsAcceptor::from(Arc::new(config)))
    }
}

fn identity(error: impl std::error::Error + Send + Sync + 'static) -> TlsError {
    TlsError::Identity(Box::new(error))
}

/// The connections that finished a handshake. Each handshake runs in
/// its own task, so one stalled client holds up no other.
pub(crate) struct Incoming {
    connections: mpsc::Receiver<TlsStream<TcpStream>>,
    accepting: JoinHandle<()>,
}

impl Incoming {
    /// Accepts on `listener` until dropped.
    pub(crate) fn new(listener: TcpListener, acceptor: TlsAcceptor) -> Self {
        let (sender, connections) = mpsc::channel(64);
        Self {
            connections,
            accepting: tokio::spawn(accept(listener, acceptor, sender)),
        }
    }
}

impl Drop for Incoming {
    fn drop(&mut self) {
        self.accepting.abort();
    }
}

impl Stream for Incoming {
    type Item = io::Result<TlsStream<TcpStream>>;

    fn poll_next(mut self: Pin<&mut Self>, cx: &mut Context<'_>) -> Poll<Option<Self::Item>> {
        self.connections.poll_recv(cx).map(|stream| stream.map(Ok))
    }
}

async fn accept(
    listener: TcpListener,
    acceptor: TlsAcceptor,
    sender: mpsc::Sender<TlsStream<TcpStream>>,
) {
    let mut handshakes = JoinSet::new();
    loop {
        tokio::select! {
            accepted = listener.accept() => match accepted {
                Ok((stream, _)) => {
                    let (acceptor, sender) = (acceptor.clone(), sender.clone());
                    handshakes.spawn(async move {
                        match tokio::time::timeout(HANDSHAKE_TIMEOUT, acceptor.accept(stream)).await {
                            Ok(Ok(stream)) => {
                                let _ = sender.send(stream).await;
                            }
                            Ok(Err(error)) => tracing::debug!(%error, "tls handshake failed"),
                            Err(_) => tracing::debug!("tls handshake timed out"),
                        }
                    });
                }
                Err(error) => {
                    tracing::debug!(%error, "accept failed");
                    tokio::time::sleep(Duration::from_millis(50)).await;
                }
            },
            Some(_) = handshakes.join_next() => {}
        }
    }
}

/// The listener's own pair, read at start: `POLICY_TLS_CERT_FILE` and
/// `POLICY_TLS_KEY_FILE`. The key file is readable by its owner alone,
/// and its owner is this process's user, as the machine listener
/// requires of its own.
pub fn load_identity(cert_file: &Path, key_file: &Path) -> Result<Identity, TlsError> {
    let unreadable = |path: &Path| {
        let path = path.display().to_string();
        move |source| TlsError::Unreadable { path, source }
    };
    let metadata = fs::metadata(key_file).map_err(unreadable(key_file))?;
    owner_only(
        key_file,
        metadata.mode(),
        metadata.uid(),
        rustix::process::geteuid().as_raw(),
    )?;
    let key = fs::read(key_file).map_err(unreadable(key_file))?;
    let cert = fs::read(cert_file).map_err(unreadable(cert_file))?;
    Ok(Identity::from_pem(cert, key))
}

fn owner_only(path: &Path, mode: u32, owner: u32, reader: u32) -> Result<(), TlsError> {
    if mode & 0o077 != 0 {
        return Err(TlsError::KeyExposed(path.display().to_string()));
    }
    if owner != reader {
        return Err(TlsError::KeyForeign(path.display().to_string()));
    }
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::{TlsError, owner_only};
    use std::path::Path;

    const KEY: &str = "tls.key";

    #[test]
    fn a_key_open_to_its_group_or_to_others_is_refused() {
        for mode in [0o100640, 0o100604, 0o100660] {
            let refused = owner_only(Path::new(KEY), mode, 10001, 10001);
            assert!(matches!(refused, Err(TlsError::KeyExposed(_))), "{mode:o}");
        }
        assert!(owner_only(Path::new(KEY), 0o100600, 10001, 10001).is_ok());
    }

    #[test]
    fn a_key_owned_by_another_user_is_refused() {
        let refused = owner_only(Path::new(KEY), 0o100600, 0, 10001);
        assert!(matches!(refused, Err(TlsError::KeyForeign(_))));
        assert!(owner_only(Path::new(KEY), 0o100400, 10001, 10001).is_ok());
    }
}
