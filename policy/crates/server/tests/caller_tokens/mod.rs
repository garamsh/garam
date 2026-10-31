//! Tokens as the platform's issuer will sign them (ADR-0138), with a
//! key each test run generates.

#![allow(dead_code)]

use std::sync::OnceLock;
use std::time::{SystemTime, UNIX_EPOCH};

use base64::Engine as _;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use jsonwebtoken::jwk::{Jwk, JwkSet, PublicKeyUse};
use jsonwebtoken::{Algorithm, EncodingKey, Header, encode};
use rsa::pkcs1::{EncodeRsaPrivateKey, EncodeRsaPublicKey, LineEnding};
use rsa::rand_core::OsRng;
use rsa::{RsaPrivateKey, RsaPublicKey};
use serde::Serialize;

pub const ISSUER: &str = "https://garam.test";
pub const KID: &str = "current";
pub const HEADER: &str = r#"{"alg":"RS256","kid":"current","typ":"JWT"}"#;
pub const CALLER: &str = "grn:root:default:service:platform";

pub struct Signer {
    kid: String,
    key: EncodingKey,
    public: String,
}

impl Signer {
    pub fn new(kid: &str) -> Self {
        let private = RsaPrivateKey::new(&mut OsRng, 2048).unwrap();
        let pem = private.to_pkcs1_pem(LineEnding::LF).unwrap();
        let public = RsaPublicKey::from(&private)
            .to_pkcs1_pem(LineEnding::LF)
            .unwrap();
        Self {
            kid: kid.to_owned(),
            key: EncodingKey::from_rsa_pem(pem.as_bytes()).unwrap(),
            public,
        }
    }

    pub fn public_pem(&self) -> &str {
        &self.public
    }

    pub fn sign(&self, claims: &Claims) -> String {
        self.sign_as(&self.kid, claims)
    }

    pub fn sign_as(&self, kid: &str, claims: &Claims) -> String {
        let mut header = Header::new(Algorithm::RS256);
        header.kid = Some(kid.to_owned());
        encode(&header, claims, &self.key).unwrap()
    }

    /// A token over exactly these bytes, for what no well-behaved
    /// encoder writes.
    pub fn sign_raw(&self, header: &str, payload: &str) -> String {
        let message = format!(
            "{}.{}",
            URL_SAFE_NO_PAD.encode(header),
            URL_SAFE_NO_PAD.encode(payload)
        );
        let signature =
            jsonwebtoken::crypto::sign(message.as_bytes(), &self.key, Algorithm::RS256).unwrap();
        format!("{message}.{signature}")
    }

    fn jwk(&self) -> Jwk {
        let mut jwk = Jwk::from_encoding_key(&self.key, Algorithm::RS256).unwrap();
        jwk.common.key_id = Some(self.kid.clone());
        jwk.common.public_key_use = Some(PublicKeyUse::Signature);
        jwk
    }
}

/// One key per test binary: generating one takes seconds unoptimised.
pub fn signer() -> &'static Signer {
    static SIGNER: OnceLock<Signer> = OnceLock::new();
    SIGNER.get_or_init(|| Signer::new(KID))
}

pub fn jwks(signers: &[&Signer]) -> String {
    let keys = signers.iter().map(|signer| signer.jwk()).collect();
    serde_json::to_string(&JwkSet { keys }).unwrap()
}

/// `jwks` with a member of its first key set, or taken off by `Null`.
pub fn with(jwks: &str, member: &str, value: serde_json::Value) -> String {
    let mut parsed: serde_json::Value = serde_json::from_str(jwks).unwrap();
    let key = parsed["keys"][0].as_object_mut().unwrap();
    if value.is_null() {
        key.remove(member);
    } else {
        key.insert(member.to_owned(), value);
    }
    parsed.to_string()
}

#[derive(Debug, Clone, Serialize)]
pub struct Claims {
    pub iss: String,
    pub aud: String,
    pub sub: String,
    pub iat: i64,
    pub exp: i64,
}

pub fn now() -> i64 {
    let seconds = SystemTime::now().duration_since(UNIX_EPOCH).unwrap();
    i64::try_from(seconds.as_secs()).unwrap()
}

pub fn claims() -> Claims {
    let now = now();
    Claims {
        iss: ISSUER.to_owned(),
        aud: "policy".to_owned(),
        sub: CALLER.to_owned(),
        iat: now,
        exp: now + 300,
    }
}
