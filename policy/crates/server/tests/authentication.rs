//! A caller's token over a real gRPC connection, with the stores faked:
//! this suite reads no database.

#![cfg(test)]

mod caller_tokens;

use caller_tokens::*;
use cedar_policy::EntityUid;
use decision::{Cause, Engine, EntitySlice, EntitySource, OrgGrn, OrgPolicies, PolicySource};
use jsonwebtoken::{Algorithm, EncodingKey, Header, encode};
use serde_json::json;
use server::proto::policy_service_client::PolicyServiceClient;
use server::proto::{Action, EvaluateRequest, Resource, Subject};
use server::{KeysError, Service, Verifier, authenticated};
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

#[test]
fn keys_load_only_as_one_or_two_rs256_keys_with_a_kid_each() {
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
    let refused = Verifier::from_jwks(&without(&jwks(&[one]), "kid"), ISSUER);
    assert!(matches!(refused, Err(KeysError::NoKeyId)), "{refused:?}");
    let refused = Verifier::from_jwks(&without(&jwks(&[one]), "alg"), ISSUER);
    assert!(
        matches!(refused, Err(KeysError::NotRs256(_))),
        "{refused:?}"
    );
}
