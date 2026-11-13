//! The reasons a decision carries, as the platform reads them to
//! explain a permission (#1906): the policy ids that decided, by
//! `Evaluate` and by one entry of `Evaluations`, against the graph
//! `main` runs.

use super::*;

const ZED: Named = ("Iam::User", "grn:acme/iam/user/zed");
const DUO: Named = ("Iam::User", "grn:acme/iam/user/duo");
const ENG: Named = ("Iam::Team", "grn:acme/iam/group/eng");
const RIVAL: Named = ("Iam::Agent", "grn:globex/iam/agent/rival");
const ISOLATION: &str = "tenant-isolation";

/// The decision and the reasons of one answer, ordered, since the
/// order a decision names them in is not stated.
#[derive(Debug, PartialEq)]
struct Answer {
    allowed: bool,
    policies: Vec<String>,
    errors: Vec<String>,
}

fn answer(decision: pb::EvaluateResponse) -> Answer {
    let reasons = decision.context.unwrap_or_default().reasons;
    let mut policies: Vec<String> = reasons.iter().map(|r| r.policy_id.clone()).collect();
    policies.sort();
    Answer {
        allowed: decision.decision,
        policies,
        errors: reasons.into_iter().map(|r| r.error).collect(),
    }
}

fn org() -> OrgGrn {
    OrgGrn::new(ACME)
}

fn slug(grn: &str) -> &str {
    grn.rsplit('/').next().unwrap()
}

fn member_link(who: Named) -> String {
    grant::link_policy(&org(), Role::Member, who.1)
}

fn sorted<const N: usize>(ids: [&str; N]) -> Vec<String> {
    let mut ids: Vec<String> = ids.iter().map(|id| (*id).to_owned()).collect();
    ids.sort();
    ids
}

impl Platform {
    /// The answer of `Evaluate` and of the one entry of `Evaluations`
    /// that states only the subject, which must be the same.
    async fn reasons(&mut self, who: Named, action: &str, on: Named) -> Answer {
        let resource = Resource {
            r#type: on.0.into(),
            id: on.1.into(),
            properties: None,
        };
        let action = Action {
            name: action.into(),
        };
        let single = self.request(EvaluateRequest {
            subject: subject(who),
            action: Some(action.clone()),
            resource: Some(resource.clone()),
            context: None,
            read: 0,
            probe: true,
        });
        let single = answer(self.grpc.evaluate(single).await.unwrap().into_inner());
        let batch = self.request(pb::EvaluationsRequest {
            subject: None,
            action: Some(action),
            resource: Some(resource),
            context: None,
            read: 0,
            evaluations: vec![pb::Evaluation {
                subject: subject(who),
                ..pb::Evaluation::default()
            }],
            probe: true,
        });
        let [entry]: [pb::EvaluateResponse; 1] = self
            .grpc
            .evaluations(batch)
            .await
            .unwrap()
            .into_inner()
            .evaluations
            .try_into()
            .unwrap();
        assert_eq!(answer(entry), single, "Evaluations differs from Evaluate");
        single
    }

    /// The platform writes a member's link, as it does for a member.
    async fn links(&mut self, who: Named) {
        let slug = slug(&member_link(who)).to_owned();
        self.create(None, &slug, role_link(Role::Member, who))
            .await
            .unwrap();
        self.activate(None, &slug).await.unwrap();
    }

    async fn writes(&mut self, writer: Named, slug: &str, body: PolicyBody) -> String {
        self.create(Some(writer), slug, body).await.unwrap();
        self.activate(Some(writer), slug).await.unwrap();
        policy_of(slug)
    }
}

