//! AuthZEN's HTTP binding over the graph `main` runs, against Postgres:
//! `make -C policy stack-up suites`.

#![cfg(test)]

mod binding_client;
mod caller_tokens;
mod listening;

use std::str::FromStr;

use binding_client::{Listening, listening, token_of};
use cedar_policy::{EntityId, EntityTypeName, EntityUid};
use decision::{Engine, OrgGrn, Role, RoleScope};
use entity::{EntityChange, EntityStore, Upsert};
use grant::{Body, NewPolicy, PolicyStore};
use serde_json::{Value, json};
use sqlx::PgPool;
use sqlx::migrate::Migrator;

type Named = (&'static str, &'static str);

const ACME: &str = "grn:root/iam/org/acme";
const ORG: Named = ("Iam::Org", ACME);
const MEMBERS: Named = ("Iam::OrgMembers", ACME);
const LEE: Named = ("Iam::User", "grn:acme/iam/user/lee");
const MIA: Named = ("Iam::User", "grn:globex/iam/user/mia");
const BOT: Named = ("Iam::Agent", "grn:acme/iam/agent/bot");
const PEER: Named = ("Iam::Agent", "grn:acme/iam/agent/peer");
const COWORK: &str = "grn:acme/iam/service/cowork";

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

/// Lee is a member holding the member role; mia belongs to nothing yet,
/// and holds the role too, which does her no good outside the
/// organization.
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
    let store = PolicyStore::new(pool.clone());
    for (grn, principal) in [
        ("grn:acme/iam/policy/lee-member", LEE),
        ("grn:acme/iam/policy/mia-member", MIA),
    ] {
        let body = Body::role_link(
            &engine,
            &org,
            Role::Member,
            RoleScope::Organization,
            uid(principal),
            uid(ORG),
        )
        .unwrap();
        let policy = NewPolicy {
            grn,
            org: &org,
            name: "seeded",
            body,
            created_by: "test",
        };
        store.create(policy).await.unwrap();
        store.activate(&org, grn).await.unwrap();
    }
}

fn entity(named: Named) -> Value {
    json!({"type": named.0, "id": named.1})
}

fn question(who: Named, action: &str, what: Named) -> Value {
    json!({"subject": entity(who), "action": {"name": action}, "resource": entity(what)})
}

async fn decide(service: &Listening, who: Named, what: Named) -> Value {
    let reply = service
        .call(
            "POST",
            "/access/v1/evaluation",
            Some(&token_of("platform")),
            &[],
            Some(&question(who, "iam:agent.read", what)),
        )
        .await;
    assert_eq!(reply.status, 200, "{}", reply.body);
    reply.json()
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn an_allowed_and_a_denied_evaluation_answer_200_with_their_reasons(pool: PgPool) {
    seed(&pool).await;
    let service = listening(pool).await;

    let allowed = decide(&service, LEE, BOT).await;
    assert_eq!(allowed["decision"], json!(true), "{allowed}");
    assert_eq!(
        allowed["context"]["reasons"][0]["policy_id"],
        json!("grn:acme/iam/policy/lee-member")
    );

    // Mia holds the role in acme and is no member of it: tenant
    // isolation denies her, in a 200.
    let denied = decide(&service, MIA, BOT).await;
    assert_eq!(denied["decision"], json!(false), "{denied}");

    let unknown = decide(&service, LEE, PEER).await;
    assert_eq!(unknown["decision"], json!(false), "{unknown}");
    assert!(
        !unknown["context"]["reasons"][0]["error"]
            .as_str()
            .unwrap()
            .is_empty(),
        "{unknown}"
    );
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn evaluations_sharing_a_resource_are_answered_one_by_one_and_in_order(pool: PgPool) {
    seed(&pool).await;
    let service = listening(pool).await;
    let entry = |who: Named| json!({"subject": entity(who)});
    let body = json!({
        "action": {"name": "iam:agent.read"},
        "resource": entity(BOT),
        "evaluations": [entry(MIA), entry(LEE), entry(MIA), entry(LEE)],
    });

    let reply = service
        .call(
            "POST",
            "/access/v1/evaluations",
            Some(&token_of("platform")),
            &[],
            Some(&body),
        )
        .await;

    assert_eq!(reply.status, 200, "{}", reply.body);
    let decisions: Vec<Value> = reply.json()["evaluations"]
        .as_array()
        .unwrap()
        .iter()
        .map(|answer| answer["decision"].clone())
        .collect();
    assert_eq!(
        decisions,
        [json!(false), json!(true), json!(false), json!(true)]
    );
}

fn changes(sequence: &str, parents: &[Named]) -> Value {
    let parents: Vec<Value> = parents.iter().copied().map(entity).collect();
    json!({"changes": [{
        "entity": entity(MIA),
        "sequence": sequence,
        "upsert": {"parents": parents},
    }]})
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn an_entity_written_over_http_decides_the_next_question(pool: PgPool) {
    seed(&pool).await;
    let service = listening(pool).await;
    let platform = token_of("platform");
    let write = |body: Value, token: String| {
        let service = &service;
        async move {
            service
                .call(
                    "POST",
                    "/garam/policy/v1/entity-changes",
                    Some(&token),
                    &[],
                    Some(&body),
                )
                .await
        }
    };
    assert_eq!(decide(&service, MIA, BOT).await["decision"], json!(false));

    let joined = write(changes("10", &[MEMBERS]), platform.clone()).await;
    assert_eq!(joined.status, 200, "{}", joined.body);
    assert_eq!(
        joined.json(),
        json!({"written": 1, "dropped": 0, "refusals": []})
    );
    assert_eq!(decide(&service, MIA, BOT).await["decision"], json!(true));

    // A scoped caller does not own `Iam::User`: refused, and the
    // member stays a member.
    let refused = write(changes("11", &[]), token_of(COWORK)).await;
    assert_eq!(refused.status, 403, "{}", refused.body);
    assert_eq!(decide(&service, MIA, BOT).await["decision"], json!(true));

    // Control: the platform's write of the same change is applied.
    let left = write(changes("11", &[]), platform).await;
    assert_eq!(left.status, 200, "{}", left.body);
    assert_eq!(decide(&service, MIA, BOT).await["decision"], json!(false));
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_faulty_entry_is_refused_by_its_index_and_the_rest_are_written(pool: PgPool) {
    seed(&pool).await;
    let service = listening(pool).await;
    let body = json!({"changes": [
        {"entity": entity(MIA), "sequence": 10},
        {"entity": entity(MIA), "sequence": "10",
         "upsert": {"parents": [entity(MEMBERS)]}},
    ]});

    let reply = service
        .call(
            "POST",
            "/garam/policy/v1/entity-changes",
            Some(&token_of("platform")),
            &[],
            Some(&body),
        )
        .await;

    assert_eq!(reply.status, 200, "{}", reply.body);
    let answer = reply.json();
    assert_eq!(
        (&answer["written"], &answer["dropped"]),
        (&json!(1), &json!(0))
    );
    assert_eq!(answer["refusals"][0]["index"], json!(0));
    assert_eq!(answer["refusals"].as_array().unwrap().len(), 1);
}
