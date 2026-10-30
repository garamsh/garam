use std::collections::HashMap;
use std::path::Path;

use jsonwebtoken::jwk::{JwkSet, KeyAlgorithm};
use jsonwebtoken::{
    Algorithm, DecodingKey, Validation, decode, decode_header, get_current_timestamp,
};
use serde::Deserialize;
use tonic::service::Interceptor;
use tonic::{Request, Status};

/// The audience every caller's token names (ADR-0138).
const AUDIENCE: &str = "policy";
/// Keys one issuer may hold at once: the current one and its successor.
const MAX_KEYS: usize = 2;
/// The longest lifetime a token may state, `exp` less `iat`.
const MAX_LIFETIME_SECONDS: i64 = 300;
/// The skew allowed against the issuer's clock, as ADR-0102's.
const CLOCK_SKEW_SECONDS: u64 = 30;

/// An authenticated service.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Caller {
    /// The `sub` of its token.
    pub subject: String,
}

/// Why the configured keys do not load.
#[derive(Debug, thiserror::Error)]
pub enum KeysError {
    /// The file cannot be read.
    #[error("the JWKS file cannot be read")]
    Read(#[source] std::io::Error),
    /// The file is not a JWKS.
    #[error("the JWKS file does not parse")]
    Parse(#[source] serde_json::Error),
    /// No key would verify any token.
    #[error("the JWKS file holds no key")]
    Empty,
    /// More keys than a rotation needs.
    #[error("the JWKS file holds {0} keys, and an issuer has at most {MAX_KEYS}")]
    TooMany(usize),
    /// Lookup is by `kid`, so a key without one could not be reached.
    #[error("a key names no kid")]
    NoKeyId,
    /// Two keys share a `kid`.
    #[error("two keys name kid {0}")]
    DuplicateKeyId(String),
    /// A key not stated as RS256.
    #[error("key {0} is not an RS256 key")]
    NotRs256(String),
    /// A key's material does not parse.
    #[error("key {kid} does not parse")]
    Material {
        /// The key.
        kid: String,
        /// Why not.
        #[source]
        source: jsonwebtoken::errors::Error,
    },
}

/// Why a token is refused. The caller is told none of this.
#[derive(Debug, thiserror::Error)]
pub enum Refusal {
    /// No `authorization: Bearer` header.
    #[error("no bearer token")]
    NoToken,
    /// The token does not parse or names no key.
    #[error("the token names no configured key")]
    UnknownKey,
    /// Signature, algorithm, issuer, audience or dates failed.
    #[error("the token does not verify")]
    Invalid(#[source] jsonwebtoken::errors::Error),
    /// `exp` less `iat` is not above zero and within ADR-0138's bound.
    #[error("the token does not state a lifetime up to {MAX_LIFETIME_SECONDS} seconds")]
    Lifetime,
    /// `iat` is ahead of this clock by more than the skew, so its
    /// stated lifetime says nothing about when the token ends.
    #[error("the token is issued in the future")]
    IssuedInTheFuture,
}

#[derive(Debug, Deserialize)]
struct Claims {
    sub: String,
    iat: i64,
    exp: i64,
}

/// Verifies callers' tokens offline against configured keys
/// (ADR-0130 §10, ADR-0138).
#[derive(Clone)]
pub struct Verifier {
    keys: HashMap<String, DecodingKey>,
    validation: Validation,
}

impl std::fmt::Debug for Verifier {
    fn fmt(&self, formatter: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let mut kids: Vec<&String> = self.keys.keys().collect();
        kids.sort();
        formatter
            .debug_struct("Verifier")
            .field("keys", &kids)
            .finish()
    }
}

impl Verifier {
    /// Reads the issuer's JWKS file.
    pub fn load(path: &Path, issuer: &str) -> Result<Self, KeysError> {
        let jwks = std::fs::read_to_string(path).map_err(KeysError::Read)?;
        Self::from_jwks(&jwks, issuer)
    }

    /// Parses a JWKS, refusing anything but one or two RS256 keys each
    /// with a `kid` of its own.
    pub fn from_jwks(jwks: &str, issuer: &str) -> Result<Self, KeysError> {
        let set: JwkSet = serde_json::from_str(jwks).map_err(KeysError::Parse)?;
        if set.keys.is_empty() {
            return Err(KeysError::Empty);
        }
        if set.keys.len() > MAX_KEYS {
            return Err(KeysError::TooMany(set.keys.len()));
        }
        let mut keys = HashMap::new();
        for jwk in &set.keys {
            let kid = jwk.common.key_id.clone().ok_or(KeysError::NoKeyId)?;
            if jwk.common.key_algorithm != Some(KeyAlgorithm::RS256) {
                return Err(KeysError::NotRs256(kid));
            }
            let key = DecodingKey::from_jwk(jwk).map_err(|source| KeysError::Material {
                kid: kid.clone(),
                source,
            })?;
            if keys.insert(kid.clone(), key).is_some() {
                return Err(KeysError::DuplicateKeyId(kid));
            }
        }
        let mut validation = Validation::new(Algorithm::RS256);
        validation.set_audience(&[AUDIENCE]);
        validation.set_issuer(&[issuer]);
        validation.set_required_spec_claims(&["exp", "iat", "iss", "aud", "sub"]);
        validation.leeway = CLOCK_SKEW_SECONDS;
        Ok(Self { keys, validation })
    }

    /// Verifies a compact token.
    pub fn verify(&self, token: &str) -> Result<Caller, Refusal> {
        let header = decode_header(token).map_err(|_| Refusal::UnknownKey)?;
        let key = header
            .kid
            .and_then(|kid| self.keys.get(&kid))
            .ok_or(Refusal::UnknownKey)?;
        let claims = decode::<Claims>(token, key, &self.validation)
            .map_err(Refusal::Invalid)?
            .claims;
        if !(1..=MAX_LIFETIME_SECONDS).contains(&claims.exp.saturating_sub(claims.iat)) {
            return Err(Refusal::Lifetime);
        }
        let latest = get_current_timestamp().saturating_add(CLOCK_SKEW_SECONDS);
        if u64::try_from(claims.iat).is_ok_and(|iat| iat > latest) {
            return Err(Refusal::IssuedInTheFuture);
        }
        Ok(Caller {
            subject: claims.sub,
        })
    }
}

impl Interceptor for Verifier {
    fn call(&mut self, mut request: Request<()>) -> Result<Request<()>, Status> {
        let verified = request
            .metadata()
            .get("authorization")
            .and_then(|value| value.to_str().ok())
            .and_then(|value| value.strip_prefix("Bearer "))
            .ok_or(Refusal::NoToken)
            .and_then(|token| self.verify(token));
        match verified {
            Ok(caller) => {
                request.extensions_mut().insert(caller);
                Ok(request)
            }
            Err(refusal) => {
                tracing::info!(error = %refusal, "refused a caller");
                Err(Status::unauthenticated("the caller is not authenticated"))
            }
        }
    }
}