async fn with_users(pool: &PgPool) {
    seed(pool).await;
    let engine = Engine::load().unwrap();
    let changes = [
        upsert(&engine, ZED, None, None, &[MEMBERS]),
        upsert(&engine, ENG, Some(ORG), None, &[ORG]),
        upsert(&engine, DUO, None, None, &[MEMBERS, ENG]),
        upsert(
            &engine,
            RIVAL,
            Some(GLOBEX_ORG),
            None,
            &[GLOBEX_MEMBERS, GLOBEX_ORG],
        ),
    ];
    EntityStore::new(pool.clone(), engine)
        .apply(&changes)
        .await
        .unwrap();
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn an_allow_by_a_members_link_names_the_identifier_the_platform_stores_it_under(
    pool: PgPool,
) {
    with_users(&pool).await;
    let mut service = platform(pool).await;

    // Control: before the link, the member is denied by nothing.
    let before = service.reasons(ZED, "iam:agent.read", BOT).await;
    assert_eq!((before.allowed, before.policies), (false, vec![]));

    service.links(ZED).await;
    let allowed = service.reasons(ZED, "iam:agent.read", BOT).await;
    assert!(allowed.allowed, "{allowed:?}");
    assert_eq!(allowed.policies, [member_link(ZED)]);
    assert_eq!(allowed.errors, [""]);

    // The identifier is the platform's naming, held to a value.
    assert_eq!(
        grant::link_policy(&org(), Role::Admin, LEE.1),
        format!("grn:acme/iam/policy/{LEE_ADMIN}")
    );
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn an_allow_by_a_scoped_link_names_the_identifier_the_platform_stores_it_under(pool: PgPool) {
    with_users(&pool).await;
    let mut service = platform(pool).await;

    // Control: a member of the team holds nothing on it yet.
    let before = service.reasons(DUO, "iam:group.read", ENG).await;
    assert_eq!((before.allowed, before.policies), (false, vec![]));

    let id = grant::scoped_link_policy(&org(), Role::Member, ENG, ENG);
    let body = PolicyBody {
        body: Some(Wire::RoleLink(pb::RoleLink {
            role: pb::Role::Member.into(),
            scope: pb::RoleScope::TeamOrProject.into(),
            principal: entity(ENG),
            resource: entity(ENG),
        })),
    };
    service.create(None, slug(&id), body).await.unwrap();
    service.activate(None, slug(&id)).await.unwrap();

    let allowed = service.reasons(DUO, "iam:group.read", ENG).await;
    assert!(allowed.allowed, "{allowed:?}");
    assert_eq!(allowed.policies, [id]);

    // The identifier is the platform's naming, held to ADR-0163's value.
    assert_eq!(
        grant::scoped_link_policy(&org(), Role::Admin, ENG, ENG),
        "grn:acme/iam/policy/link-scoped-admin-057a36867e8b5d54bdfba284ee2f3a02"
    );
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn an_allow_two_policies_permit_names_both_in_no_stated_order(pool: PgPool) {
    with_users(&pool).await;
    let mut service = platform(pool).await;
    service.links(ZED).await;
    let alone = service.reasons(ZED, "iam:agent.read", BOT).await;
    assert_eq!(alone.policies, [member_link(ZED)]);

    let permit_source = r#"permit (
    principal == Iam::User::"grn:acme/iam/user/zed",
    action == Iam::Action::"agent.read",
    resource == Iam::Agent::"grn:acme/iam/agent/bot"
);"#;
    let permitted = service
        .writes(OWEN, "zed-reads-bot", permit(permit_source))
        .await;

    let both = service.reasons(ZED, "iam:agent.read", BOT).await;
    assert!(both.allowed, "{both:?}");
    assert_eq!(
        both.policies,
        sorted([member_link(ZED).as_str(), permitted.as_str()])
    );
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_deny_by_a_guardrail_names_the_guardrail_and_not_the_link_it_overrode(pool: PgPool) {
    with_users(&pool).await;
    let mut service = platform(pool).await;
    service.links(ZED).await;
    let before = service.reasons(ZED, "iam:agent.read", BOT).await;
    assert_eq!(before.policies, [member_link(ZED)], "control: allowed");

    let guard = service
        .writes(ADA, "nobody-reads-bot", guardrail(NO_ONE_READS_BOT))
        .await;
    let denied = service.reasons(ZED, "iam:agent.read", BOT).await;
    assert!(!denied.allowed, "{denied:?}");
    assert_eq!(denied.policies, [guard]);
    assert_eq!(denied.errors, [""]);
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_deny_by_tenant_isolation_names_it_and_not_the_link_the_other_organization_holds(
    pool: PgPool,
) {
    with_users(&pool).await;
    let mut service = platform(pool).await;
    // The same link, written for a member of acme and for one of
    // globex, so that only isolation tells them apart.
    service.links(ZED).await;
    service.links(GIL).await;

    let member = service.reasons(ZED, "iam:agent.read", BOT).await;
    assert_eq!(member.policies, [member_link(ZED)], "control: allowed");

    let outsider = service.reasons(GIL, "iam:agent.read", BOT).await;
    assert!(!outsider.allowed, "{outsider:?}");
    assert_eq!(outsider.policies, [ISOLATION]);
    assert_eq!(outsider.errors, [""]);
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_default_deny_names_no_policy(pool: PgPool) {
    with_users(&pool).await;
    let mut service = platform(pool).await;

    // Control: the owner is allowed the action, by their link.
    let owner = service.reasons(OWEN, "iam:org.delete", ORG).await;
    assert!(owner.allowed, "{owner:?}");
    assert_eq!(owner.policies, [policy_of("owen")]);

    service.links(ZED).await;
    let member = service.reasons(ZED, "iam:org.delete", ORG).await;
    assert_eq!(
        member,
        Answer {
            allowed: false,
            policies: vec![],
            errors: vec![],
        }
    );
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_fault_is_a_deny_naming_only_what_failed_and_a_refused_entry_names_no_policy(
    pool: PgPool,
) {
    with_users(&pool).await;
    let mut service = platform(pool).await;
    service.links(ZED).await;
    let ghost = ("Iam::Agent", "grn:acme/iam/agent/ghost");

    // A resource the store lacks is denied by isolation erroring.
    let missing = service.reasons(ZED, "iam:agent.read", ghost).await;
    assert!(!missing.allowed, "{missing:?}");
    assert!(missing.errors.iter().any(|e| !e.is_empty()), "{missing:?}");

    // A refused entry is a deny with one reason naming no policy.
    let batch = service.request(pb::EvaluationsRequest {
        subject: subject(ZED),
        action: Some(Action {
            name: "iam:agent.read".into(),
        }),
        resource: Some(Resource {
            r#type: BOT.0.into(),
            id: BOT.1.into(),
            properties: None,
        }),
        context: None,
        read: 0,
        evaluations: vec![
            pb::Evaluation::default(),
            pb::Evaluation {
                action: Some(Action {
                    name: "iam:no-such-action".into(),
                }),
                ..pb::Evaluation::default()
            },
        ],
        probe: true,
    });
    let answers = service.grpc.evaluations(batch).await.unwrap().into_inner();
    let [fine, bad] = answers.evaluations.try_into().unwrap();
    let (fine, bad) = (answer(fine), answer(bad));
    assert_eq!(fine.policies, [member_link(ZED)], "control: allowed");
    assert!(!bad.allowed);
    assert_eq!(bad.policies, [""]);
    assert!(!bad.errors[0].is_empty(), "{bad:?}");
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_deny_by_the_senders_guardrail_across_organizations_names_it_under_its_organization(
    pool: PgPool,
) {
    with_users(&pool).await;
    let engine = Engine::load().unwrap();
    let consent = |org: Named, to: Named, members: Named| {
        EntityChange::upsert(
            &engine,
            uid(org),
            2,
            Upsert {
                org: Some(uid(org)),
                members: Some(uid(members)),
                consents: vec![uid(to)],
                ..Upsert::default()
            },
        )
        .unwrap()
    };
    EntityStore::new(pool.clone(), engine.clone())
        .apply(&[
            consent(ORG, GLOBEX_ORG, MEMBERS),
            consent(GLOBEX_ORG, ORG, GLOBEX_MEMBERS),
        ])
        .await
        .unwrap();
    let mut service = platform(pool).await;

    // Control: both consent, so the send is allowed by the
    // system-tier permit.
    let sent = service.reasons(BOT, "iam:message.send", RIVAL).await;
    assert!(sent.allowed, "{sent:?}");
    assert_eq!(sent.policies, ["agent-message-send"]);

    let source = r#"forbid (
    principal == Iam::Agent::"grn:acme/iam/agent/bot",
    action == Iam::Action::"message.send",
    resource
);"#;
    let guard = service
        .writes(ADA, "bot-stays-home", guardrail(source))
        .await;
    let denied = service.reasons(BOT, "iam:message.send", RIVAL).await;
    assert!(!denied.allowed, "{denied:?}");
    assert_eq!(denied.policies, [format!("{ACME}/{guard}")]);
}
