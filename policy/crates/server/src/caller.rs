use std::collections::HashMap;
use std::path::Path;

use base64::Engine as _;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use jsonwebtoken::{
    Algorithm, DecodingKey, Validation, decode, decode_header, get_current_timestamp,
};
use serde::Deserialize;
use tonic::service::Interceptor;
use tonic::{Request, Status};

use crate::status::{ServiceError, to_status};

/// The audience every caller's token names (ADR-0138).
const AUDIENCE: &str = "policy";
/// Keys one issuer may hold at once: the current one and its successor.
const MAX_KEYS: usize = 2;
/// The longest lifetime a token may state, `exp` less `iat`.
const MAX_LIFETIME_SECONDS: i64 = 300;
/// The RSA modulus sizes ADR-0102 accepts, in bits.
const MODULUS_BITS: std::ops::RangeInclusive<usize> = 2048..=4096;
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
    /// The file is not a JWKS of public signing keys. A member none
    /// is expected to carry, a private one above all, refuses the file.
    #[error("the JWKS file does not parse")]
    Parse(#[source] serde_json::Error),
    /// No issuer is configured, so no token could name it.
    #[error("no issuer is configured")]
    NoIssuer,
    /// No key would verify any token.
    #[error("the JWKS file holds no key")]
    Empty,
    /// More keys than a rotation needs.
    #[error("the JWKS file holds {0} keys, and an issuer has at most {MAX_KEYS}")]
    TooMany(usize),
    /// Two keys share a `kid`.
    #[error("two keys name kid {0}")]
    DuplicateKeyId(String),
    /// A key that is not an RSA signing key for RS256.
    #[error("key {0} is not an RS256 signing key")]
    NotRs256(String),
    /// A modulus outside the profile's sizes.
    #[error("key {kid} holds a {bits}-bit modulus")]
    ModulusSize {
        /// The key.
        kid: String,
        /// Its size.
        bits: usize,
    },
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
    /// The token names no configured key.
    #[error("the token names no configured key")]
    UnknownKey,
    /// Signature, algorithm, issuer, audience or dates failed.
    #[error("the token does not verify")]
    Invalid(#[source] jsonwebtoken::errors::Error),
    /// `exp` less `iat` is not above zero and within ADR-0138's bound.
    #[error("the token does not state a lifetime up to {MAX_LIFETIME_SECONDS} seconds")]
    Lifetime,
    /// The protected header is not exactly ADR-0102's: `alg`, `kid`
    /// and `typ` once each, `typ` being `JWT`.
    #[error("the token's protected header is outside the profile")]
    Header,
    /// `sub` names no one, or `nbf` falls after `exp`.
    #[error("the token's claims are outside the profile")]
    Claims,
    /// `iat` is ahead of this clock by more than the skew, so its
    /// stated lifetime says nothing about when the token ends.
    #[error("the token is issued in the future")]
    IssuedInTheFuture,
}

/// A key as the file states it. Every member is named, so a private
/// member is refused rather than ignored.
#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
struct PublicKey {
    kty: String,
    #[serde(rename = "use")]
    usage: String,
    alg: String,
    kid: String,
    n: String,
    e: String,
}

#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
struct KeySet {
    keys: Vec<PublicKey>,
}

/// The protected header. `deny_unknown_fields` is what refuses `crit`,
/// `jwk`, `jku` and `x5u`, and derived parsing refuses a duplicate.
#[derive(Debug, Deserialize)]
#[serde(deny_unknown_fields)]
struct ProtectedHeader {
    #[allow(dead_code)]
    alg: String,
    kid: String,
    typ: String,
}

/// `aud` and `iss` are strings here so an array is refused: the
/// library alone would accept `aud: ["policy", "other"]`. Other claims
/// convey no authorization and are ignored.
#[derive(Debug, Deserialize)]
struct Claims {
    #[allow(dead_code)]
    iss: String,
    #[allow(dead_code)]
    aud: String,
    sub: String,
    iat: i64,
    exp: i64,
    nbf: Option<i64>,
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

    /// Parses a JWKS under ADR-0102's profile: one or two public
    /// RS256 signing keys of 2048 to 4096 bits, each with a `kid` of
    /// its own.
    pub fn from_jwks(jwks: &str, issuer: &str) -> Result<Self, KeysError> {
        if issuer.is_empty() {
            return Err(KeysError::NoIssuer);
        }
        let set: KeySet = serde_json::from_str(jwks).map_err(KeysError::Parse)?;
        if set.keys.is_empty() {
            return Err(KeysError::Empty);
        }
        if set.keys.len() > MAX_KEYS {
            return Err(KeysError::TooMany(set.keys.len()));
        }
        let mut keys = HashMap::new();
        for jwk in set.keys {
            let key = decoding_key(&jwk)?;
            if keys.insert(jwk.kid.clone(), key).is_some() {
                return Err(KeysError::DuplicateKeyId(jwk.kid));
            }
        }
        let mut validation = Validation::new(Algorithm::RS256);
        validation.set_audience(&[AUDIENCE]);
        validation.set_issuer(&[issuer]);
        validation.set_required_spec_claims(&["exp", "iat", "iss", "aud", "sub"]);
        validation.validate_nbf = true;
        validation.leeway = CLOCK_SKEW_SECONDS;
        Ok(Self { keys, validation })
    }

    /// Verifies a compact token.
    pub fn verify(&self, token: &str) -> Result<Caller, Refusal> {
        let header = protected_header(token)?;
        if header.typ != "JWT" {
            return Err(Refusal::Header);
        }
        let key = self.keys.get(&header.kid).ok_or(Refusal::UnknownKey)?;
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
        if claims.sub.is_empty() || claims.nbf.is_some_and(|nbf| nbf > claims.exp) {
            return Err(Refusal::Claims);
        }
        Ok(Caller {
            subject: claims.sub,
        })
    }
}

fn decoding_key(jwk: &PublicKey) -> Result<DecodingKey, KeysError> {
    if jwk.kty != "RSA" || jwk.usage != "sig" || jwk.alg != "RS256" {
        return Err(KeysError::NotRs256(jwk.kid.clone()));
    }
    let bits = modulus_bits(&jwk.n).ok_or_else(|| KeysError::NotRs256(jwk.kid.clone()))?;
    if !MODULUS_BITS.contains(&bits) {
        return Err(KeysError::ModulusSize {
            kid: jwk.kid.clone(),
            bits,
        });
    }
    DecodingKey::from_rsa_components(&jwk.n, &jwk.e).map_err(|source| KeysError::Material {
        kid: jwk.kid.clone(),
        source,
    })
}

/// The modulus's size, its leading zero bits not counted.
fn modulus_bits(n: &str) -> Option<usize> {
    let bytes = URL_SAFE_NO_PAD.decode(n).ok()?;
    let first = bytes.iter().position(|byte| *byte != 0)?;
    let leading = usize::try_from(bytes[first].leading_zeros()).ok()?;
    Some((bytes.len() - first) * 8 - leading)
}

fn protected_header(token: &str) -> Result<ProtectedHeader, Refusal> {
    decode_header(token).map_err(|_| Refusal::Header)?;
    let segment = token.split('.').next().ok_or(Refusal::Header)?;
    let bytes = URL_SAFE_NO_PAD
        .decode(segment)
        .map_err(|_| Refusal::Header)?;
    serde_json::from_slice(&bytes).map_err(|_| Refusal::Header)
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
            Err(refusal) => Err(to_status(&ServiceError::Unauthenticated(refusal))),
        }
    }
}
