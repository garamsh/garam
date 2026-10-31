//! The store against Postgres: `make -C policy stack-up suites`.

#![cfg(test)]

use std::str::FromStr;

use cedar_policy::{EntityId, EntityTypeName, EntityUid};
use decision::{Engine, EntityRef, Evaluation, OrgGrn, OrgPolicies, Outcome, PolicySource};
use entity::{Applied, EntityChange, EntityStore};
use serde_json::json;
use sqlx::PgPool;

type Named = (&'static str, &'static str);

const ACME: &str = "grn:root:default:org:acme";
const ORG: Named = ("Org", ACME);
const MEMBERS: Named = ("OrgMembers", ACME);
const ENG: Named = ("Team", "grn:acme:default:group:eng");
const LEE: Named = ("User", "grn:acme:default:user:lee");
const OUTSIDER: Named = ("User", "grn:acme:default:user:outsider");
const BOT: Named = ("Agent", "grn:acme:default:agent:bot");
const CHANNEL: Named = ("Channel", "grn:acme:default:channel:general");
const GONE: Named = ("Channel", "grn:acme:default:channel:gone");

fn uid(named: Named) -> EntityUid {
    EntityUid::from_type_name_and_id(
        EntityTypeName::from_str(named.0).unwrap(),
        EntityId::new(named.1),
    )
}

fn upsert(named: Named, sequence: i64, org: Option<Named>, parents: &[Named]) -> EntityChange {
    change(named, sequence, org, None, parents)
}

fn change(
    named: Named,
    sequence: i64,
    org: Option<Named>,
    members: Option<Named>,
    parents: &[Named],
) -> EntityChange {
    EntityChange::upsert(
        &Engine::load().unwrap(),
        uid(named),
        sequence,
        org.map(uid),
        members.map(uid),
        parents.iter().copied().map(uid).collect(),
    )
    .unwrap()
}

fn delete(named: Named, sequence: i64) -> EntityChange {
    EntityChange::delete(uid(named), sequence)
}

/// An organization, a team, a member, a team-only user and an agent. The
/// channel names its organization by attribute and holds no parent, so
/// only the attribute leads a slice to it.
fn organization() -> Vec<EntityChange> {
    vec![
        upsert(MEMBERS, 1, None, &[]),
        change(ORG, 1, Some(ORG), Some(MEMBERS), &[]),
        upsert(ENG, 1, Some(ORG), &[ORG]),
        upsert(LEE, 1, None, &[MEMBERS, ENG]),
        upsert(OUTSIDER, 1, None, &[ENG]),
        upsert(BOT, 1, Some(ORG), &[MEMBERS, ORG, ENG]),
        upsert(CHANNEL, 1, Some(ORG), &[]),
    ]
}

async fn store(pool: PgPool) -> EntityStore {
    let store = EntityStore::new(pool, Engine::load().unwrap());
    store.apply(&organization()).await.unwrap();
    store
}

fn holds(slice: &decision::EntitySlice, named: Named) -> bool {
    slice.entities.get(&uid(named)).is_some()
}

/// Leaves the system tier to decide.
struct NoPolicies;

impl PolicySource for NoPolicies {
    async fn active(&self, _: &OrgGrn) -> Result<OrgPolicies, decision::Cause> {
        Ok(OrgPolicies::default())
    }
}

