//! The graph `main` runs, against Postgres, over a real connection:
//! `make -C policy stack-up suites`.

#![cfg(test)]

mod caller_tokens;
mod listening;

use std::str::FromStr;

use caller_tokens::*;
use cedar_policy::{EntityId, EntityTypeName, EntityUid};
use decision::{Engine, OrgGrn, Role, RoleScope};
use entity::{EntityChange, EntityStore};
use grant::{Body, NewPolicy, PolicyStore};
use listening::{Pair, bound, trusting};
use server::proto::policy_service_client::PolicyServiceClient;
use server::proto::{Action, EvaluateRequest, Evaluation, EvaluationsRequest, Resource, Subject};
use server::{Verifier, connect, migrate, probe, serve};
use sqlx::PgPool;
use sqlx::migrate::Migrator;
use tonic::transport::Channel;
use tonic::{Code, Request};

type Named = (&'static str, &'static str);

const ACME: &str = "grn:root:default:org:acme";
const ORG: Named = ("Org", ACME);
const MEMBERS: Named = ("OrgMembers", ACME);
const ENG: Named = ("Team", "grn:acme:default:group:eng");
const LEE: Named = ("User", "grn:acme:default:user:lee");
/// A member, in eng, which holds a role on the organization.
const DUO: Named = ("User", "grn:acme:default:user:duo");
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
        upsert(&engine, ENG, Some(ORG), None, &[ORG]),
        upsert(&engine, DUO, None, None, &[MEMBERS, ENG]),
        upsert(&engine, BOT, Some(ORG), None, &[MEMBERS, ORG]),
    ];
    EntityStore::new(pool.clone(), engine.clone())
        .apply(&changes)
        .await
        .unwrap();

    let org = OrgGrn::new(ACME);
    let grn = "grn:acme:default:policy:lee-member";
    let store = PolicyStore::new(pool.clone());
    let held_by = |principal: Named| {
        Body::role_link(
            &engine,
            &org,
            Role::Member,
            RoleScope::Organization,
            uid(principal),
            uid(ORG),
        )
        .unwrap()
    };
    for (grn, principal) in [(grn, LEE), ("grn:acme:default:policy:eng-member", ENG)] {
        let policy = NewPolicy {
            grn,
            org: &org,
            name: "seeded",
            body: held_by(principal),
            created_by: "test",
        };
        store.create(policy).await.unwrap();
        store.activate(&org, grn).await.unwrap();
    }
}

