//! An uninstall's purge of the policies naming an installation or its
//! instances (ADR-0192 decision 4), against Postgres: `make -C policy
//! stack-up suites`.

#![cfg(test)]

mod fixture;

use audit::Origin;
use decision::OrgGrn;
use fixture::*;
use grant::{Body, NewPolicy, PolicyStore};
use sqlx::PgPool;

const INSTALLATION: Named = (
    "Iam::Installation",
    "grn:acme/iam/installation/garam-bot-connector",
);
const INSTANCE: Named = ("Iam::Instance", "grn:acme/iam/instance/5f60710a1b2c3d4e");
const OTHER_INSTALLATION: Named = ("Iam::Installation", "grn:acme/iam/installation/deployer");

/// A `forbid` of reading `resource`, a guardrail naming it in its scope.
fn no_reading(resource: Named) -> String {
    format!(
        r#"forbid (principal, action == Iam::Action::"installation.read", resource == {}::"{}");"#,
        resource.0, resource.1
    )
}

fn policy<'a>(grn: &'a str, org: &'a OrgGrn, body: Body) -> NewPolicy<'a> {
    NewPolicy {
        grn,
        org,
        name: "p",
        body,
        created_by: OWNER.1,
    }
}

fn store(pool: PgPool) -> PolicyStore {
    PolicyStore::new(pool).acting(Origin {
        service: "platform".into(),
        subject: Some(ADMIN.1.into()),
    })
}

/// Every policy naming the installation or its instance goes, whoever
/// wrote it and wherever it names it: in a guardrail's scope, in a
/// guardrail's condition, in a permit's scope, and in a version that is
/// not the default, since a rollback would make it decide again. A
/// policy naming another installation, and one naming neither, are the
/// controls, and each removal is recorded as the actor's.
#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_purge_takes_every_policy_naming_the_entities_and_no_other(pool: PgPool) {
    let world = World::new();
    let right = world.owner_right().await;
    let store = store(pool.clone());
    let org = OrgGrn::new(ACME);
    sqlx::query(
        "INSERT INTO entities (entity_type, entity_id, sequence, document)
         VALUES ('Iam::Org', $1, 1, '{}'::jsonb)",
    )
    .bind(ACME)
    .execute(&pool)
    .await
    .unwrap();

    let guardrail = |source: &str| Body::guardrail(&world.engine, &org, source).unwrap();
    let in_scope = "grn:acme/iam/policy/a-scope";
    let in_condition = "grn:acme/iam/policy/b-condition";
    let a_permit = "grn:acme/iam/policy/c-permit";
    let in_a_later_version = "grn:acme/iam/policy/d-later";
    let another = "grn:acme/iam/policy/e-another";
    let neither = "grn:acme/iam/policy/f-neither";
    store
        .create(policy(in_scope, &org, guardrail(&no_reading(INSTALLATION))))
        .await
        .unwrap();
    store
        .create(policy(
            in_condition,
            &org,
            guardrail(&format!(
                r#"forbid (principal, action == Iam::Action::"instance.read", resource)
                   when {{ resource == {}::"{}" }};"#,
                INSTALLATION.0, INSTALLATION.1
            )),
        ))
        .await
        .unwrap();
    let permit = format!(
        r#"permit (principal == {}::"{}", action == Iam::Action::"installation.read", resource == {}::"{}");"#,
        OWNER.0, OWNER.1, INSTALLATION.0, INSTALLATION.1
    );
    store
        .create(policy(
            a_permit,
            &org,
            Body::permit(&world.engine, &right, &permit).unwrap(),
        ))
        .await
        .unwrap();
    store
        .create(policy(
            in_a_later_version,
            &org,
            guardrail(&no_reading(OTHER_INSTALLATION)),
        ))
        .await
        .unwrap();
    store
        .append_version(
            &org,
            in_a_later_version,
            &guardrail(&no_reading(INSTALLATION)),
            OWNER.1,
        )
        .await
        .unwrap();
    store
        .create(policy(
            another,
            &org,
            guardrail(&no_reading(OTHER_INSTALLATION)),
        ))
        .await
        .unwrap();
    store
        .create(policy(neither, &org, guardrail(&no_reading(ORG))))
        .await
        .unwrap();

    let purged = store
        .purge(&org, &[uid(INSTALLATION), uid(INSTANCE)])
        .await
        .unwrap();

    assert_eq!(
        purged,
        [in_scope, in_condition, a_permit, in_a_later_version]
    );
    let left: Vec<String> = store
        .list(&org)
        .await
        .unwrap()
        .into_iter()
        .map(|summary| summary.grn)
        .collect();
    assert_eq!(left, [another, neither], "the controls stand");
    let recorded: Vec<(String, String)> = sqlx::query_as(
        "SELECT subject, outcome FROM audit_records
         WHERE outcome = 'removed' ORDER BY policy_ids",
    )
    .fetch_all(&pool)
    .await
    .unwrap();
    assert_eq!(recorded.len(), 4, "{recorded:?}");
    assert!(
        recorded.iter().all(|(subject, _)| subject == ADMIN.1),
        "each removal is the actor's: {recorded:?}"
    );

    assert!(
        store
            .purge(&org, &[uid(INSTALLATION)])
            .await
            .unwrap()
            .is_empty(),
        "a second purge finds nothing"
    );
}

/// A purge reads only its own organization's policies: another
/// organization's policy is never judged, whatever it names.
#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_purge_reads_only_its_organizations_policies(pool: PgPool) {
    let world = World::new();
    let store = store(pool);
    let globex = OrgGrn::new(GLOBEX);
    let theirs = "grn:globex/iam/policy/theirs";
    let globex_installation: Named = (
        "Iam::Installation",
        "grn:globex/iam/installation/garam-bot-connector",
    );
    store
        .create(policy(
            theirs,
            &globex,
            Body::guardrail(&world.engine, &globex, &no_reading(globex_installation)).unwrap(),
        ))
        .await
        .unwrap();

    let purged = store
        .purge(&OrgGrn::new(ACME), &[uid(globex_installation)])
        .await
        .unwrap();
    assert!(purged.is_empty());
    assert_eq!(store.list(&globex).await.unwrap().len(), 1);
}