fn message(from: Named, to: Named) -> Evaluation<'static> {
    Evaluation {
        subject: EntityRef {
            entity_type: from.0,
            id: from.1,
        },
        action: "message:send",
        resource: EntityRef {
            entity_type: to.0,
            id: to.1,
        },
        context: json!({}),
    }
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_slice_holds_each_resources_org_and_org_members(pool: PgPool) {
    let store = store(pool).await;

    let slice = store.read_slice(&uid(LEE), &uid(CHANNEL)).await.unwrap();

    assert_eq!(slice.org, Some(OrgGrn::new(ACME)));
    for held in [LEE, CHANNEL, ORG, MEMBERS, ENG] {
        assert!(holds(&slice, held), "{held:?}");
    }
    assert!(!holds(&slice, OUTSIDER), "a slice holds no other member");

    // No parent of the asker leads to these: only the resource does.
    let stranger: Named = ("User", "grn:globex:default:user:stranger");
    store
        .apply(&[upsert(stranger, 1, None, &[])])
        .await
        .unwrap();
    let slice = store
        .read_slice(&uid(stranger), &uid(CHANNEL))
        .await
        .unwrap();
    assert!(holds(&slice, ORG));
    assert!(holds(&slice, MEMBERS));
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_slice_decides_a_member_in_and_a_team_only_user_out(pool: PgPool) {
    let store = store(pool).await;
    let engine = Engine::load().unwrap();
    let peer: Named = ("Agent", "grn:acme:default:agent:peer");
    let stranger: Named = ("Agent", "grn:acme:default:agent:stranger");
    let more = [
        upsert(peer, 1, Some(ORG), &[MEMBERS, ORG]),
        upsert(stranger, 1, Some(ORG), &[ENG]),
    ];
    store.apply(&more).await.unwrap();

    let member = engine
        .evaluate(&NoPolicies, &store, &message(peer, BOT))
        .await
        .unwrap();
    assert_eq!(member.outcome, Outcome::Allow, "{member:?}");

    let team_only = engine
        .evaluate(&NoPolicies, &store, &message(stranger, BOT))
        .await
        .unwrap();
    assert_eq!(team_only.outcome, Outcome::Deny, "{team_only:?}");
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_resource_the_store_does_not_hold_has_no_organization(pool: PgPool) {
    let store = store(pool).await;

    let slice = store.read_slice(&uid(LEE), &uid(GONE)).await.unwrap();

    assert_eq!(slice.org, None);
    assert!(holds(&slice, LEE));
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_replayed_delivery_changes_nothing(pool: PgPool) {
    let store = store(pool).await;

    let applied = store.apply(&organization()).await.unwrap();

    assert_eq!(
        applied,
        Applied {
            written: 0,
            dropped: organization().len(),
        }
    );
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn an_earlier_change_arriving_late_is_dropped(pool: PgPool) {
    let store = store(pool).await;
    let left_the_org = [upsert(LEE, 5, None, &[ENG])];
    store.apply(&left_the_org).await.unwrap();

    let late = [upsert(LEE, 3, None, &[MEMBERS, ENG])];
    let applied = store.apply(&late).await.unwrap();

    assert_eq!(
        applied,
        Applied {
            written: 0,
            dropped: 1
        }
    );
    let slice = store.read_slice(&uid(LEE), &uid(CHANNEL)).await.unwrap();
    let members = uid(MEMBERS);
    assert!(!slice.entities.is_ancestor_of(&members, &uid(LEE)));
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_later_change_replaces_what_an_entity_reaches(pool: PgPool) {
    let store = store(pool).await;

    let moved = [upsert(LEE, 2, None, &[MEMBERS])];
    let applied = store.apply(&moved).await.unwrap();

    assert_eq!(
        applied,
        Applied {
            written: 1,
            dropped: 0
        }
    );
    let slice = store.read_slice(&uid(LEE), &uid(LEE)).await.unwrap();
    assert!(!holds(&slice, ENG), "the team left with the parent");
    assert!(holds(&slice, MEMBERS));
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_deleted_entity_is_gone_and_an_earlier_write_does_not_revive_it(pool: PgPool) {
    let store = store(pool).await;

    store.apply(&[delete(CHANNEL, 4)]).await.unwrap();
    let slice = store.read_slice(&uid(LEE), &uid(CHANNEL)).await.unwrap();
    assert!(!holds(&slice, CHANNEL));
    assert_eq!(slice.org, None);

    let late = [upsert(CHANNEL, 3, Some(ORG), &[])];
    assert_eq!(store.apply(&late).await.unwrap().dropped, 1);
    let slice = store.read_slice(&uid(LEE), &uid(CHANNEL)).await.unwrap();
    assert!(!holds(&slice, CHANNEL));

    let recreated = [upsert(CHANNEL, 5, Some(ORG), &[])];
    assert_eq!(store.apply(&recreated).await.unwrap().written, 1);
    let slice = store.read_slice(&uid(LEE), &uid(CHANNEL)).await.unwrap();
    assert!(holds(&slice, CHANNEL));
}

#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_slice_holds_no_parent_the_store_has_deleted(pool: PgPool) {
    let store = store(pool).await;
    let before = store.read_slice(&uid(LEE), &uid(CHANNEL)).await.unwrap();
    assert!(before.entities.is_ancestor_of(&uid(ENG), &uid(LEE)));
    assert!(before.entities.is_ancestor_of(&uid(MEMBERS), &uid(LEE)));

    store.apply(&[delete(ENG, 2)]).await.unwrap();

    let slice = store.read_slice(&uid(LEE), &uid(CHANNEL)).await.unwrap();
    assert!(!slice.entities.is_ancestor_of(&uid(ENG), &uid(LEE)));
    assert!(slice.entities.is_ancestor_of(&uid(MEMBERS), &uid(LEE)));
}
