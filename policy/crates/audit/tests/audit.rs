//! The record against Postgres: `make -C policy stack-up suites`.

#![cfg(test)]

use audit::{
    Denial, FAULT, Kind, MAX_LIVE_DENIALS, Origin, Outcome, Recorded, Recorder, Write, record_write,
};
use sqlx::PgPool;
use sqlx::migrate::Migrator;

static MIGRATIONS: Migrator = sqlx::migrate!("../../migrations");

const ACME: &str = "grn:root/iam/org/acme";
const GLOBEX: &str = "grn:root/iam/org/globex";
const LEE: &str = "grn:acme/iam/user/lee";
const BOT: &str = "grn:acme/iam/agent/bot";

async fn hold(pool: &PgPool, org: &str) {
    sqlx::query(
        "INSERT INTO entities (entity_type, entity_id, sequence, document)
         VALUES ('Iam::Org', $1, 1, '{}'::jsonb)",
    )
    .bind(org)
    .execute(pool)
    .await
    .unwrap();
}

fn denial<'a>(subject: &'a str, policies: &'a [String]) -> Denial<'a> {
    Denial {
        service: "platform",
        subject,
        action: "iam:agent.watch",
        resource: BOT,
        policies,
    }
}

fn ids(list: &[&str]) -> Vec<String> {
    list.iter().map(|id| (*id).to_owned()).collect()
}

