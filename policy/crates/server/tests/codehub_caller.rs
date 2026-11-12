//! Codehub as a caller of the policy service (ADR-0149), over the graph
//! `main` runs, against Postgres: `make -C policy stack-up suites`.
//! The refusals that read no store are in `authzen_binding.rs`.

#![cfg(test)]

mod binding_client;
mod caller_tokens;
mod listening;

use std::str::FromStr;

use binding_client::{Listening, listening, token_of};
use cedar_policy::{EntityId, EntityTypeName, EntityUid};
use decision::{Engine, EntityRef, OrgGrn, Role, RoleScope};
use entity::{EntityChange, EntityStore, Upsert};
use grant::{Body, NewPolicy, PermitRight, PolicyStore};
use listening::trusting;
use serde_json::{Value, json};
use server::proto::policy_service_client::PolicyServiceClient;
use server::proto::{Action, EvaluateRequest, Resource, Subject};
use sqlx::PgPool;
use sqlx::migrate::Migrator;
use tonic::{Code, Request};

type Named = (&'static str, &'static str);

const ACME: &str = "grn:root/iam/org/acme";
const ORG: Named = ("Iam::Org", ACME);
const MEMBERS: Named = ("Iam::OrgMembers", ACME);
const LEE: Named = ("Iam::User", "grn:acme/iam/user/lee");
const ADA: Named = ("Iam::User", "grn:acme/iam/user/ada");
const OWNER: Named = ("Iam::User", "grn:acme/iam/user/owen");
const BOT: Named = ("Iam::Agent", "grn:acme/iam/agent/bot");
const REPO_ID: &str = "grn:acme/codehub/repo/4821";
const REPO: Named = ("Codehub::Repo", REPO_ID);
const CODEHUB: &str = "grn:acme/iam/service/codehub";
const EVALUATION: &str = "/access/v1/evaluation";
const EVALUATIONS: &str = "/access/v1/evaluations";
const CHANGES: &str = "/garam/policy/v1/entity-changes";

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
        Upsert {
            org: org.map(uid),
            members: members.map(uid),
            parents: parents.iter().copied().map(uid).collect(),
            ..Upsert::default()
        },
    )
    .unwrap()
}

async fn link(store: &PolicyStore, engine: &Engine, role: Role, who: Named, grn: &str) {
    let org = OrgGrn::new(ACME);
    let body = Body::role_link(
        engine,
        &org,
        role,
        RoleScope::Organization,
        uid(who),
        uid(ORG),
    )
    .unwrap();
    store
        .create(NewPolicy {
            grn,
            org: &org,
            name: "seeded",
            body,
            created_by: "test",
        })
        .await
        .unwrap();
    store.activate(&org, grn).await.unwrap();
}

/// Lee and the bot are members, ada an administrator and owen the
/// owner. The repository is not seeded: codehub feeds it.
async fn seed(pool: &PgPool) {
    let engine = Engine::load().unwrap();
    let changes = [
        upsert(&engine, MEMBERS, None, None, &[]),
        upsert(&engine, ORG, Some(ORG), Some(MEMBERS), &[]),
        upsert(&engine, LEE, None, None, &[MEMBERS]),
        upsert(&engine, ADA, None, None, &[MEMBERS]),
        upsert(&engine, OWNER, None, None, &[MEMBERS]),
        upsert(&engine, BOT, Some(ORG), None, &[MEMBERS, ORG]),
    ];
    EntityStore::new(pool.clone(), engine.clone())
        .apply(&changes)
        .await
        .unwrap();

    let store = PolicyStore::new(pool.clone());
    link(
        &store,
        &engine,
        Role::Member,
        LEE,
        "grn:acme/iam/policy/lee",
    )
    .await;
    link(
        &store,
        &engine,
        Role::Member,
        BOT,
        "grn:acme/iam/policy/bot",
    )
    .await;
    link(&store, &engine, Role::Admin, ADA, "grn:acme/iam/policy/ada").await;
    link(
        &store,
        &engine,
        Role::Owner,
        OWNER,
        "grn:acme/iam/policy/owen",
    )
    .await;
}

