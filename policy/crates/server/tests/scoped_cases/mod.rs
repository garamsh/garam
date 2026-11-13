//! The identifiers the platform writes the scoped link of a team or a
//! project under are its alone, and it makes effective or removes
//! only what it wrote there (ADR-0163), against the graph `main`
//! runs.

use super::audit_cases::of_kind;
use super::*;

const ENG: Named = ("Iam::Team", "grn:acme/iam/group/eng");
const OPS: Named = ("Iam::Team", "grn:acme/iam/group/ops");
const WEB: Named = ("Iam::Project", "grn:acme/iam/project/web");

fn org() -> OrgGrn {
    OrgGrn::new(ACME)
}

/// The slug of the identifier the platform names `held`'s link of
/// `role` on `on` by.
fn slug_of(role: Role, held: Named, on: Named) -> String {
    grant::scoped_link_policy(&org(), role, held, on)
        .rsplit('/')
        .next()
        .unwrap()
        .to_owned()
}

/// A team's or a project's role on itself, as the platform's group
/// and project roles are.
fn own_role(role: Role, who: Named) -> PolicyBody {
    scoped(role, who, who)
}

fn scoped(role: Role, principal: Named, resource: Named) -> PolicyBody {
    PolicyBody {
        body: Some(Wire::RoleLink(pb::RoleLink {
            role: match role {
                Role::Owner => pb::Role::Owner,
                Role::Admin => pb::Role::Admin,
                Role::Member => pb::Role::Member,
            }
            .into(),
            scope: pb::RoleScope::TeamOrProject.into(),
            principal: entity(principal),
            resource: entity(resource),
        })),
    }
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

fn stored_scoped(role: Role, who: Named) -> Body {
    Body::role_link(
        &Engine::load().unwrap(),
        &org(),
        role,
        RoleScope::TeamOrProject,
        uid(who),
        uid(who),
    )
    .unwrap()
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
async fn the_platform_alone_creates_a_teams_or_projects_link_under_the_identifier_that_names_it(
    pool: PgPool,
) {
    seed(&pool).await;
    let mut service = platform(pool).await;
    let before = policies(&mut service).await;

    // Control: the link the identifier names is written, written
    // again, made effective and removed, in a team's scope and in a
    // project's.
    for (role, who) in [(Role::Admin, ENG), (Role::Member, WEB)] {
        let slug = slug_of(role, who, who);
        service
            .create(None, &slug, own_role(role, who))
            .await
            .unwrap();
        refused(
            service.create(None, &slug, own_role(role, who)).await,
            Code::AlreadyExists,
        );
        service.activate(None, &slug).await.unwrap();
        service.activate(None, &slug).await.unwrap();
        service.delete(None, &slug).await.unwrap();
    }
    // Control: a "link-" identifier no prefix opens is free.
    service
        .create(None, "link-x", own_role(Role::Member, ENG))
        .await
        .unwrap();

    let admin_of_eng = slug_of(Role::Admin, ENG, ENG);
    let refused_bodies = [
        // Another role than the identifier's.
        (admin_of_eng.clone(), own_role(Role::Member, ENG)),
        // Another team or project than the identifier's.
        (admin_of_eng.clone(), own_role(Role::Admin, OPS)),
        (admin_of_eng.clone(), own_role(Role::Admin, WEB)),
        // A team holding the role on another resource than itself.
        (
            slug_of(Role::Admin, ENG, OPS),
            scoped(Role::Admin, ENG, OPS),
        ),
        (
            slug_of(Role::Admin, ENG, WEB),
            scoped(Role::Admin, ENG, WEB),
        ),
        // What a person writes through the console: a user or an
        // agent holds the role on a team.
        (
            slug_of(Role::Admin, LEE, ENG),
            scoped(Role::Admin, LEE, ENG),
        ),
        (
            slug_of(Role::Admin, BOT, ENG),
            scoped(Role::Admin, BOT, ENG),
        ),
        // A guardrail and a permit are no link.
        (admin_of_eng.clone(), guardrail(NO_ONE_WATCHES_BOT)),
        (admin_of_eng.clone(), permit(LEE_WATCHES_BOT)),
        // A member's link is not a scoped link, nor the other way.
        (admin_of_eng.clone(), role_link(Role::Admin, LEE)),
        (LEE_ADMIN.to_owned(), own_role(Role::Admin, ENG)),
        // A tail that is no hash of these fields.
        (
            "link-scoped-admin-fadf4f48".to_owned(),
            own_role(Role::Admin, ENG),
        ),
        ("link-scoped-".to_owned(), own_role(Role::Admin, ENG)),
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
async fn a_writer_changes_nothing_under_a_scoped_identifier_not_even_the_link_it_names(
    pool: PgPool,
) {
    seed(&pool).await;
    let eng_admin = slug_of(Role::Admin, ENG, ENG);
    let web_member = slug_of(Role::Member, WEB, WEB);
    stored(&pool, &eng_admin, stored_scoped(Role::Admin, ENG)).await;
    let mut service = platform(pool).await;

    for writer in [OWEN, ADA] {
        let who = Some(writer);
        refused(
            service
                .create(who, &web_member, own_role(Role::Member, WEB))
                .await,
            Code::PermissionDenied,
        );
        refused(
            service
                .append(who, &eng_admin, own_role(Role::Admin, ENG))
                .await,
            Code::PermissionDenied,
        );
        refused(
            service.activate(who, &eng_admin).await,
            Code::PermissionDenied,
        );
        refused(
            service.roll_back(who, &eng_admin, 1).await,
            Code::PermissionDenied,
        );
        refused(
            service.delete(who, &eng_admin).await,
            Code::PermissionDenied,
        );
    }
    let found = service
        .get(Some(OWEN), ACME, &policy_of(&eng_admin))
        .await
        .unwrap();
    assert_eq!(found.versions.len(), 1, "no version was appended");
    assert!(
        !policies(&mut service)
            .await
            .contains(&policy_of(&web_member)),
        "no link was created"
    );

    // Control: an owner writes the same body beside it, and the
    // platform, which wrote the link, makes it effective and removes
    // it.
    service
        .create(Some(OWEN), "link-x", own_role(Role::Admin, ENG))
        .await
        .unwrap();
    service.activate(None, &eng_admin).await.unwrap();
    service.delete(None, &eng_admin).await.unwrap();
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn the_platform_makes_effective_or_removes_only_a_policy_whose_every_version_is_the_scoped_link(
    pool: PgPool,
) {
    seed(&pool).await;
    let store = PolicyStore::new(pool.clone());

    // A guardrail and a link of another role under identifiers of
    // the scoped form.
    let guard = slug_of(Role::Admin, ENG, ENG);
    stored(&pool, &guard, {
        Body::guardrail(&Engine::load().unwrap(), &org(), NO_ONE_READS_BOT).unwrap()
    })
    .await;
    let other_role = slug_of(Role::Owner, ENG, ENG);
    stored(&pool, &other_role, stored_scoped(Role::Member, ENG)).await;
    // The link, then a second version that is not.
    let later = slug_of(Role::Member, WEB, WEB);
    stored(&pool, &later, stored_scoped(Role::Member, WEB)).await;
    store
        .append_version(
            &org(),
            &policy_of(&later),
            &stored_scoped(Role::Admin, WEB),
            "test",
        )
        .await
        .unwrap();
    // Control: the link itself, inactive.
    let own = slug_of(Role::Owner, WEB, WEB);
    stored(&pool, &own, stored_scoped(Role::Owner, WEB)).await;

    let mut service = platform(pool).await;
    assert!(service.asks(LEE, "iam:agent.read", true).await);
    let writes = [
        (&guard, own_role(Role::Admin, ENG)),
        (&other_role, own_role(Role::Owner, ENG)),
        (&later, own_role(Role::Member, WEB)),
    ];
    for (slug, link) in writes {
        // The platform's create of the link finds the policy there,
        // which it tolerates, and goes on to activate it.
        refused(service.create(None, slug, link).await, Code::AlreadyExists);
        refused(service.activate(None, slug).await, Code::PermissionDenied);
        refused(service.delete(None, slug).await, Code::PermissionDenied);
    }
    assert!(
        service.asks(LEE, "iam:agent.read", true).await,
        "the guardrail is not in force"
    );
    let found = policies(&mut service).await;
    for slug in [&guard, &other_role, &later] {
        assert!(found.contains(&policy_of(slug)), "{slug} was removed");
    }

    service.activate(None, &own).await.unwrap();
    service.delete(None, &own).await.unwrap();
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_writers_refusal_under_a_scoped_identifier_is_a_denial_and_the_platforms_is_not(
    pool: PgPool,
) {
    seed(&pool).await;
    let mut service = platform(pool).await;
    let denials = |records| of_kind(records, pb::AuditRecordKind::Denial);
    let eng_admin = slug_of(Role::Admin, ENG, ENG);

    refused(
        service
            .create(Some(OWEN), &eng_admin, own_role(Role::Admin, ENG))
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
            .create(None, &eng_admin, own_role(Role::Member, ENG))
            .await,
        Code::PermissionDenied,
    );
    let after = denials(service.audit(Some(OWEN), ACME).await.unwrap());
    assert_eq!(after.len(), before, "{after:?}");
}
