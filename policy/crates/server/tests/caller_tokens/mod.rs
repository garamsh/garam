//! Tokens as the platform's issuer will sign them (ADR-0138), with a
//! key each test run generates.

#![allow(dead_code)]

use std::sync::OnceLock;
use std::time::{SystemTime, UNIX_EPOCH};

use jsonwebtoken::jwk::{Jwk, JwkSet};
use jsonwebtoken::{Algorithm, EncodingKey, Header, encode};
use rsa::pkcs1::{EncodeRsaPrivateKey, EncodeRsaPublicKey, LineEnding};
use rsa::rand_core::OsRng;
use rsa::{RsaPrivateKey, RsaPublicKey};
use serde::Serialize;

pub const ISSUER: &str = "https://garam.test";
pub const KID: &str = "current";
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

    fn jwk(&self) -> Jwk {
        let mut jwk = Jwk::from_encoding_key(&self.key, Algorithm::RS256).unwrap();
        jwk.common.key_id = Some(self.kid.clone());
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

/// `jwks` with a member taken off its first key.
pub fn without(jwks: &str, member: &str) -> String {
    let mut parsed: serde_json::Value = serde_json::from_str(jwks).unwrap();
    parsed["keys"][0].as_object_mut().unwrap().remove(member);
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
