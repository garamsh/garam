//! The identifiers the platform writes a member's role link under are
//! its alone, and it makes effective or removes only what it wrote
//! there (ADR-0161), against the graph `main` runs.

use super::audit_cases::of_kind;
use super::*;

fn org() -> OrgGrn {
    OrgGrn::new(ACME)
}

/// The slug of the identifier the platform names `who`'s link of
/// `role` by.
fn slug_of(role: Role, who: Named) -> String {
    grant::link_policy(&org(), role, who.1)
        .rsplit('/')
        .next()
        .unwrap()
        .to_owned()
}

/// A policy stored without the service, as one that predates the
/// reservation was.
async fn stored(pool: &PgPool, slug: &str, body: Body) {
    PolicyStore::new(pool.clone())
        .create(NewPolicy {
            grn: &policy_of(slug),
            org: &org(),
            name: "predates",
            body,
            created_by: "test",
        })
        .await
        .unwrap();
}

fn stored_link(role: Role, who: Named) -> Body {
    Body::role_link(
        &Engine::load().unwrap(),
        &org(),
        role,
        RoleScope::Organization,
        uid(who),
        uid(ORG),
    )
    .unwrap()
}

fn stored_guardrail() -> Body {
    Body::guardrail(&Engine::load().unwrap(), &org(), NO_ONE_READS_BOT).unwrap()
}

