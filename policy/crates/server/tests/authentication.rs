//! A caller's token over a real gRPC connection, with the stores faked:
//! this suite reads no database.

#![cfg(test)]

mod caller_tokens;

use base64::Engine as _;
use base64::engine::general_purpose::URL_SAFE_NO_PAD;
use caller_tokens::*;
use cedar_policy::EntityUid;
use decision::{Cause, Engine, EntitySlice, EntitySource, OrgGrn, OrgPolicies, PolicySource};
use jsonwebtoken::{Algorithm, EncodingKey, Header, encode};
use rsa::RsaPrivateKey;
use rsa::pkcs1::{EncodeRsaPrivateKey, LineEnding};
use rsa::rand_core::OsRng;
use serde_json::{Value, json};
use server::proto::policy_service_client::PolicyServiceClient;
use server::proto::{Action, EvaluateRequest, Resource, Subject};
use server::{KeysError, Refusal, Service, Verifier, authenticated};
use tokio::net::TcpListener;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::{Channel, Server};
use tonic::{Code, Request};

struct Empty(Engine);

impl PolicySource for Empty {
    async fn active(&self, _: &OrgGrn) -> Result<OrgPolicies, Cause> {
        Ok(OrgPolicies::default())
    }
}

impl EntitySource for Empty {
    async fn slice(&self, _: &EntityUid, _: &EntityUid) -> Result<EntitySlice, Cause> {
        Ok(EntitySlice {
            entities: self.0.entities(json!([]))?,
            org: None,
        })
    }
}

