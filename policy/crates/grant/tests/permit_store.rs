//! A stored `permit` (ADR-0144) against Postgres: `make -C policy
//! stack-up suites`.

#![cfg(test)]

mod fixture;

use decision::{EntityRef, Evaluation, OrgGrn, Outcome, PolicySource, Reason};
use fixture::*;
use grant::{Body, Error, NewPolicy, PolicyStore};
use serde_json::json;
use sqlx::PgPool;

const PERMIT: &str = "grn:acme/iam/policy/watch-bot";

fn draft<'a>(body: Body, org: &'a OrgGrn, created_by: &'a str) -> NewPolicy<'a> {
    NewPolicy {
        grn: PERMIT,
        org,
        name: "watch-bot",
        body,
        created_by,
    }
}

fn ask(who: Named) -> Evaluation<'static> {
    Evaluation {
        subject: EntityRef {
            entity_type: who.0,
            id: who.1,
        },
        action: "iam:agent-watch",
        resource: EntityRef {
            entity_type: BOT.0,
            id: BOT.1,
        },
        context: json!({}),
    }
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_stored_permit_allows_by_its_id_and_never_past_the_organization(pool: PgPool) {
    let world = World::new();
    let right = world.owner_right().await;
    let body = Body::permit(&world.engine, &right, WATCH_BOT).unwrap();
    let store = PolicyStore::new(pool);
    let org = OrgGrn::new(ACME);

    store.create(draft(body, &org, OWNER.1)).await.unwrap();
    assert!(store.active(&org).await.unwrap().permits.is_empty());
    store.activate(&org, PERMIT).await.unwrap();
    assert_eq!(store.active(&org).await.unwrap().permits.len(), 1);

    let entities = World {
        engine: world.engine.clone(),
        entities: world.entities.clone(),
        held: Default::default(),
    };
    let allowed = world
        .engine
        .evaluate(&store, &entities, &ask(MEMBER))
        .await
        .unwrap();
    println!("a stored permit allows pat to watch the bot: {allowed:?}");
    assert_eq!(allowed.outcome, Outcome::Allow);
    assert_eq!(
        allowed.reasons,
        [Reason::Policy(cedar_policy::PolicyId::new(PERMIT))]
    );

    let outside = world
        .engine
        .evaluate(&store, &entities, &ask(STRANGER))
        .await
        .unwrap();
    assert_eq!(outside.outcome, Outcome::Deny);
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_permit_is_stored_only_for_the_org_and_writer_its_right_was_given_to(pool: PgPool) {
    let world = World::new();
    let right = world.owner_right().await;
    let store = PolicyStore::new(pool);
    let org = OrgGrn::new(ACME);
    let globex = OrgGrn::new(GLOBEX);
    let body = || Body::permit(&world.engine, &right, WATCH_BOT).unwrap();

    let by_another_writer = store.create(draft(body(), &org, ADMIN.1)).await;
    assert!(
        matches!(by_another_writer, Err(Error::PermitRight)),
        "{by_another_writer:?}"
    );
    let under_another_org = store
        .create(NewPolicy {
            grn: "grn:globex/iam/policy/watch-bot",
            org: &globex,
            name: "watch-bot",
            body: body(),
            created_by: OWNER.1,
        })
        .await;
    assert!(
        matches!(under_another_org, Err(Error::PermitRight)),
        "{under_another_org:?}"
    );

    store.create(draft(body(), &org, OWNER.1)).await.unwrap();

    let appended_by_another = store.append_version(&org, PERMIT, &body(), ADMIN.1).await;
    assert!(
        matches!(appended_by_another, Err(Error::PermitRight)),
        "{appended_by_another:?}"
    );
    let control = store.append_version(&org, PERMIT, &body(), OWNER.1).await;
    assert_eq!(control.unwrap(), 2);
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_permit_version_is_made_effective_only_under_a_right(pool: PgPool) {
    let world = World::new();
    let right = world.owner_right().await;
    let body = |source: &str| Body::permit(&world.engine, &right, source).unwrap();
    let store = PolicyStore::new(pool);
    let org = OrgGrn::new(ACME);
    let held = || async { store.active(&org).await.unwrap().permits.len() };

    store
        .create(draft(body(WATCH_BOT), &org, OWNER.1))
        .await
        .unwrap();
    let without = store.activate(&org, PERMIT).await;
    assert!(
        matches!(without, Err(Error::PermitNeedsRight(_))),
        "{without:?}"
    );
    assert_eq!(held().await, 0);
    store.activate_permit(&org, PERMIT, &right).await.unwrap();
    assert_eq!(held().await, 1);

    // A narrower second version, made the default: rolling back to the
    // first would widen it again.
    let lee = r#"principal == Iam::User::"grn:acme/iam/user/lee","#;
    let narrower = body(&WATCH_BOT.replace("principal,", lee));
    store
        .append_version(&org, PERMIT, &narrower, OWNER.1)
        .await
        .unwrap();
    store
        .roll_back_permit(&org, PERMIT, 2, &right)
        .await
        .unwrap();
    let widened = store.roll_back(&org, PERMIT, 1).await;
    assert!(
        matches!(widened, Err(Error::PermitNeedsRight(_))),
        "{widened:?}"
    );
    store
        .roll_back_permit(&org, PERMIT, 1, &right)
        .await
        .unwrap();

    // A missing version is still a missing version.
    let missing = store.roll_back(&org, PERMIT, 9).await;
    assert!(
        matches!(missing, Err(Error::NoSuchVersion { .. })),
        "{missing:?}"
    );

    // Deleting only narrows, so it asks for nothing.
    store.delete(&org, PERMIT).await.unwrap();
}
