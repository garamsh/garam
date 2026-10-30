use std::path::PathBuf;

/// A required setting that is not set.
#[derive(Debug, thiserror::Error)]
#[error("{0} is not set")]
pub struct ConfigError(&'static str);

/// What the binary reads from its environment.
#[derive(Debug, Clone)]
pub struct Config {
    /// `POLICY_ADDR`, the address to listen on.
    pub addr: String,
    /// `POLICY_DATABASE_URL`.
    pub database_url: String,
    /// `POLICY_TOKEN_ISSUER`, the `iss` a caller's token must name.
    pub token_issuer: String,
    /// `POLICY_JWKS_FILE`, the issuer's public keys.
    pub jwks_file: PathBuf,
}

impl Config {
    /// Reads the environment, refusing to start without any setting.
    pub fn from_env() -> Result<Self, ConfigError> {
        let required = |name: &'static str| std::env::var(name).map_err(|_| ConfigError(name));
        Ok(Self {
            addr: required("POLICY_ADDR")?,
            database_url: required("POLICY_DATABASE_URL")?,
            token_issuer: required("POLICY_TOKEN_ISSUER")?,
            jwks_file: required("POLICY_JWKS_FILE")?.into(),
        })
    }
}
