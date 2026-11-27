//! Migration 000004's withdrawal of `iam:cluster.declare` from the
//! stored policies (ADR-0193), against Postgres: `make -C policy
//! stack-up suites`.

#![cfg(test)]

use sqlx::PgPool;

const WITHDRAWAL: &str =
    include_str!("../../../migrations/000004_cluster_declare_withdrawn.up.sql");

/// Stores `grn` with one version per source, the last the default, as
/// an earlier schema admitted them: no write today admits the action.
async fn stored(pool: &PgPool, grn: &str, sources: &[&str]) {
    let mut tx = pool.begin().await.unwrap();
    sqlx::query(
        "INSERT INTO policies (grn, org_grn, name, status, default_version)
         VALUES ($1, 'grn:root/iam/org/acme', 'p', 'active', $2)",
    )
    .bind(grn)
    .bind(i32::try_from(sources.len()).unwrap())
    .execute(&mut *tx)
    .await
    .unwrap();
    for (n, source) in sources.iter().enumerate() {
        sqlx::query(
            "INSERT INTO policy_versions (policy_grn, version, body, created_by)
             VALUES ($1, $2, jsonb_build_object('kind', 'permit', 'source', $3::text,
                     'org', 'grn:root/iam/org/acme', 'writer', 'grn:root/iam/user/owner'),
                     'grn:root/iam/user/owner')",
        )
        .bind(grn)
        .bind(i32::try_from(n + 1).unwrap())
        .bind(*source)
        .execute(&mut *tx)
        .await
        .unwrap();
    }
    tx.commit().await.unwrap();
}

fn permit(actions: &str) -> String {
    format!(
        r#"permit (principal, action {actions}, resource in Iam::Org::"grn:root/iam/org/acme");"#
    )
}

/// Every policy any version of which names the action goes, whether it
/// names it alone, in a list beside another action, spaced inside its
/// name, or only in a version a rollback would bring back. A policy
/// naming another action alone, and one naming an action whose name
/// only begins the same, are the controls.
#[sqlx::test(migrations = "../../migrations")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn the_withdrawal_takes_every_policy_naming_cluster_declare(pool: PgPool) {
    let declare = r#"Iam::Action::"cluster.declare""#;
    let read = r#"Iam::Action::"org.read""#;
    stored(
        &pool,
        "grn:acme/iam/policy/alone",
        &[&permit(&format!("== {declare}"))],
    )
    .await;
    stored(
        &pool,
        "grn:acme/iam/policy/listed",
        &[&permit(&format!("in [{read}, {declare}]"))],
    )
    .await;
    stored(
        &pool,
        "grn:acme/iam/policy/spaced",
        &[&permit(r#"== Iam :: Action :: "cluster.declare""#)],
    )
    .await;
    stored(
        &pool,
        "grn:acme/iam/policy/earlier",
        &[
            &permit(&format!("== {declare}")),
            &permit(&format!("== {read}")),
        ],
    )
    .await;
    stored(
        &pool,
        "grn:acme/iam/policy/another",
        &[&permit(&format!("== {read}"))],
    )
    .await;
    stored(
        &pool,
        "grn:acme/iam/policy/prefixed",
        &[&permit(r#"== Iam::Action::"cluster.declared""#)],
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
            "grn:acme/iam/policy/prefixed"
        ]
    );
    let versions: i64 = sqlx::query_scalar("SELECT count(*) FROM policy_versions")
        .fetch_one(&pool)
        .await
        .unwrap();
    assert_eq!(versions, 2, "the versions went with their policies");
}