async fn client() -> PolicyServiceClient<Channel> {
    let engine = Engine::load().unwrap();
    let service = Service::new(engine.clone(), Empty(engine.clone()), Empty(engine));
    let verifier = Verifier::from_jwks(&jwks(&[signer()]), ISSUER).unwrap();
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap();
    tokio::spawn(
        Server::builder()
            .add_service(authenticated(service, verifier))
            .serve_with_incoming(TcpListenerStream::new(listener)),
    );
    let channel = Channel::from_shared(format!("http://{address}"))
        .unwrap()
        .connect()
        .await
        .unwrap();
    PolicyServiceClient::new(channel)
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

async fn answer_with(authorization: Option<String>) -> Result<(), Code> {
    let mut request = Request::new(question());
    if let Some(value) = authorization {
        request
            .metadata_mut()
            .insert("authorization", value.parse().unwrap());
    }
    client()
        .await
        .evaluate(request)
        .await
        .map(|_| ())
        .map_err(|status| status.code())
}

async fn answer_to(token: String) -> Result<(), Code> {
    answer_with(Some(format!("Bearer {token}"))).await
}

#[tokio::test]
async fn a_token_the_issuer_signed_is_answered() {
    assert_eq!(answer_to(signer().sign(&claims())).await, Ok(()));
}

#[tokio::test]
async fn a_request_with_no_bearer_token_is_refused() {
    assert_eq!(answer_with(None).await, Err(Code::Unauthenticated));

    let token = signer().sign(&claims());
    assert_eq!(
        answer_with(Some(format!("Basic {token}"))).await,
        Err(Code::Unauthenticated)
    );
    assert_eq!(answer_with(Some(format!("Bearer {token}"))).await, Ok(()));
}

#[tokio::test]
async fn a_token_signed_by_another_key_under_a_known_kid_is_refused() {
    let forged = Signer::new("other").sign_as(KID, &claims());
    assert_eq!(answer_to(forged).await, Err(Code::Unauthenticated));

    assert_eq!(answer_to(signer().sign(&claims())).await, Ok(()));
}

#[tokio::test]
async fn a_token_naming_an_unknown_kid_is_refused() {
    let unknown = signer().sign_as("retired", &claims());
    assert_eq!(answer_to(unknown).await, Err(Code::Unauthenticated));

    assert_eq!(answer_to(signer().sign_as(KID, &claims())).await, Ok(()));
}

#[tokio::test]
async fn a_token_for_another_audience_is_refused() {
    let machine_listener = Claims {
        aud: "garam-machine".into(),
        ..claims()
    };
    assert_eq!(
        answer_to(signer().sign(&machine_listener)).await,
        Err(Code::Unauthenticated)
    );
}

#[tokio::test]
async fn a_token_from_another_issuer_is_refused() {
    let elsewhere = Claims {
        iss: "elsewhere".into(),
        ..claims()
    };
    assert_eq!(
        answer_to(signer().sign(&elsewhere)).await,
        Err(Code::Unauthenticated)
    );
}

#[tokio::test]
async fn an_expired_token_is_refused_past_the_skew() {
    let now = now();
    let past_skew = Claims {
        iat: now - 300,
        exp: now - 31,
        ..claims()
    };
    assert_eq!(
        answer_to(signer().sign(&past_skew)).await,
        Err(Code::Unauthenticated)
    );

    let inside_skew = Claims {
        iat: now - 300,
        exp: now - 10,
        ..claims()
    };
    assert_eq!(answer_to(signer().sign(&inside_skew)).await, Ok(()));
}

#[tokio::test]
async fn a_token_stating_more_than_five_minutes_is_refused() {
    let now = now();
    let too_long = Claims {
        iat: now,
        exp: now + 301,
        ..claims()
    };
    assert_eq!(
        answer_to(signer().sign(&too_long)).await,
        Err(Code::Unauthenticated)
    );

    let longest = Claims {
        iat: now,
        exp: now + 300,
        ..claims()
    };
    assert_eq!(answer_to(signer().sign(&longest)).await, Ok(()));
}

#[tokio::test]
async fn a_token_stating_no_lifetime_is_refused() {
    let now = now();
    let none = Claims {
        iat: now + 10,
        exp: now + 10,
        ..claims()
    };
    assert_eq!(
        answer_to(signer().sign(&none)).await,
        Err(Code::Unauthenticated)
    );
}

#[tokio::test]
async fn a_token_issued_in_the_future_is_refused_past_the_skew() {
    let now = now();
    let past_skew = Claims {
        iat: now + 3600,
        exp: now + 3660,
        ..claims()
    };
    assert_eq!(
        answer_to(signer().sign(&past_skew)).await,
        Err(Code::Unauthenticated)
    );

    let inside_skew = Claims {
        iat: now + 10,
        exp: now + 70,
        ..claims()
    };
    assert_eq!(answer_to(signer().sign(&inside_skew)).await, Ok(()));
}

#[tokio::test]
async fn a_token_signed_with_the_public_key_as_a_secret_is_refused() {
    let mut header = Header::new(Algorithm::HS256);
    header.kid = Some(KID.into());
    let secret = EncodingKey::from_secret(signer().public_pem().as_bytes());
    let forged = encode(&header, &claims(), &secret).unwrap();

    assert_eq!(answer_to(forged).await, Err(Code::Unauthenticated));
}

#[tokio::test]
async fn a_token_under_either_key_of_a_rotation_is_verified() {
    let (old, new) = (signer(), Signer::new("next"));
    let verifier = Verifier::from_jwks(&jwks(&[old, &new]), ISSUER).unwrap();

    assert!(verifier.verify(&old.sign(&claims())).is_ok());
    assert!(verifier.verify(&new.sign(&claims())).is_ok());
    assert!(
        verifier
            .verify(&Signer::new("next").sign(&claims()))
            .is_err()
    );
}

fn verifier() -> Verifier {
    Verifier::from_jwks(&jwks(&[signer()]), ISSUER).unwrap()
}

/// `claims()` as JSON, with `member` set.
fn payload(member: &str, value: Value) -> String {
    let mut claims = serde_json::to_value(claims()).unwrap();
    claims[member] = value;
    claims.to_string()
}

#[test]
fn a_protected_header_outside_the_profile_is_refused() {
    let body = payload("sub", json!(CALLER));
    let verifier = verifier();

    for header in [
        r#"{"alg":"RS256","kid":"current"}"#,
        r#"{"alg":"RS256","kid":"current","typ":"at+jwt"}"#,
        r#"{"alg":"RS256","kid":"current","typ":"JWT","crit":["x"],"x":1}"#,
        r#"{"alg":"RS256","kid":"current","typ":"JWT","jku":"https://x"}"#,
        r#"{"alg":"RS256","kid":"current","typ":"JWT","typ":"JWT"}"#,
    ] {
        let token = signer().sign_raw(header, &body);
        let refused = verifier.verify(&token);
        assert!(
            matches!(refused, Err(Refusal::Header)),
            "{header}: {refused:?}"
        );
    }

    let control = verifier.verify(&signer().sign_raw(HEADER, &body));
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn claims_outside_the_profile_are_refused() {
    let verifier = verifier();
    let now = now();
    let sign = |body: String| signer().sign_raw(HEADER, &body);

    for body in [
        payload("aud", json!(["policy", "other"])),
        payload("aud", json!(["policy"])),
        payload("iss", json!([ISSUER])),
        payload("sub", json!("")),
        payload("nbf", json!(now + 3600)),
        payload("nbf", json!(now + 301)),
        payload("exp", json!((now + 100) as f64 + 0.5)),
        payload("exp", json!((now + 100).to_string())),
        format!(
            r#"{{"iss":"{ISSUER}","aud":"other","aud":"policy","sub":"s","iat":{now},"exp":{}}}"#,
            now + 300
        ),
    ] {
        let refused = verifier.verify(&sign(body.clone()));
        assert!(refused.is_err(), "{body}: {refused:?}");
    }

    let control = verifier.verify(&sign(payload("nbf", json!(now + 10))));
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn a_key_outside_the_profile_stops_the_service_at_start() {
    let good = jwks(&[signer()]);
    let parsed = |jwks: &str| Verifier::from_jwks(jwks, ISSUER);

    assert!(parsed(&good).is_ok());
    assert!(matches!(
        Verifier::from_jwks(&good, ""),
        Err(KeysError::NoIssuer)
    ));
    for (member, value) in [
        ("kty", json!("oct")),
        ("kty", json!("EC")),
        ("use", json!("enc")),
        ("alg", json!("RS512")),
    ] {
        let refused = parsed(&with(&good, member, value));
        assert!(
            matches!(refused, Err(KeysError::NotRs256(_))),
            "{refused:?}"
        );
    }
    for (member, value) in [
        ("kid", Value::Null),
        ("use", Value::Null),
        ("alg", Value::Null),
        ("d", json!("AQAB")),
        ("x5u", json!("https://x")),
    ] {
        let refused = parsed(&with(&good, member, value));
        assert!(matches!(refused, Err(KeysError::Parse(_))), "{refused:?}");
    }

    let private = RsaPrivateKey::new(&mut OsRng, 1024).unwrap();
    let pem = private.to_pkcs1_pem(LineEnding::LF).unwrap();
    let weak = jsonwebtoken::jwk::Jwk::from_encoding_key(
        &EncodingKey::from_rsa_pem(pem.as_bytes()).unwrap(),
        Algorithm::RS256,
    )
    .unwrap();
    let weak: Value = serde_json::to_value(weak).unwrap();
    let refused = parsed(&with(&good, "n", weak["n"].clone()));
    assert!(
        matches!(refused, Err(KeysError::ModulusSize { bits: 1024, .. })),
        "{refused:?}"
    );

    let oversized = URL_SAFE_NO_PAD.encode(vec![0xff_u8; 513]);
    let refused = parsed(&with(&good, "n", json!(oversized)));
    assert!(
        matches!(refused, Err(KeysError::ModulusSize { bits: 4104, .. })),
        "{refused:?}"
    );
}

#[test]
fn keys_load_only_as_one_or_two_with_a_kid_each() {
    let (one, two, three) = (signer(), Signer::new("second"), Signer::new("third"));

    assert!(Verifier::from_jwks(&jwks(&[one]), ISSUER).is_ok());
    assert!(Verifier::from_jwks(&jwks(&[one, &two]), ISSUER).is_ok());

    let refused = Verifier::from_jwks(&jwks(&[one, &two, &three]), ISSUER);
    assert!(matches!(refused, Err(KeysError::TooMany(3))), "{refused:?}");
    let refused = Verifier::from_jwks(r#"{"keys":[]}"#, ISSUER);
    assert!(matches!(refused, Err(KeysError::Empty)), "{refused:?}");
    let refused = Verifier::from_jwks(&jwks(&[one, one]), ISSUER);
    assert!(
        matches!(refused, Err(KeysError::DuplicateKeyId(_))),
        "{refused:?}"
    );
}