async fn records(recorder: &Recorder, org: &str) -> Vec<audit::Record> {
    recorder.list(org, None, 100).await.unwrap().records
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_denial_leaves_a_record_naming_the_policies_of_its_reasons_by_id(pool: PgPool) {
    hold(&pool, ACME).await;
    let recorder = Recorder::new(pool);
    let policies = ids(&["grn:acme/iam/policy/no-watch", FAULT]);

    assert_eq!(
        recorder.deny(&denial(LEE, &policies)).await,
        Recorded::Stored
    );

    let [record] = records(&recorder, ACME).await.try_into().unwrap();
    assert_eq!(record.kind, Kind::Denial);
    assert_eq!(record.subject.as_deref(), Some(LEE));
    assert_eq!(record.action.as_deref(), Some("iam:agent.watch"));
    assert_eq!(record.resource.as_deref(), Some(BOT));
    assert_eq!(
        record.policy_ids,
        ids(&[FAULT, "grn:acme/iam/policy/no-watch"])
    );
    assert_eq!(record.count, 1);
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn identical_denials_of_an_hour_collapse_whatever_the_order_of_their_reasons(pool: PgPool) {
    hold(&pool, ACME).await;
    let recorder = Recorder::new(pool);
    let forward = ids(&["grn:acme/iam/policy/a", "grn:acme/iam/policy/b"]);
    let backward = ids(&["grn:acme/iam/policy/b", "grn:acme/iam/policy/a"]);

    assert_eq!(
        recorder.deny(&denial(LEE, &forward)).await,
        Recorded::Stored
    );
    assert_eq!(
        recorder.deny(&denial(LEE, &backward)).await,
        Recorded::Collapsed
    );
    // The control: a different subject is a different denial.
    assert_eq!(
        recorder
            .deny(&denial("grn:acme/iam/user/kim", &forward))
            .await,
        Recorded::Stored
    );

    let all = records(&recorder, ACME).await;
    assert_eq!(all.len(), 2);
    let lee = all
        .iter()
        .find(|r| r.subject.as_deref() == Some(LEE))
        .unwrap();
    assert_eq!(lee.count, 2);
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_denial_of_an_organization_the_store_does_not_hold_is_omitted_and_counted(pool: PgPool) {
    hold(&pool, ACME).await;
    let recorder = Recorder::new(pool);
    let none = ids(&[]);
    let foreign = Denial {
        resource: "grn:globex/iam/agent/bot",
        ..denial(LEE, &none)
    };

    assert_eq!(recorder.deny(&foreign).await, Recorded::Omitted);
    assert_eq!(recorder.omitted(), 1);
    assert!(records(&recorder, GLOBEX).await.is_empty());
    // The control: the held organization's denial is recorded.
    assert_eq!(recorder.deny(&denial(LEE, &none)).await, Recorded::Stored);
    assert_eq!(recorder.omitted(), 1);
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn past_the_cap_a_new_denial_is_counted_in_the_overflow_and_a_matching_one_collapses(
    pool: PgPool,
) {
    hold(&pool, ACME).await;
    let recorder = Recorder::new(pool.clone());
    let none = ids(&[]);
    assert_eq!(recorder.deny(&denial(LEE, &none)).await, Recorded::Stored);
    sqlx::query(
        "INSERT INTO audit_records
             (org_grn, kind, service, subject, action, resource, outcome,
              policy_ids, identity, bucket)
         SELECT $1, 'denial', 'platform', 'grn:acme/iam/user/u' || n,
                'iam:agent.watch', $2, 'deny', '{}', 'seed' || n, 0
         FROM generate_series(1, $3) AS n",
    )
    .bind(ACME)
    .bind(BOT)
    .bind(MAX_LIVE_DENIALS - 1)
    .execute(&pool)
    .await
    .unwrap();

    assert_eq!(
        recorder.deny(&denial(LEE, &none)).await,
        Recorded::Collapsed
    );
    assert_eq!(
        recorder.deny(&denial("grn:acme/iam/user/kim", &none)).await,
        Recorded::Overflowed
    );
    assert_eq!(
        recorder.deny(&denial("grn:acme/iam/user/ada", &none)).await,
        Recorded::Overflowed
    );

    let page = recorder.list(ACME, None, 1).await.unwrap();
    let [overflow] = page.records.try_into().unwrap();
    assert_eq!(overflow.kind, Kind::DenialOverflow);
    assert_eq!(overflow.count, 2);
    assert!(overflow.subject.is_none());
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_denial_past_the_retention_is_unread_and_swept_and_a_write_of_the_same_age_stays(
    pool: PgPool,
) {
    hold(&pool, ACME).await;
    let recorder = Recorder::new(pool.clone());
    let none = ids(&[]);
    recorder.deny(&denial(LEE, &none)).await;
    let mut transaction = pool.begin().await.unwrap();
    record_write(
        &mut transaction,
        &Origin {
            service: "platform".into(),
            subject: None,
        },
        &Write {
            org: ACME,
            action: "iam:policy.create",
            policy: "grn:acme/iam/policy/p",
            outcome: Outcome::Created,
            from: None,
            to: Some(1),
        },
    )
    .await
    .unwrap();
    transaction.commit().await.unwrap();
    assert_eq!(records(&recorder, ACME).await.len(), 2);

    sqlx::query("UPDATE audit_records SET first_at = now() - interval '91 days', last_at = now() - interval '91 days'")
        .execute(&pool)
        .await
        .unwrap();

    let read = records(&recorder, ACME).await;
    assert_eq!(read.len(), 1, "the read excludes it before any sweep");
    assert_eq!(read[0].kind, Kind::Write);
    assert_eq!(recorder.sweep().await.unwrap(), 1);
    let kept: i64 = sqlx::query_scalar("SELECT count(*) FROM audit_records")
        .fetch_one(&pool)
        .await
        .unwrap();
    assert_eq!(kept, 1);
    assert_eq!(records(&recorder, ACME).await[0].kind, Kind::Write);
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn deleting_an_organization_removes_its_records_and_no_other(pool: PgPool) {
    hold(&pool, ACME).await;
    hold(&pool, GLOBEX).await;
    let recorder = Recorder::new(pool.clone());
    let none = ids(&[]);
    recorder.deny(&denial(LEE, &none)).await;
    recorder
        .deny(&Denial {
            resource: "grn:globex/iam/agent/bot",
            ..denial("grn:globex/iam/user/gil", &none)
        })
        .await;

    sqlx::query(
        "UPDATE entities SET document = NULL
         WHERE entity_type = 'Iam::Org' AND entity_id = $1",
    )
    .bind(ACME)
    .execute(&pool)
    .await
    .unwrap();

    assert!(records(&recorder, ACME).await.is_empty());
    assert_eq!(records(&recorder, GLOBEX).await.len(), 1);
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_denial_that_cannot_be_recorded_is_logged_counted_and_goes_no_further(pool: PgPool) {
    hold(&pool, ACME).await;
    let recorder = Recorder::new(pool.clone());
    let none = ids(&[]);
    assert_eq!(recorder.deny(&denial(LEE, &none)).await, Recorded::Stored);
    assert_eq!(recorder.lost(), 0);

    sqlx::query("ALTER TABLE audit_records ADD CONSTRAINT refuse CHECK (false) NOT VALID")
        .execute(&pool)
        .await
        .unwrap();

    assert_eq!(
        recorder.deny(&denial("grn:acme/iam/user/kim", &none)).await,
        Recorded::Lost
    );
    assert_eq!(recorder.lost(), 1);
}
