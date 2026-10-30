//! The store against Postgres: `make -C policy stack-up suites`.

#![cfg(test)]

use std::str::FromStr;

use cedar_policy::{EntityId, EntityTypeName, EntityUid};
use decision::{Engine, OrgGrn, OrgPolicies, PolicySource, Role, RoleLink, RoleScope};
use grant::{Body, Error, NewPolicy, PolicyStore};
use sqlx::PgPool;

const ACME: &str = "grn:root:default:org:acme";
const GLOBEX: &str = "grn:root:default:org:globex";
const GRANT: &str = "grn:acme:default:policy:lee-member";

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
        uid("User", "grn:acme:default:user:lee"),
        uid("Org", ACME),
    )
    .unwrap()
}

fn draft(body: Body, org: &OrgGrn) -> NewPolicy<'_> {
    NewPolicy {
        grn: GRANT,
        org,
        name: "lee",
        body,
        created_by: "grn:acme:default:user:kim",
    }
}

async fn active(store: &PolicyStore, org: &OrgGrn) -> OrgPolicies {
    store.active(org).await.unwrap()
}

fn roles(policies: &OrgPolicies) -> Vec<Role> {
    policies
        .links
        .iter()
        .map(|link: &RoleLink| link.role)
        .collect()
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_draft_decides_nothing_until_it_is_activated(pool: PgPool) {
    let store = PolicyStore::new(pool);
    let org = OrgGrn::new(ACME);

    store.create(draft(role(Role::Member), &org)).await.unwrap();
    assert!(active(&store, &org).await.links.is_empty());

    store.activate(&org, GRANT).await.unwrap();
    assert_eq!(roles(&active(&store, &org).await), [Role::Member]);
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_policy_is_read_and_written_only_by_its_own_organization(pool: PgPool) {
    let store = PolicyStore::new(pool);
    let org = OrgGrn::new(ACME);
    let other = OrgGrn::new(GLOBEX);
    let kim = "grn:acme:default:user:kim";
    store.create(draft(role(Role::Member), &org)).await.unwrap();
    store.activate(&org, GRANT).await.unwrap();

    assert!(active(&store, &other).await.links.is_empty());
    let admin = role(Role::Admin);
    let rewrite = store.append_version(&other, GRANT, &admin, kim);
    assert!(matches!(rewrite.await, Err(Error::ForeignPolicy(_))));
    let moved = store.roll_back(&other, GRANT, 1).await;
    assert!(matches!(moved, Err(Error::ForeignPolicy(_))), "{moved:?}");
    let activated = store.activate(&other, GRANT).await;
    assert!(matches!(activated, Err(Error::ForeignPolicy(_))));
    let deleted = store.delete(&other, GRANT).await;
    assert!(matches!(deleted, Err(Error::ForeignPolicy(_))));
    let read = store.versions(&other, GRANT).await;
    assert!(matches!(read, Err(Error::ForeignPolicy(_))), "{read:?}");

    assert_eq!(roles(&active(&store, &org).await), [Role::Member]);
    assert_eq!(store.versions(&org, GRANT).await.unwrap().len(), 1);
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_policy_is_created_only_under_its_own_organizations_grn(pool: PgPool) {
    let store = PolicyStore::new(pool);
    let org = OrgGrn::new(ACME);
    let named = |grn| NewPolicy {
        grn,
        ..draft(role(Role::Member), &org)
    };

    for grn in [
        "grn:globex:default:policy:squatted",
        "tenant-isolation",
        "role-org-owner",
        "grn:acme:default:group:eng",
        "grn:acme:default:policy:",
    ] {
        let refused = store.create(named(grn)).await;
        assert!(
            matches!(refused, Err(Error::ForeignPolicy(_))),
            "{grn}: {refused:?}"
        );
    }

    store.create(named(GRANT)).await.unwrap();
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_policy_cannot_point_at_a_version_it_does_not_hold(pool: PgPool) {
    let store = PolicyStore::new(pool.clone());
    let org = OrgGrn::new(ACME);
    store.create(draft(role(Role::Member), &org)).await.unwrap();

    let dangling = sqlx::query("UPDATE policies SET default_version = 99")
        .execute(&pool)
        .await;
    assert!(dangling.is_err(), "{dangling:?}");

    let control = sqlx::query("UPDATE policies SET default_version = 1")
        .execute(&pool)
        .await;
    assert!(control.is_ok(), "{control:?}");
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_stored_guardrail_past_the_bounds_fails_the_read_and_does_not_parse(pool: PgPool) {
    let store = PolicyStore::new(pool.clone());
    let org = OrgGrn::new(ACME);
    let engine = Engine::load().unwrap();
    let body = Body::guardrail(&engine, &org, "forbid (principal, action, resource);").unwrap();
    store.create(draft(body, &org)).await.unwrap();
    store.activate(&org, GRANT).await.unwrap();
    let nested = format!("{}true{}", "(".repeat(200), ")".repeat(200));
    let source = format!("forbid (principal, action, resource) when {{ {nested} }};");
    sqlx::query(
        "INSERT INTO policy_versions (policy_grn, version, body, created_by)
         VALUES ($1, 2, jsonb_build_object('kind', 'guardrail', 'source', $2::text), 'test')",
    )
    .bind(GRANT)
    .bind(source)
    .execute(&pool)
    .await
    .unwrap();
    store.roll_back(&org, GRANT, 2).await.unwrap();

    let read = store.active_policies(&org).await;

    assert!(matches!(read, Err(Error::GuardrailTooDeep)), "{read:?}");
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn appending_a_version_leaves_the_default_where_it_is(pool: PgPool) {
    let store = PolicyStore::new(pool);
    let org = OrgGrn::new(ACME);
    store.create(draft(role(Role::Member), &org)).await.unwrap();
    store.activate(&org, GRANT).await.unwrap();

    let version = store
        .append_version(&org, GRANT, &role(Role::Admin), "grn:acme:default:user:kim")
        .await
        .unwrap();

    assert_eq!(version, 2);
    assert_eq!(roles(&active(&store, &org).await), [Role::Member]);
    assert_eq!(store.versions(&org, GRANT).await.unwrap().len(), 2);
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn the_default_moves_to_any_stored_version_and_no_other(pool: PgPool) {
    let store = PolicyStore::new(pool);
    let org = OrgGrn::new(ACME);
    store.create(draft(role(Role::Member), &org)).await.unwrap();
    store.activate(&org, GRANT).await.unwrap();
    let kim = "grn:acme:default:user:kim";
    store
        .append_version(&org, GRANT, &role(Role::Admin), kim)
        .await
        .unwrap();

    store.roll_back(&org, GRANT, 2).await.unwrap();
    assert_eq!(roles(&active(&store, &org).await), [Role::Admin]);
    store.roll_back(&org, GRANT, 1).await.unwrap();
    assert_eq!(roles(&active(&store, &org).await), [Role::Member]);

    let refused = store.roll_back(&org, GRANT, 3).await;
    assert!(
        matches!(refused, Err(Error::NoSuchVersion { version: 3, .. })),
        "{refused:?}"
    );
    assert_eq!(roles(&active(&store, &org).await), [Role::Member]);
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_stored_version_cannot_be_rewritten_or_removed(pool: PgPool) {
    let store = PolicyStore::new(pool.clone());
    let org = OrgGrn::new(ACME);
    store.create(draft(role(Role::Member), &org)).await.unwrap();

    let rewritten = sqlx::query("UPDATE policy_versions SET created_by = 'someone'")
        .execute(&pool)
        .await;
    assert!(rewritten.is_err(), "{rewritten:?}");
    let removed = sqlx::query("DELETE FROM policy_versions")
        .execute(&pool)
        .await;
    assert!(removed.is_err(), "{removed:?}");

    let versions = store.versions(&org, GRANT).await.unwrap();
    assert_eq!(versions.len(), 1);
    assert_eq!(versions[0].created_by, "grn:acme:default:user:kim");
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn deleting_a_policy_takes_its_versions_with_it(pool: PgPool) {
    let store = PolicyStore::new(pool);
    let org = OrgGrn::new(ACME);
    store.create(draft(role(Role::Member), &org)).await.unwrap();
    store.activate(&org, GRANT).await.unwrap();

    store.delete(&org, GRANT).await.unwrap();

    assert!(store.versions(&org, GRANT).await.unwrap().is_empty());
    assert!(active(&store, &org).await.links.is_empty());
    let again = store.delete(&org, GRANT).await;
    assert!(matches!(again, Err(Error::NotFound(_))), "{again:?}");
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_grn_is_held_by_one_policy(pool: PgPool) {
    let store = PolicyStore::new(pool);
    let org = OrgGrn::new(ACME);
    store.create(draft(role(Role::Member), &org)).await.unwrap();

    let again = store.create(draft(role(Role::Admin), &org)).await;
    assert!(matches!(again, Err(Error::AlreadyExists(_))), "{again:?}");
    assert_eq!(store.versions(&org, GRANT).await.unwrap().len(), 1);
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_guardrail_reads_back_as_a_forbid_under_its_grn(pool: PgPool) {
    let store = PolicyStore::new(pool);
    let org = OrgGrn::new(ACME);
    let engine = Engine::load().unwrap();
    let body = Body::guardrail(&engine, &org, "forbid (principal, action, resource);").unwrap();
    store.create(draft(body, &org)).await.unwrap();
    store.activate(&org, GRANT).await.unwrap();

    let policies = active(&store, &org).await;

    assert_eq!(policies.guardrails.len(), 1);
    assert_eq!(policies.guardrails[0].id().to_string(), GRANT);
    assert!(
        engine
            .compose(&policies.links, &policies.guardrails)
            .is_ok()
    );
}
