//! The graph `main` runs, against Postgres, over a real connection:
//! `make -C policy stack-up suites`.

#![cfg(test)]

mod caller_tokens;

use std::str::FromStr;

use caller_tokens::*;
use cedar_policy::{EntityId, EntityTypeName, EntityUid};
use decision::{Engine, OrgGrn, Role, RoleScope};
use entity::{EntityChange, EntityStore};
use grant::{Body, NewPolicy, PolicyStore};
use server::proto::policy_service_client::PolicyServiceClient;
use server::proto::{Action, EvaluateRequest, Evaluation, EvaluationsRequest, Resource, Subject};
use server::{Verifier, serve};
use sqlx::PgPool;
use sqlx::migrate::Migrator;
use tokio::net::TcpListener;
use tonic::transport::Channel;
use tonic::{Code, Request};

type Named = (&'static str, &'static str);

const ACME: &str = "grn:root:default:org:acme";
const ORG: Named = ("Org", ACME);
const MEMBERS: Named = ("OrgMembers", ACME);
const LEE: Named = ("User", "grn:acme:default:user:lee");
const MIA: Named = ("User", "grn:globex:default:user:mia");
const BOT: Named = ("Agent", "grn:acme:default:agent:bot");
const PEER: Named = ("Agent", "grn:acme:default:agent:peer");

static MIGRATIONS: Migrator = sqlx::migrate!("../../migrations");

fn uid(named: Named) -> EntityUid {
    EntityUid::from_type_name_and_id(
        EntityTypeName::from_str(named.0).unwrap(),
        EntityId::new(named.1),
    )
}

fn upsert(
    engine: &Engine,
    named: Named,
    org: Option<Named>,
    members: Option<Named>,
    parents: &[Named],
) -> EntityChange {
    EntityChange::upsert(
        engine,
        uid(named),
        1,
        org.map(uid),
        members.map(uid),
        parents.iter().copied().map(uid).collect(),
    )
    .unwrap()
}

/// Lee is a member holding the member role; mia belongs to nothing here.
async fn seed(pool: &PgPool) {
    let engine = Engine::load().unwrap();
    let changes = [
        upsert(&engine, MEMBERS, None, None, &[]),
        upsert(&engine, ORG, Some(ORG), Some(MEMBERS), &[]),
        upsert(&engine, LEE, None, None, &[MEMBERS]),
        upsert(&engine, MIA, None, None, &[]),
        upsert(&engine, BOT, Some(ORG), None, &[MEMBERS, ORG]),
    ];
    EntityStore::new(pool.clone(), engine.clone())
        .apply(&changes)
        .await
        .unwrap();

    let org = OrgGrn::new(ACME);
    let grn = "grn:acme:default:policy:lee-member";
    let store = PolicyStore::new(pool.clone());
    let body = Body::role_link(
        &engine,
        Role::Member,
        RoleScope::Organization,
        uid(LEE),
        uid(ORG),
    )
    .unwrap();
    store
        .create(NewPolicy {
            grn,
            org: &org,
            name: "lee",
            body,
            created_by: "test",
        })
        .await
        .unwrap();
    store.activate(grn).await.unwrap();
}

async fn client(pool: PgPool) -> PolicyServiceClient<Channel> {
    let verifier = Verifier::from_jwks(&jwks(&[signer()]), ISSUER).unwrap();
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap();
    tokio::spawn(serve(pool, verifier, listener, std::future::pending()));
    let channel = Channel::from_shared(format!("http://{address}"))
        .unwrap()
        .connect()
        .await
        .unwrap();
    PolicyServiceClient::new(channel)
}

fn authenticated<T>(message: T) -> Request<T> {
    let mut request = Request::new(message);
    let value = format!("Bearer {}", signer().sign(&claims()));
    request
        .metadata_mut()
        .insert("authorization", value.parse().unwrap());
    request
}

fn subject(named: Named) -> Option<Subject> {
    Some(Subject {
        r#type: named.0.into(),
        id: named.1.into(),
    })
}

