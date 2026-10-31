use std::fs;
use std::os::unix::fs::MetadataExt;
use std::path::Path;

use tonic::transport::Identity;

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