async fn policies(service: &mut Platform) -> Vec<String> {
    service
        .list(Some(OWEN), ACME)
        .await
        .unwrap()
        .into_iter()
        .map(|policy| policy.policy)
        .collect()
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn the_platform_alone_creates_a_members_link_under_the_identifier_that_names_it(
    pool: PgPool,
) {
    seed(&pool).await;
    let mut service = platform(pool).await;
    let before = policies(&mut service).await;

    // Control: the link the identifier names is written, made
    // effective and removed.
    service
        .create(None, LEE_ADMIN, role_link(Role::Admin, LEE))
        .await
        .unwrap();
    service.activate(None, LEE_ADMIN).await.unwrap();
    service.delete(None, LEE_ADMIN).await.unwrap();
    // Control: a "link-" identifier no role prefix opens is free.
    service
        .create(None, "link-x", role_link(Role::Member, KIM))
        .await
        .unwrap();

    let refused_bodies = [
        // Another role than the identifier's.
        (LEE_ADMIN.to_owned(), role_link(Role::Member, LEE)),
        // Another member than the identifier's.
        (LEE_ADMIN.to_owned(), role_link(Role::Admin, KIM)),
        // A guardrail and a permit are no link.
        (LEE_ADMIN.to_owned(), guardrail(NO_ONE_WATCHES_BOT)),
        (LEE_ADMIN.to_owned(), permit(LEE_WATCHES_BOT)),
        // The identifier of another role than the one this link holds.
        (slug_of(Role::Owner, LEE), role_link(Role::Admin, LEE)),
        // A tail that is no member's hash.
        (
            "link-admin-fadf4f48".to_owned(),
            role_link(Role::Admin, LEE),
        ),
        ("link-member-".to_owned(), role_link(Role::Member, KIM)),
    ];
    for (slug, body) in refused_bodies {
        refused(
            service.create(None, &slug, body).await,
            Code::PermissionDenied,
        );
    }

    let mut added = policies(&mut service).await;
    added.retain(|policy| !before.contains(policy));
    assert_eq!(added, [policy_of("link-x")], "nothing refused was stored");
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_writer_changes_nothing_under_a_reserved_identifier_not_even_the_link_it_names(
    pool: PgPool,
) {
    seed(&pool).await;
    let lee_member = slug_of(Role::Member, LEE);
    let kim_member = slug_of(Role::Member, KIM);
    stored(&pool, &lee_member, stored_link(Role::Member, LEE)).await;
    let mut service = platform(pool).await;

    for writer in [OWEN, ADA] {
        let who = Some(writer);
        refused(
            service
                .create(who, &kim_member, role_link(Role::Member, KIM))
                .await,
            Code::PermissionDenied,
        );
        refused(
            service
                .append(who, &lee_member, role_link(Role::Member, LEE))
                .await,
            Code::PermissionDenied,
        );
        refused(
            service.activate(who, &lee_member).await,
            Code::PermissionDenied,
        );
        refused(
            service.roll_back(who, &lee_member, 1).await,
            Code::PermissionDenied,
        );
        refused(
            service.delete(who, &lee_member).await,
            Code::PermissionDenied,
        );
    }
    let found = service
        .get(Some(OWEN), ACME, &policy_of(&lee_member))
        .await
        .unwrap();
    assert_eq!(found.versions.len(), 1, "no version was appended");
    assert!(
        !policies(&mut service)
            .await
            .contains(&policy_of(&kim_member)),
        "no link was created"
    );

    // Control: an owner writes beside it, and the platform, which
    // wrote it, makes it effective and removes it.
    service
        .create(Some(OWEN), "link-x", role_link(Role::Member, KIM))
        .await
        .unwrap();
    service.activate(None, &lee_member).await.unwrap();
    service.delete(None, &lee_member).await.unwrap();
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn the_platform_makes_effective_or_removes_only_a_policy_whose_every_version_is_the_members_link(
    pool: PgPool,
) {
    seed(&pool).await;
    let store = PolicyStore::new(pool.clone());

    // A guardrail that forbids what the member's link would allow,
    // and links that are not this member's.
    let guard = slug_of(Role::Admin, LEE);
    stored(&pool, &guard, stored_guardrail()).await;
    let other_role = slug_of(Role::Admin, KIM);
    stored(&pool, &other_role, stored_link(Role::Member, KIM)).await;
    let other_member = slug_of(Role::Member, KIM);
    stored(&pool, &other_member, stored_link(Role::Member, LEE)).await;
    // The link, then a second version that is not.
    let later = slug_of(Role::Member, ADA);
    stored(&pool, &later, stored_link(Role::Member, ADA)).await;
    store
        .append_version(
            &org(),
            &policy_of(&later),
            &stored_link(Role::Admin, ADA),
            "test",
        )
        .await
        .unwrap();
    // Control: the link itself, inactive.
    let own = slug_of(Role::Owner, KIM);
    stored(&pool, &own, stored_link(Role::Owner, KIM)).await;

    let mut service = platform(pool).await;
    assert!(service.asks(LEE, "iam:agent.read", true).await);
    let writes = [
        (&guard, role_link(Role::Admin, LEE)),
        (&other_role, role_link(Role::Admin, KIM)),
        (&other_member, role_link(Role::Member, KIM)),
        (&later, role_link(Role::Member, ADA)),
    ];
    for (slug, link) in writes {
        // The platform's create of the link finds the policy there,
        // which it tolerates, and goes on to activate it.
        refused(service.create(None, slug, link).await, Code::AlreadyExists);
        refused(service.activate(None, slug).await, Code::PermissionDenied);
        refused(service.delete(None, slug).await, Code::PermissionDenied);
    }
    // The guardrail was not made effective as the member's link, and
    // nothing was removed.
    assert!(
        service.asks(LEE, "iam:agent.read", true).await,
        "the guardrail is not in force"
    );
    let found = policies(&mut service).await;
    for slug in [&guard, &other_role, &other_member, &later] {
        assert!(found.contains(&policy_of(slug)), "{slug} was removed");
    }

    service.activate(None, &own).await.unwrap();
    service.delete(None, &own).await.unwrap();
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_writers_refusal_under_a_reserved_identifier_is_a_denial_and_the_platforms_is_not(
    pool: PgPool,
) {
    seed(&pool).await;
    let mut service = platform(pool).await;
    let denials = |records| of_kind(records, pb::AuditRecordKind::Denial);

    refused(
        service
            .create(Some(OWEN), LEE_ADMIN, role_link(Role::Admin, LEE))
            .await,
        Code::PermissionDenied,
    );
    let recorded = denials(service.audit(Some(OWEN), ACME).await.unwrap());
    let [denial] = recorded
        .iter()
        .filter(|r| r.action == "iam:policy.attach")
        .collect::<Vec<_>>()
        .try_into()
        .unwrap();
    assert_eq!(denial.subject, OWEN.1);
    assert_eq!(denial.resource, ACME);

    // The platform's refusal names no writer to record.
    let before = recorded.len();
    refused(
        service
            .create(None, LEE_ADMIN, role_link(Role::Member, LEE))
            .await,
        Code::PermissionDenied,
    );
    let after = denials(service.audit(Some(OWEN), ACME).await.unwrap());
    assert_eq!(after.len(), before, "{after:?}");
}
