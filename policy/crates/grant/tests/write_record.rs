//! Each write of a policy leaves its record in its own transaction
//! (ADR-0148 item 1), against Postgres: `make -C policy stack-up
//! suites`.

#![cfg(test)]

use std::str::FromStr;

use audit::{Origin, Recorder};
use cedar_policy::{EntityId, EntityTypeName, EntityUid};
use decision::{Engine, OrgGrn, Role, RoleScope};
use grant::{Body, Error, NewPolicy, PolicyStore};
use sqlx::PgPool;

const ACME: &str = "grn:root/iam/org/acme";
const GRANT: &str = "grn:acme/iam/policy/lee-member";
const LEE: &str = "grn:acme/iam/user/lee";

fn uid(entity_type: &str, id: &str) -> EntityUid {
    EntityUid::from_type_name_and_id(
        EntityTypeName::from_str(entity_type).unwrap(),
        EntityId::new(id),
    )
}

fn role(role: Role) -> Body {
    Body::role_link(
        &Engine::load().unwrap(),
        &OrgGrn::new(ACME),
        role,
        RoleScope::Organization,
        uid("Iam::User", LEE),
        uid("Iam::Org", ACME),
    )
    .unwrap()
}

fn draft(body: Body, org: &OrgGrn) -> NewPolicy<'_> {
    NewPolicy {
        grn: GRANT,
        org,
        name: "lee",
        body,
        created_by: "grn:acme/iam/user/kim",
    }
}

fn store(pool: PgPool) -> PolicyStore {
    PolicyStore::new(pool).acting(Origin {
        service: "platform".into(),
        subject: Some("grn:acme/iam/user/kim".into()),
    })
}

/// The outcome, action and versions of each write, oldest first.
async fn written(pool: &PgPool) -> Vec<(String, String, Option<i32>, Option<i32>)> {
    let page = Recorder::new(pool.clone())
        .list(ACME, None, 100)
        .await
        .unwrap();
    page.records
        .into_iter()
        .rev()
        .map(|r| {
            (
                r.outcome.unwrap(),
                r.action.unwrap(),
                r.version_from,
                r.version_to,
            )
        })
        .collect()
}

fn entry(
    outcome: &str,
    action: &str,
    from: Option<i32>,
    to: Option<i32>,
) -> (String, String, Option<i32>, Option<i32>) {
    (outcome.into(), action.into(), from, to)
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn every_write_of_a_policy_leaves_its_record_and_a_read_leaves_none(pool: PgPool) {
    let store = store(pool.clone());
    let org = OrgGrn::new(ACME);

    store.create(draft(role(Role::Member), &org)).await.unwrap();
    store
        .append_version(&org, GRANT, &role(Role::Admin), "grn:acme/iam/user/kim")
        .await
        .unwrap();
    store.roll_back(&org, GRANT, 2).await.unwrap();
    store.activate(&org, GRANT).await.unwrap();
    store.get(&org, GRANT).await.unwrap();
    store.list(&org).await.unwrap();
    store.delete(&org, GRANT).await.unwrap();

    assert_eq!(
        written(&pool).await,
        [
            entry("created", "iam:policy.attach", None, Some(1)),
            entry("version-added", "iam:policy.attach", None, Some(2)),
            entry("rolled-back", "iam:policy.attach", Some(1), Some(2)),
            entry("activated", "iam:policy.attach", None, Some(2)),
            entry("removed", "iam:policy.detach", None, None),
        ]
    );
    let page = Recorder::new(pool).list(ACME, None, 100).await.unwrap();
    assert!(
        page.records
            .iter()
            .all(|r| r.service.as_deref() == Some("platform"))
    );
    assert!(
        page.records
            .iter()
            .all(|r| r.resource.as_deref() == Some(GRANT))
    );
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_refused_write_leaves_no_record(pool: PgPool) {
    let store = store(pool.clone());
    let org = OrgGrn::new(ACME);
    store.create(draft(role(Role::Member), &org)).await.unwrap();

    let again = store.create(draft(role(Role::Member), &org)).await;
    assert!(matches!(again, Err(Error::AlreadyExists(_))), "{again:?}");
    let missing = store.activate(&org, "grn:acme/iam/policy/none").await;
    assert!(missing.is_err());

    assert_eq!(
        written(&pool).await,
        [entry("created", "iam:policy.attach", None, Some(1))]
    );
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_write_whose_record_cannot_be_written_is_not_made(pool: PgPool) {
    let store = store(pool.clone());
    let org = OrgGrn::new(ACME);
    store.create(draft(role(Role::Member), &org)).await.unwrap();
    store.activate(&org, GRANT).await.unwrap();

    sqlx::query("ALTER TABLE audit_records ADD CONSTRAINT refuse CHECK (false) NOT VALID")
        .execute(&pool)
        .await
        .unwrap();

    let appended = store
        .append_version(&org, GRANT, &role(Role::Admin), "grn:acme/iam/user/kim")
        .await;
    assert!(matches!(appended, Err(Error::Record(_))), "{appended:?}");
    assert_eq!(store.versions(&org, GRANT).await.unwrap().len(), 1);
    let deleted = store.delete(&org, GRANT).await;
    assert!(matches!(deleted, Err(Error::Record(_))), "{deleted:?}");
    assert!(store.get(&org, GRANT).await.is_ok(), "the policy stays");
    let created = NewPolicy {
        grn: "grn:acme/iam/policy/other",
        ..draft(role(Role::Member), &org)
    };
    assert!(matches!(store.create(created).await, Err(Error::Record(_))));
    assert_eq!(store.list(&org).await.unwrap().len(), 1);
}
