//! Migration 000004's withdrawal of `iam:cluster.declare` from the
//! stored permits (ADR-0193), against Postgres: `make -C policy
//! stack-up suites`.

#![cfg(test)]

use sqlx::PgPool;

const WITHDRAWAL: &str =
    include_str!("../../../migrations/000004_cluster_declare_withdrawn.up.sql");

const ACME: &str = "grn:root/iam/org/acme";

/// Stores `grn` with one version per source, the last the default, as
/// an earlier schema admitted them: no write today admits the action.
/// `kind` is `permit` or `guardrail`.
async fn stored(pool: &PgPool, grn: &str, kind: &str, sources: &[&str]) {
    let mut tx = pool.begin().await.unwrap();
    sqlx::query(
        "INSERT INTO policies (grn, org_grn, name, status, default_version)
         VALUES ($1, $2, 'p', 'active', $3)",
    )
    .bind(grn)
    .bind(ACME)
    .bind(i32::try_from(sources.len()).unwrap())
    .execute(&mut *tx)
    .await
    .unwrap();
    for (n, source) in sources.iter().enumerate() {
        sqlx::query(
            "INSERT INTO policy_versions (policy_grn, version, body, created_by)
             VALUES ($1, $2, jsonb_build_object('kind', $3::text, 'source', $4::text,
                     'org', $5::text, 'writer', 'grn:acme/iam/user/owner'),
                     'grn:acme/iam/user/owner')",
        )
        .bind(grn)
        .bind(i32::try_from(n + 1).unwrap())
        .bind(kind)
        .bind(*source)
        .bind(ACME)
        .execute(&mut *tx)
        .await
        .unwrap();
    }
    tx.commit().await.unwrap();
}

fn permit(actions: &str) -> String {
    format!(r#"permit (principal, action {actions}, resource in Iam::Org::"{ACME}");"#)
}

/// Every permit any version of which names the action goes, whether it
/// names it alone, in a list beside another action, spaced inside its
/// name, or only in a version a rollback would bring back, and each
/// removal is recorded for its organization as the service's purge
/// records one. The controls are a permit naming another action alone,
/// one naming an action whose name only begins the same, and a
/// guardrail naming it beside another action, which stays so that
/// action's deny is not lifted.
#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn the_withdrawal_takes_every_permit_naming_cluster_declare(pool: PgPool) {
    sqlx::query(
        "INSERT INTO entities (entity_type, entity_id, sequence, document)
         VALUES ('Iam::Org', $1, 1, '{}'::jsonb)",
    )
    .bind(ACME)
    .execute(&pool)
    .await
    .unwrap();
    let declare = r#"Iam::Action::"cluster.declare""#;
    let read = r#"Iam::Action::"org.read""#;
    let alone = permit(&format!("== {declare}"));
    let listed = permit(&format!("in [{read}, {declare}]"));
    let spaced = permit(r#"== Iam :: Action :: "cluster.declare""#);
    let earlier = [
        permit(&format!("== {declare}")),
        permit(&format!("== {read}")),
    ];
    stored(&pool, "grn:acme/iam/policy/alone", "permit", &[&alone]).await;
    stored(&pool, "grn:acme/iam/policy/listed", "permit", &[&listed]).await;
    stored(&pool, "grn:acme/iam/policy/spaced", "permit", &[&spaced]).await;
    stored(
        &pool,
        "grn:acme/iam/policy/earlier",
        "permit",
        &[&earlier[0], &earlier[1]],
    )
    .await;
    let another = permit(&format!("== {read}"));
    stored(&pool, "grn:acme/iam/policy/another", "permit", &[&another]).await;
    let prefixed = permit(r#"== Iam::Action::"cluster.declared""#);
    stored(
        &pool,
        "grn:acme/iam/policy/prefixed",
        "permit",
        &[&prefixed],
    )
    .await;
    let guardrail = format!(
        r#"forbid (principal, action in [{read}, {declare}], resource in Iam::Org::"{ACME}");"#
    );
    stored(
        &pool,
        "grn:acme/iam/policy/guardrail",
        "guardrail",
        &[&guardrail],
    )
    .await;

    sqlx::raw_sql(WITHDRAWAL).execute(&pool).await.unwrap();

    let left: Vec<String> = sqlx::query_scalar("SELECT grn FROM policies ORDER BY grn")
        .fetch_all(&pool)
        .await
        .unwrap();
    assert_eq!(
        left,
        [
            "grn:acme/iam/policy/another",
            "grn:acme/iam/policy/guardrail",
            "grn:acme/iam/policy/prefixed"
        ]
    );
    let versions: i64 = sqlx::query_scalar("SELECT count(*) FROM policy_versions")
        .fetch_one(&pool)
        .await
        .unwrap();
    assert_eq!(versions, 3, "the versions went with their policies");
    let recorded: Vec<(String, String, String, String)> = sqlx::query_as(
        "SELECT org_grn, action, resource, outcome FROM audit_records
         WHERE kind = 'write' ORDER BY resource",
    )
    .fetch_all(&pool)
    .await
    .unwrap();
    let removal = |grn: &str| {
        (
            ACME.to_owned(),
            "iam:policy.delete".to_owned(),
            grn.to_owned(),
            "removed".to_owned(),
        )
    };
    assert_eq!(
        recorded,
        [
            removal("grn:acme/iam/policy/alone"),
            removal("grn:acme/iam/policy/earlier"),
            removal("grn:acme/iam/policy/listed"),
            removal("grn:acme/iam/policy/spaced"),
        ],
        "each removal is recorded, and nothing else"
    );
}