fn resource(named: Named) -> Option<Resource> {
    Some(Resource {
        r#type: named.0.into(),
        id: named.1.into(),
        properties: None,
    })
}

fn action(name: &str) -> Option<Action> {
    Some(Action { name: name.into() })
}

fn question(who: Named, name: &str, what: Named) -> EvaluateRequest {
    EvaluateRequest {
        subject: subject(who),
        action: action(name),
        resource: resource(what),
        context: None,
        read: 0,
    }
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn evaluate_decides_from_the_services_own_stores(pool: PgPool) {
    seed(&pool).await;
    let mut client = client(pool).await;

    let allowed = client
        .evaluate(authenticated(question(LEE, "agent:read", BOT)))
        .await
        .unwrap()
        .into_inner();
    assert!(allowed.decision, "{allowed:?}");
    let reasons = allowed.context.unwrap().reasons;
    assert_eq!(reasons[0].policy_id, "grn:acme:default:policy:lee-member");

    let outside = client
        .evaluate(authenticated(question(MIA, "agent:read", BOT)))
        .await
        .unwrap()
        .into_inner();
    assert!(!outside.decision, "{outside:?}");
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_resource_outside_the_store_is_denied_with_its_error_as_a_reason(pool: PgPool) {
    seed(&pool).await;
    let mut client = client(pool).await;

    let unknown = client
        .evaluate(authenticated(question(LEE, "agent:read", PEER)))
        .await
        .unwrap()
        .into_inner();

    assert!(!unknown.decision, "{unknown:?}");
    let reasons = unknown.context.unwrap().reasons;
    assert!(!reasons[0].error.is_empty(), "{reasons:?}");
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn evaluations_answer_in_order_and_take_the_requests_defaults(pool: PgPool) {
    seed(&pool).await;
    let mut client = client(pool).await;
    let entry = |who: Named| Evaluation {
        subject: subject(who),
        action: None,
        resource: None,
        context: None,
    };

    let answers = client
        .evaluations(authenticated(EvaluationsRequest {
            subject: None,
            action: action("agent:read"),
            resource: resource(BOT),
            context: None,
            read: 0,
            evaluations: vec![entry(MIA), entry(LEE), entry(MIA)],
        }))
        .await
        .unwrap()
        .into_inner();

    let decisions: Vec<bool> = answers.evaluations.iter().map(|a| a.decision).collect();
    assert_eq!(decisions, [false, true, false]);
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_caller_supplies_no_grounds_and_names_only_the_schema(pool: PgPool) {
    seed(&pool).await;
    let mut client = client(pool).await;

    let mut with_properties = question(LEE, "agent:read", BOT);
    let mut grounds = prost_types::Struct::default();
    grounds.fields.insert(
        "org".into(),
        prost_types::Value {
            kind: Some(prost_types::value::Kind::StringValue("x".into())),
        },
    );
    if let Some(resource) = with_properties.resource.as_mut() {
        resource.properties = Some(grounds);
    }
    let refused = client.evaluate(authenticated(with_properties)).await;
    assert_eq!(refused.unwrap_err().code(), Code::InvalidArgument);

    let unnamed = client
        .evaluate(authenticated(question(LEE, "no:such", BOT)))
        .await;
    assert_eq!(unnamed.unwrap_err().code(), Code::InvalidArgument);

    let control = client
        .evaluate(authenticated(question(LEE, "agent:read", BOT)))
        .await;
    assert!(control.is_ok(), "{control:?}");
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_store_that_cannot_be_read_fails_the_call_rather_than_answering(pool: PgPool) {
    seed(&pool).await;
    let mut client = client(pool.clone()).await;
    let control = client
        .evaluate(authenticated(question(LEE, "agent:read", BOT)))
        .await;
    assert!(control.is_ok(), "{control:?}");

    pool.close().await;

    let failed = client
        .evaluate(authenticated(question(LEE, "agent:read", BOT)))
        .await;
    assert_eq!(failed.unwrap_err().code(), Code::Unavailable);
}