fn entity(kind: &str, id: &str) -> Value {
    json!({"type": kind, "id": id})
}

fn question(who: Named, action: &str, what: Named, context: Option<Value>) -> Value {
    let mut body = json!({
        "subject": entity(who.0, who.1),
        "action": {"name": action},
        "resource": entity(what.0, what.1),
    });
    if let Some(context) = context {
        body["context"] = context;
    }
    body
}

async fn post(service: &Listening, path: &str, body: &Value) -> binding_client::Reply {
    service
        .call("POST", path, Some(&token_of(CODEHUB)), &[], Some(body))
        .await
}

/// Codehub feeds the repository under the organization.
async fn feed_repo(service: &Listening, kind: &str) {
    let org = entity(if kind == "grn" { kind } else { ORG.0 }, ACME);
    let body = json!({"changes": [{
        "entity": entity(kind, REPO_ID),
        "sequence": "1",
        "upsert": {"org": org, "parents": [org]},
    }]});
    let reply = post(service, CHANGES, &body).await;
    assert_eq!(reply.status, 200, "{}", reply.body);
    assert_eq!(
        reply.json(),
        json!({"written": 1, "dropped": 0, "refusals": []})
    );
}

async fn allowed(service: &Listening, body: &Value) -> bool {
    let reply = post(service, EVALUATION, body).await;
    assert_eq!(reply.status, 200, "{}", reply.body);
    reply.json()["decision"].as_bool().unwrap()
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_member_reads_a_fed_repository_and_an_administrator_writes_it(pool: PgPool) {
    seed(&pool).await;
    let service = listening(pool).await;
    feed_repo(&service, "Codehub::Repo").await;
    let read = |who| question(who, "codehub:repo.read", REPO, None);
    let write = |who| question(who, "codehub:repo.write", REPO, None);

    assert!(allowed(&service, &read(LEE)).await);
    assert!(!allowed(&service, &write(LEE)).await);
    assert!(allowed(&service, &write(ADA)).await);
    assert!(allowed(&service, &write(OWNER)).await);
    let org_use = question(LEE, "codehub:org.use", ORG, None);
    assert!(allowed(&service, &org_use).await);
    let create = question(LEE, "codehub:repo.create", ORG, None);
    assert!(!allowed(&service, &create).await);
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn codehub_is_refused_another_organization_and_another_service(pool: PgPool) {
    seed(&pool).await;
    let service = listening(pool).await;
    let foreign = question(
        LEE,
        "codehub:repo.read",
        ("Codehub::Repo", "grn:globex/codehub/repo/9"),
        None,
    );
    let iam = question(LEE, "iam:agent.read", BOT, None);

    for body in [foreign, iam] {
        let reply = post(&service, EVALUATION, &body).await;
        assert_eq!(reply.status, 403, "{}", reply.body);
    }
    // Control: its own organization and service are answered.
    let own = question(LEE, "codehub:org.use", ORG, None);
    assert!(allowed(&service, &own).await);
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn codehub_feeds_only_the_type_it_owns(pool: PgPool) {
    seed(&pool).await;
    let service = listening(pool).await;
    let change = |kind: &str, id: &str| {
        json!({"changes": [{
            "entity": entity(kind, id),
            "sequence": "1",
            "upsert": {"org": entity("Iam::Org", ACME)},
        }]})
    };

    for (kind, id) in [
        ("Iam::User", "grn:acme/iam/user/new"),
        (
            "Cowork::Channel",
            "grn:acme/cowork/project/web/channel/general",
        ),
    ] {
        let reply = post(&service, CHANGES, &change(kind, id)).await;
        assert_eq!(reply.status, 403, "{kind}: {}", reply.body);
    }
    // Control: the type it owns is written.
    let reply = post(&service, CHANGES, &change("Codehub::Repo", REPO_ID)).await;
    assert_eq!(reply.status, 200, "{}", reply.body);
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn the_type_grn_decides_as_the_cedar_type_name_does(pool: PgPool) {
    seed(&pool).await;
    let service = listening(pool).await;
    // The feed itself names every type by `grn`.
    feed_repo(&service, "grn").await;
    let both = |who: Named, action: &str, what: Named| {
        (
            question(who, action, what, None),
            question(("grn", who.1), action, ("grn", what.1), None),
        )
    };

    let cases = [
        // a user, a repository
        (LEE, "codehub:repo.read", REPO, true),
        (LEE, "codehub:repo.write", REPO, false),
        // an agent
        (BOT, "codehub:repo.read", REPO, true),
        // an organization
        (LEE, "codehub:org.use", ORG, true),
        (LEE, "codehub:repo.create", ORG, false),
    ];
    for (who, action, what, expected) in cases {
        let (named, by_grn) = both(who, action, what);
        assert_eq!(allowed(&service, &named).await, expected, "{named}");
        assert_eq!(allowed(&service, &by_grn).await, expected, "{by_grn}");
    }
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_grn_no_manifest_declares_is_a_bad_request(pool: PgPool) {
    seed(&pool).await;
    let service = listening(pool).await;
    let ask = |subject: &str, resource: &str| {
        json!({
            "subject": entity("grn", subject),
            "action": {"name": "codehub:repo.read"},
            "resource": entity("grn", resource),
        })
    };

    for body in [
        ask(LEE.1, "grn:acme/billing/invoice/1"),
        ask(LEE.1, "grn:acme/codehub/branch/main"),
        ask("not a grn", REPO_ID),
    ] {
        let reply = post(&service, EVALUATION, &body).await;
        assert_eq!(reply.status, 400, "{body}: {}", reply.body);
    }
    // Control: the same question with declared GRNs is answered.
    let reply = post(&service, EVALUATION, &ask(LEE.1, REPO_ID)).await;
    assert_eq!(reply.status, 200, "{}", reply.body);
}

const GUARDRAIL: &str = r#"forbid (
    principal,
    action == Codehub::Action::"repo.write",
    resource
) when {
    context has path
    && (context.path == "src" || context.path like "src/*")
};"#;

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_guardrail_on_the_path_denies_the_directory_and_nothing_else(pool: PgPool) {
    seed(&pool).await;
    let service = listening(pool.clone()).await;
    feed_repo(&service, "Codehub::Repo").await;
    let at = |path: Option<&str>| {
        question(
            ADA,
            "codehub:repo.write",
            REPO,
            path.map(|path| json!({"path": path})),
        )
    };
    // Control: before the guardrail, an administrator writes there.
    assert!(allowed(&service, &at(Some("src/a"))).await);

    let engine = Engine::load().unwrap();
    let org = OrgGrn::new(ACME);
    let store = PolicyStore::new(pool);
    let grn = "grn:acme/iam/policy/frozen-src";
    let body = Body::guardrail(&engine, &org, GUARDRAIL).unwrap();
    store
        .create(NewPolicy {
            grn,
            org: &org,
            name: "frozen-src",
            body,
            created_by: "test",
        })
        .await
        .unwrap();
    store.activate(&org, grn).await.unwrap();

    assert!(!allowed(&service, &at(Some("src/a"))).await);
    assert!(!allowed(&service, &at(Some("src"))).await);
    assert!(allowed(&service, &at(Some("srcx/a"))).await);
    assert!(allowed(&service, &at(None)).await);
}

const DOCS_PERMIT: &str = r#"permit (
    principal,
    action == Codehub::Action::"repo.write",
    resource == Codehub::Repo::"grn:acme/codehub/repo/4821"
) when {
    context has path && context.path like "docs/*"
};"#;

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn an_owners_permit_on_a_path_lets_a_member_write_there_only(pool: PgPool) {
    seed(&pool).await;
    let service = listening(pool.clone()).await;
    feed_repo(&service, "Codehub::Repo").await;
    let at = |path: Option<&str>| {
        question(
            LEE,
            "codehub:repo.write",
            REPO,
            path.map(|path| json!({"path": path})),
        )
    };
    // Control: a member writes nowhere before the permit.
    assert!(!allowed(&service, &at(Some("docs/a"))).await);

    let engine = Engine::load().unwrap();
    let org = OrgGrn::new(ACME);
    let store = PolicyStore::new(pool.clone());
    let entities = EntityStore::new(pool, engine.clone());
    let right = PermitRight::ask(
        &engine,
        &store,
        &entities,
        &org,
        EntityRef {
            entity_type: OWNER.0,
            id: OWNER.1,
        },
    )
    .await
    .unwrap();
    let grn = "grn:acme/iam/policy/docs-writers";
    let body = Body::permit(&engine, &right, DOCS_PERMIT).unwrap();
    store
        .create(NewPolicy {
            grn,
            org: &org,
            name: "docs-writers",
            body,
            created_by: OWNER.1,
        })
        .await
        .unwrap();
    store.activate_permit(&org, grn, &right).await.unwrap();

    assert!(allowed(&service, &at(Some("docs/a"))).await);
    assert!(!allowed(&service, &at(Some("src/a"))).await);
    assert!(!allowed(&service, &at(Some("xdocs/a"))).await);
    assert!(!allowed(&service, &at(None)).await);
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn an_undeclared_context_is_refused_on_both_transports(pool: PgPool) {
    seed(&pool).await;
    let service = listening(pool).await;
    feed_repo(&service, "Codehub::Repo").await;
    let token = token_of(CODEHUB);
    let contexts = [
        json!({"branch": "main"}),
        json!({"path": 7}),
        json!({"path": ["src"]}),
    ];
    let mut grpc = PolicyServiceClient::new(trusting(&service.cert, service.address).await);

    for context in &contexts {
        let body = question(ADA, "codehub:repo.write", REPO, Some(context.clone()));
        let over_http = post(&service, EVALUATION, &body).await;
        assert_eq!(over_http.status, 400, "{context}: {}", over_http.body);

        let mut request = Request::new(EvaluateRequest {
            subject: Some(Subject {
                r#type: ADA.0.into(),
                id: ADA.1.into(),
            }),
            action: Some(Action {
                name: "codehub:repo.write".into(),
            }),
            resource: Some(Resource {
                r#type: REPO.0.into(),
                id: REPO.1.into(),
                properties: None,
            }),
            context: Some(structure(context)),
            read: 0,
        });
        request
            .metadata_mut()
            .insert("authorization", format!("Bearer {token}").parse().unwrap());
        let over_grpc = grpc.evaluate(request).await.unwrap_err();
        assert_eq!(over_grpc.code(), Code::InvalidArgument, "{context}");
    }

    // Control: the declared key and type is decided, as a batch entry
    // too, where a fault is the entry's own.
    let declared = question(ADA, "codehub:repo.write", REPO, Some(json!({"path": "a"})));
    assert!(allowed(&service, &declared).await);
    let batch = json!({"evaluations": [
        {"context": {"branch": "main"}},
        {"context": {"path": "a"}},
    ], "subject": entity(ADA.0, ADA.1),
       "action": {"name": "codehub:repo.write"},
       "resource": entity(REPO.0, REPO.1)});
    let reply = post(&service, EVALUATIONS, &batch).await;
    assert_eq!(reply.status, 200, "{}", reply.body);
    let answers = reply.json();
    assert_eq!(answers["evaluations"][0]["decision"], json!(false));
    assert_eq!(answers["evaluations"][1]["decision"], json!(true));
}

/// A string-valued JSON object as the gRPC context carries it, and a
/// number as a double.
fn structure(value: &Value) -> prost_types::Struct {
    prost_types::Struct {
        fields: value
            .as_object()
            .unwrap()
            .iter()
            .map(|(name, value)| (name.clone(), kind_of(value)))
            .collect(),
    }
}

fn kind_of(value: &Value) -> prost_types::Value {
    use prost_types::value::Kind;
    prost_types::Value {
        kind: Some(match value {
            Value::String(text) => Kind::StringValue(text.clone()),
            Value::Number(number) => Kind::NumberValue(number.as_f64().unwrap()),
            Value::Array(items) => Kind::ListValue(prost_types::ListValue {
                values: items.iter().map(kind_of).collect(),
            }),
            other => panic!("no context in the tests holds {other}"),
        }),
    }
}