async fn client(pool: PgPool) -> PolicyServiceClient<Channel> {
    let verifier = Verifier::from_jwks(&jwks(&[signer()]), ISSUER).unwrap();
    let pair = Pair::new();
    let bound = bound().await;
    tokio::spawn(serve(
        pool,
        verifier,
        pair.identity(),
        bound.listeners,
        std::future::pending(),
    ));
    PolicyServiceClient::new(trusting(&pair.cert, bound.contract).await)
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

fn context_field() -> prost_types::Struct {
    let mut fields = prost_types::Struct::default();
    fields.fields.insert(
        "org".into(),
        prost_types::Value {
            kind: Some(prost_types::value::Kind::StringValue("x".into())),
        },
    );
    fields
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
async fn deleting_a_team_or_org_members_ends_what_it_held(pool: PgPool) {
    seed(&pool).await;
    let store = EntityStore::new(pool.clone(), Engine::load().unwrap());
    let mut client = client(pool).await;
    let ask = || authenticated(question(DUO, "agent:read", BOT));
    let held = client.evaluate(ask()).await.unwrap().into_inner();
    assert!(held.decision, "{held:?}");

    store
        .apply(&[EntityChange::delete(uid(ENG), 2)])
        .await
        .unwrap();
    let after_team = client.evaluate(ask()).await.unwrap().into_inner();
    assert!(!after_team.decision, "{after_team:?}");

    let lees = || authenticated(question(LEE, "agent:read", BOT));
    let control = client.evaluate(lees()).await.unwrap().into_inner();
    assert!(control.decision, "{control:?}");

    store
        .apply(&[EntityChange::delete(uid(MEMBERS), 2)])
        .await
        .unwrap();
    let after_members = client.evaluate(lees()).await.unwrap().into_inner();
    assert!(!after_members.decision, "{after_members:?}");
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn evaluations_with_no_entries_answer_the_question_the_defaults_state(pool: PgPool) {
    seed(&pool).await;
    let mut client = client(pool).await;
    let empty = |who: Named| EvaluationsRequest {
        subject: subject(who),
        action: action("agent:read"),
        resource: resource(BOT),
        context: None,
        read: 0,
        evaluations: vec![],
    };

    for (who, decision) in [(LEE, true), (MIA, false)] {
        let answers = client
            .evaluations(authenticated(empty(who)))
            .await
            .unwrap()
            .into_inner()
            .evaluations;
        assert_eq!(answers.len(), 1);
        assert_eq!(answers[0].decision, decision, "{answers:?}");
    }

    let no_subject = EvaluationsRequest {
        subject: None,
        ..empty(LEE)
    };
    let answers = client
        .evaluations(authenticated(no_subject))
        .await
        .unwrap()
        .into_inner()
        .evaluations;
    assert_eq!(answers.len(), 1);
    assert!(!answers[0].decision, "{answers:?}");
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_bad_entry_is_denied_where_it_stands_and_the_rest_are_answered(pool: PgPool) {
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
            evaluations: vec![
                entry(LEE),
                entry(("Nope", "x")),
                entry(("User", "a\0b")),
                entry(LEE),
            ],
        }))
        .await
        .unwrap()
        .into_inner()
        .evaluations;

    let decisions: Vec<bool> = answers.iter().map(|a| a.decision).collect();
    assert_eq!(decisions, [true, false, false, true]);
    let reasons = answers[1].context.as_ref().unwrap();
    assert!(!reasons.reasons[0].error.is_empty(), "{reasons:?}");
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_batch_past_the_bound_is_refused(pool: PgPool) {
    seed(&pool).await;
    let mut client = client(pool).await;
    let batch = |entries: usize| EvaluationsRequest {
        subject: subject(LEE),
        action: action("agent:read"),
        resource: resource(BOT),
        context: None,
        read: 0,
        evaluations: vec![
            Evaluation {
                subject: None,
                action: None,
                resource: None,
                context: None,
            };
            entries
        ],
    };

    let refused = client.evaluations(authenticated(batch(1001))).await;
    assert_eq!(refused.unwrap_err().code(), Code::InvalidArgument);

    let control = client.evaluations(authenticated(batch(1000))).await;
    assert_eq!(control.unwrap().into_inner().evaluations.len(), 1000);
}

#[sqlx::test(migrations = false)]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn migrate_applies_the_schema_and_serving_does_not(pool: PgPool) {
    let tables = |pool: PgPool| async move {
        let count: i64 = sqlx::query_scalar(
            "SELECT count(*) FROM information_schema.tables WHERE table_name = 'policies'",
        )
        .fetch_one(&pool)
        .await
        .unwrap();
        count
    };
    let options = pool.connect_options();
    let url = format!(
        "postgres://{}:garam@{}:{}/{}",
        options.get_username(),
        options.get_host(),
        options.get_port(),
        options.get_database().unwrap()
    );

    connect(&url).await.unwrap();
    let verifier = Verifier::from_jwks(&jwks(&[signer()]), ISSUER).unwrap();
    let bound = bound().await;
    serve(
        pool.clone(),
        verifier,
        Pair::new().identity(),
        bound.listeners,
        std::future::ready(()),
    )
    .await
    .unwrap();
    assert_eq!(tables(pool.clone()).await, 0);

    migrate(&pool).await.unwrap();
    assert_eq!(tables(pool.clone()).await, 1);
    migrate(&pool).await.unwrap();
}

#[sqlx::test(migrations = false)]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn migrate_refuses_a_dirty_database_and_leaves_it_dirty(pool: PgPool) {
    migrate(&pool).await.unwrap();
    sqlx::query("UPDATE _sqlx_migrations SET success = false WHERE version = 2")
        .execute(&pool)
        .await
        .unwrap();

    let refused = migrate(&pool).await;

    assert!(refused.is_err(), "{refused:?}");
    let dirty: i64 =
        sqlx::query_scalar("SELECT count(*) FROM _sqlx_migrations WHERE success = false")
            .fetch_one(&pool)
            .await
            .unwrap();
    assert_eq!(dirty, 1);
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
    if let Some(resource) = with_properties.resource.as_mut() {
        resource.properties = Some(context_field());
    }
    let refused = client.evaluate(authenticated(with_properties)).await;
    assert_eq!(refused.unwrap_err().code(), Code::InvalidArgument);

    let mut with_context = question(LEE, "agent:read", BOT);
    with_context.context = Some(context_field());
    let refused = client.evaluate(authenticated(with_context)).await;
    assert_eq!(refused.unwrap_err().code(), Code::InvalidArgument);

    let unnamed = client
        .evaluate(authenticated(question(LEE, "no:such", BOT)))
        .await;
    assert_eq!(unnamed.unwrap_err().code(), Code::InvalidArgument);

    let nul = question(("User", "a\0b"), "agent:read", BOT);
    let refused = client.evaluate(authenticated(nul)).await;
    assert_eq!(refused.unwrap_err().code(), Code::InvalidArgument);

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

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn ready_answers_while_the_database_does(pool: PgPool) {
    let verifier = Verifier::from_jwks(&jwks(&[signer()]), ISSUER).unwrap();
    let bound = bound().await;
    let ready = format!("http://{}/readyz", bound.probes);
    tokio::spawn(serve(
        pool.clone(),
        verifier,
        Pair::new().identity(),
        bound.listeners,
        std::future::pending(),
    ));

    let answered = probe(&ready).await;
    assert!(answered.is_ok(), "{answered:?}");

    pool.close().await;

    let refused = probe(&ready).await;
    assert!(refused.is_err(), "{refused:?}");
}
