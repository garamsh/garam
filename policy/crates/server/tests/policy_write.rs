//! An organization's policies written over gRPC (ADR-0146 item 3), over
//! the graph `main` runs, against Postgres: `make -C policy stack-up
//! suites`. The platform writes, naming the writer whose action the
//! service evaluates itself.

#![cfg(test)]

mod audit_cases;
mod caller_tokens;
mod listening;
mod reason_cases;
mod reserved_cases;
mod scoped_cases;

use std::str::FromStr;

use audit::Recorder;
use cedar_policy::{EntityId, EntityTypeName, EntityUid};
use decision::{Engine, OrgGrn, Role, RoleScope};
use entity::{EntityChange, EntityStore, Upsert};
use grant::{Body, NewPolicy, PolicyStore};
use listening::{Pair, bound, trusting};
use server::proto::policy_body::Body as Wire;
use server::proto::policy_service_client::PolicyServiceClient;
use server::proto::{
    self as pb, Action, EntityRef, EvaluateRequest, PolicyBody, Resource, Subject,
};
use server::{Verifier, serve_recording};
use sqlx::PgPool;
use sqlx::migrate::Migrator;
use tonic::transport::Channel;
use tonic::{Code, Request};

type Named = (&'static str, &'static str);

const ACME: &str = "grn:root/iam/org/acme";
const GLOBEX: &str = "grn:root/iam/org/globex";
const ORG: Named = ("Iam::Org", ACME);
const MEMBERS: Named = ("Iam::OrgMembers", ACME);
const GLOBEX_ORG: Named = ("Iam::Org", GLOBEX);
const GLOBEX_MEMBERS: Named = ("Iam::OrgMembers", GLOBEX);
const LEE: Named = ("Iam::User", "grn:acme/iam/user/lee");
const ADA: Named = ("Iam::User", "grn:acme/iam/user/ada");
const OWEN: Named = ("Iam::User", "grn:acme/iam/user/owen");
const KIM: Named = ("Iam::User", "grn:acme/iam/user/kim");
const GIL: Named = ("Iam::User", "grn:globex/iam/user/gil");
const BOT: Named = ("Iam::Agent", "grn:acme/iam/agent/bot");
const PLATFORM: &str = "platform";
const CODEHUB: &str = "grn:acme/iam/service/codehub";
const COWORK: &str = "grn:acme/iam/service/cowork";
/// The slug the platform names lee's administrator link by:
/// `printf 'grn:acme/iam/user/lee' | sha256sum | cut -c1-32`, written
/// out so the naming is held to a value and not to the function that
/// makes it.
const LEE_ADMIN: &str = "link-admin-fadf4f482ade12a917dd444d084659a0";

const LEE_WATCHES_BOT: &str = r#"permit (
    principal == Iam::User::"grn:acme/iam/user/lee",
    action == Iam::Action::"agent.watch",
    resource == Iam::Agent::"grn:acme/iam/agent/bot"
);"#;
const KIM_WATCHES_BOT: &str = r#"permit (
    principal == Iam::User::"grn:acme/iam/user/kim",
    action == Iam::Action::"agent.watch",
    resource == Iam::Agent::"grn:acme/iam/agent/bot"
);"#;
const NO_ONE_READS_BOT: &str = r#"forbid (
    principal,
    action == Iam::Action::"agent.read",
    resource == Iam::Agent::"grn:acme/iam/agent/bot"
);"#;
const NO_ONE_WATCHES_BOT: &str = r#"forbid (
    principal,
    action == Iam::Action::"agent.watch",
    resource == Iam::Agent::"grn:acme/iam/agent/bot"
);"#;

static MIGRATIONS: Migrator = sqlx::migrate!("../../migrations");

fn uid(named: Named) -> EntityUid {
    EntityUid::from_type_name_and_id(
        EntityTypeName::from_str(named.0).unwrap(),
        EntityId::new(named.1),
    )
}

fn upsert(
    engine: &Engine,
    named: Named,
    org: Option<Named>,
    members: Option<Named>,
    parents: &[Named],
) -> EntityChange {
    EntityChange::upsert(
        engine,
        uid(named),
        1,
        Upsert {
            org: org.map(uid),
            members: members.map(uid),
            parents: parents.iter().copied().map(uid).collect(),
            ..Upsert::default()
        },
    )
    .unwrap()
}

async fn link(store: &PolicyStore, engine: &Engine, role: Role, who: Named, grn: &str) {
    let org = OrgGrn::new(if who.1.contains("globex") {
        GLOBEX
    } else {
        ACME
    });
    let target = if who.1.contains("globex") {
        GLOBEX_ORG
    } else {
        ORG
    };
    let body = Body::role_link(
        engine,
        &org,
        role,
        RoleScope::Organization,
        uid(who),
        uid(target),
    )
    .unwrap();
    store
        .create(NewPolicy {
            grn,
            org: &org,
            name: "seeded",
            body,
            created_by: "test",
        })
        .await
        .unwrap();
    store.activate(&org, grn).await.unwrap();
}

/// Acme: lee, kim and the bot are members, ada an administrator, owen
/// the owner. Globex: gil its owner.
async fn seed(pool: &PgPool) {
    let engine = Engine::load().unwrap();
    let changes = [
        upsert(&engine, MEMBERS, None, None, &[]),
        upsert(&engine, ORG, Some(ORG), Some(MEMBERS), &[]),
        upsert(&engine, LEE, None, None, &[MEMBERS]),
        upsert(&engine, KIM, None, None, &[MEMBERS]),
        upsert(&engine, ADA, None, None, &[MEMBERS]),
        upsert(&engine, OWEN, None, None, &[MEMBERS]),
        upsert(&engine, BOT, Some(ORG), None, &[MEMBERS, ORG]),
        upsert(&engine, GLOBEX_MEMBERS, None, None, &[]),
        upsert(
            &engine,
            GLOBEX_ORG,
            Some(GLOBEX_ORG),
            Some(GLOBEX_MEMBERS),
            &[],
        ),
        upsert(&engine, GIL, None, None, &[GLOBEX_MEMBERS]),
    ];
    EntityStore::new(pool.clone(), engine.clone())
        .apply(&changes)
        .await
        .unwrap();
    let store = PolicyStore::new(pool.clone());
    link(
        &store,
        &engine,
        Role::Member,
        LEE,
        "grn:acme/iam/policy/lee",
    )
    .await;
    link(
        &store,
        &engine,
        Role::Member,
        KIM,
        "grn:acme/iam/policy/kim",
    )
    .await;
    link(&store, &engine, Role::Admin, ADA, "grn:acme/iam/policy/ada").await;
    link(
        &store,
        &engine,
        Role::Owner,
        OWEN,
        "grn:acme/iam/policy/owen",
    )
    .await;
    link(
        &store,
        &engine,
        Role::Owner,
        GIL,
        "grn:globex/iam/policy/gil",
    )
    .await;
}

struct Platform {
    grpc: PolicyServiceClient<Channel>,
    token: String,
}

async fn platform(pool: PgPool) -> Platform {
    as_caller(pool, PLATFORM).await
}

async fn as_caller(pool: PgPool, sub: &str) -> Platform {
    let recorder = Recorder::new(pool.clone());
    recording_as(pool, sub, recorder).await
}

async fn recording_as(pool: PgPool, sub: &str, recorder: Recorder) -> Platform {
    let pair = Pair::new();
    let bound = bound().await;
    let verifier = Verifier::from_jwks(
        &caller_tokens::jwks(&[caller_tokens::signer()]),
        caller_tokens::ISSUER,
    )
    .unwrap();
    tokio::spawn(serve_recording(
        pool,
        verifier,
        pair.identity(),
        bound.listeners,
        recorder,
        std::future::pending(),
    ));
    let token = caller_tokens::signer().sign(&caller_tokens::Claims {
        sub: sub.to_owned(),
        ..caller_tokens::claims()
    });
    Platform {
        grpc: PolicyServiceClient::new(trusting(&pair.cert, bound.contract).await),
        token,
    }
}

fn subject(named: Named) -> Option<Subject> {
    Some(Subject {
        r#type: named.0.into(),
        id: named.1.into(),
    })
}

fn entity(named: Named) -> Option<EntityRef> {
    Some(EntityRef {
        r#type: named.0.into(),
        id: named.1.into(),
    })
}

fn policy_of(slug: &str) -> String {
    format!("grn:acme/iam/policy/{slug}")
}

fn role_link(role: Role, who: Named) -> PolicyBody {
    PolicyBody {
        body: Some(Wire::RoleLink(pb::RoleLink {
            role: match role {
                Role::Owner => pb::Role::Owner,
                Role::Admin => pb::Role::Admin,
                Role::Member => pb::Role::Member,
            }
            .into(),
            scope: pb::RoleScope::Organization.into(),
            principal: entity(who),
            resource: entity(ORG),
        })),
    }
}

fn guardrail(source: &str) -> PolicyBody {
    PolicyBody {
        body: Some(Wire::Guardrail(source.into())),
    }
}

fn permit(source: &str) -> PolicyBody {
    PolicyBody {
        body: Some(Wire::Permit(source.into())),
    }
}

impl Platform {
    fn request<T>(&self, message: T) -> Request<T> {
        let mut request = Request::new(message);
        request.metadata_mut().insert(
            "authorization",
            format!("Bearer {}", self.token).parse().unwrap(),
        );
        request
    }

    async fn create(
        &mut self,
        writer: Option<Named>,
        slug: &str,
        body: PolicyBody,
    ) -> Result<(), tonic::Status> {
        let request = self.request(pb::CreatePolicyRequest {
            writer: writer.and_then(subject),
            org: ACME.into(),
            policy: policy_of(slug),
            name: slug.into(),
            body: Some(body),
        });
        self.grpc.create_policy(request).await.map(|_| ())
    }

    async fn append(
        &mut self,
        writer: Option<Named>,
        slug: &str,
        body: PolicyBody,
    ) -> Result<i32, tonic::Status> {
        let request = self.request(pb::AppendPolicyVersionRequest {
            writer: writer.and_then(subject),
            org: ACME.into(),
            policy: policy_of(slug),
            body: Some(body),
        });
        let answer = self.grpc.append_policy_version(request).await?;
        Ok(answer.into_inner().version)
    }

    async fn activate(&mut self, writer: Option<Named>, slug: &str) -> Result<(), tonic::Status> {
        let request = self.request(pb::ActivatePolicyRequest {
            writer: writer.and_then(subject),
            org: ACME.into(),
            policy: policy_of(slug),
        });
        self.grpc.activate_policy(request).await.map(|_| ())
    }

    async fn roll_back(
        &mut self,
        writer: Option<Named>,
        slug: &str,
        version: i32,
    ) -> Result<(), tonic::Status> {
        let request = self.request(pb::RollBackPolicyRequest {
            writer: writer.and_then(subject),
            org: ACME.into(),
            policy: policy_of(slug),
            version,
        });
        self.grpc.roll_back_policy(request).await.map(|_| ())
    }

    async fn delete(&mut self, writer: Option<Named>, slug: &str) -> Result<(), tonic::Status> {
        let request = self.request(pb::DeletePolicyRequest {
            writer: writer.and_then(subject),
            org: ACME.into(),
            policy: policy_of(slug),
        });
        self.grpc.delete_policy(request).await.map(|_| ())
    }

    async fn get(
        &mut self,
        reader: Option<Named>,
        org: &str,
        policy: &str,
    ) -> Result<pb::GetPolicyResponse, tonic::Status> {
        let request = self.request(pb::GetPolicyRequest {
            writer: reader.and_then(subject),
            org: org.into(),
            policy: policy.into(),
        });
        Ok(self.grpc.get_policy(request).await?.into_inner())
    }

    async fn list(
        &mut self,
        reader: Option<Named>,
        org: &str,
    ) -> Result<Vec<pb::PolicySummary>, tonic::Status> {
        let request = self.request(pb::ListPoliciesRequest {
            writer: reader.and_then(subject),
            org: org.into(),
        });
        Ok(self
            .grpc
            .list_policies(request)
            .await?
            .into_inner()
            .policies)
    }

    async fn watches(&mut self, who: Named) -> bool {
        let request = self.request(EvaluateRequest {
            subject: subject(who),
            action: Some(Action {
                name: "iam:agent.watch".into(),
            }),
            resource: Some(Resource {
                r#type: BOT.0.into(),
                id: BOT.1.into(),
                properties: None,
            }),
            context: None,
            read: 0,
            probe: false,
        });
        self.grpc
            .evaluate(request)
            .await
            .unwrap()
            .into_inner()
            .decision
    }
}

fn refused(result: Result<impl std::fmt::Debug, tonic::Status>, code: Code) {
    match result {
        Err(status) => assert_eq!(status.code(), code, "{status}"),
        Ok(done) => panic!("accepted: {done:?}"),
    }
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn an_owners_permit_is_stored_activated_and_deciding_and_an_administrators_is_refused(
    pool: PgPool,
) {
    seed(&pool).await;
    let mut service = platform(pool).await;

    refused(
        service
            .create(Some(ADA), "ada-permit", permit(LEE_WATCHES_BOT))
            .await,
        Code::PermissionDenied,
    );
    assert!(
        service
            .list(Some(OWEN), ACME)
            .await
            .unwrap()
            .iter()
            .all(|p| { p.policy != policy_of("ada-permit") })
    );

    service
        .create(Some(OWEN), "watch", permit(LEE_WATCHES_BOT))
        .await
        .unwrap();
    assert!(!service.watches(LEE).await, "a draft decides nothing");
    refused(
        service.activate(Some(ADA), "watch").await,
        Code::PermissionDenied,
    );
    assert!(!service.watches(LEE).await);
    service.activate(Some(OWEN), "watch").await.unwrap();
    assert!(service.watches(LEE).await);
    assert!(!service.watches(KIM).await, "it names lee alone");
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_writer_links_only_a_role_it_holds(pool: PgPool) {
    seed(&pool).await;
    let mut service = platform(pool).await;

    service
        .create(Some(ADA), "kim-admin", role_link(Role::Admin, KIM))
        .await
        .unwrap();
    service
        .create(Some(ADA), "kim-member", role_link(Role::Member, KIM))
        .await
        .unwrap();
    refused(
        service
            .create(Some(ADA), "kim-owner", role_link(Role::Owner, KIM))
            .await,
        Code::PermissionDenied,
    );
    service
        .create(Some(OWEN), "kim-owner", role_link(Role::Owner, KIM))
        .await
        .unwrap();
    refused(
        service
            .create(Some(LEE), "kim-lee", role_link(Role::Member, KIM))
            .await,
        Code::PermissionDenied,
    );
    let stored = service.list(Some(OWEN), ACME).await.unwrap();
    assert!(stored.iter().all(|p| p.policy != policy_of("kim-lee")));
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn an_administrator_may_not_activate_append_roll_back_or_delete_an_owners_link(pool: PgPool) {
    seed(&pool).await;
    let mut service = platform(pool).await;
    service
        .create(Some(OWEN), "kim-owner", role_link(Role::Owner, KIM))
        .await
        .unwrap();
    service
        .append(Some(OWEN), "kim-owner", role_link(Role::Member, KIM))
        .await
        .unwrap();

    refused(
        service.activate(Some(ADA), "kim-owner").await,
        Code::PermissionDenied,
    );
    refused(
        service.roll_back(Some(ADA), "kim-owner", 1).await,
        Code::PermissionDenied,
    );
    refused(
        service.delete(Some(ADA), "kim-owner").await,
        Code::PermissionDenied,
    );
    refused(
        service
            .append(Some(ADA), "kim-owner", role_link(Role::Owner, KIM))
            .await,
        Code::PermissionDenied,
    );
    // A policy that ever linked an owner is an owner's to change, even
    // to the member version an administrator holds the role of.
    refused(
        service.roll_back(Some(ADA), "kim-owner", 2).await,
        Code::PermissionDenied,
    );
    refused(
        service
            .append(Some(ADA), "kim-owner", role_link(Role::Member, KIM))
            .await,
        Code::PermissionDenied,
    );
    // Control: an owner does the same sequence.
    service
        .append(Some(OWEN), "kim-owner", role_link(Role::Member, LEE))
        .await
        .unwrap();
    service.roll_back(Some(OWEN), "kim-owner", 2).await.unwrap();
    // The default now links a member, but an earlier version linked an
    // owner: it stays an owner's to change.
    refused(
        service.roll_back(Some(ADA), "kim-owner", 1).await,
        Code::PermissionDenied,
    );
    refused(
        service.activate(Some(ADA), "kim-owner").await,
        Code::PermissionDenied,
    );
    service.activate(Some(OWEN), "kim-owner").await.unwrap();
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn an_administrator_changes_a_link_of_roles_it_holds(pool: PgPool) {
    seed(&pool).await;
    let mut service = platform(pool).await;
    service
        .create(Some(ADA), "kim-member", role_link(Role::Member, KIM))
        .await
        .unwrap();
    service
        .append(Some(ADA), "kim-member", role_link(Role::Admin, KIM))
        .await
        .unwrap();
    service.roll_back(Some(ADA), "kim-member", 2).await.unwrap();
    service.activate(Some(ADA), "kim-member").await.unwrap();
    service.delete(Some(ADA), "kim-member").await.unwrap();
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn an_administrator_removes_an_owners_permit_as_it_may_forbid_what_it_allows(pool: PgPool) {
    seed(&pool).await;
    let mut service = platform(pool).await;
    service
        .create(Some(OWEN), "watch", permit(LEE_WATCHES_BOT))
        .await
        .unwrap();
    service.activate(Some(OWEN), "watch").await.unwrap();
    assert!(service.watches(LEE).await);

    service.delete(Some(ADA), "watch").await.unwrap();

    assert!(!service.watches(LEE).await);
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_permit_with_comments_or_an_unless_of_its_own_is_stored_and_deciding(pool: PgPool) {
    seed(&pool).await;
    let mut service = platform(pool).await;
    let shapes = [
        format!("{LEE_WATCHES_BOT} // a note"),
        format!("{LEE_WATCHES_BOT} // a; b;"),
        format!("// a; b;\n{LEE_WATCHES_BOT}"),
        LEE_WATCHES_BOT.replace(");", ") unless { false };"),
    ];
    for (n, source) in shapes.iter().enumerate() {
        let slug = format!("shape-{n}");
        service
            .create(Some(OWEN), &slug, permit(source))
            .await
            .unwrap_or_else(|status| panic!("{source}: {status}"));
        service.activate(Some(OWEN), &slug).await.unwrap();
        assert!(service.watches(LEE).await, "{source}");
        service.delete(Some(OWEN), &slug).await.unwrap();
        assert!(!service.watches(LEE).await, "{source}");
    }
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_writer_without_the_read_right_is_told_nothing_of_a_policy(pool: PgPool) {
    seed(&pool).await;
    let mut service = platform(pool).await;
    service
        .create(Some(OWEN), "watch", permit(LEE_WATCHES_BOT))
        .await
        .unwrap();
    service
        .create(Some(OWEN), "no-watch", guardrail(NO_ONE_WATCHES_BOT))
        .await
        .unwrap();
    let mut told = Vec::new();
    for slug in ["watch", "no-watch", "absent"] {
        for answer in [
            service
                .append(Some(LEE), slug, role_link(Role::Member, LEE))
                .await
                .map(|_| ()),
            service
                .append(Some(LEE), slug, guardrail(NO_ONE_WATCHES_BOT))
                .await
                .map(|_| ()),
            service.activate(Some(LEE), slug).await,
            service.roll_back(Some(LEE), slug, 1).await,
            service.delete(Some(LEE), slug).await,
        ] {
            let status = answer.unwrap_err();
            told.push((status.code(), status.message().to_owned()));
        }
    }
    assert!(
        told.iter().all(|answer| answer.0 == Code::PermissionDenied),
        "{told:?}"
    );
    assert!(told.iter().all(|answer| answer.1 == told[0].1), "{told:?}");
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_writer_of_another_organization_changes_no_policy_of_this_one(pool: PgPool) {
    seed(&pool).await;
    let mut service = platform(pool).await;
    service
        .create(Some(OWEN), "kim-member", role_link(Role::Member, KIM))
        .await
        .unwrap();
    let grn = policy_of("kim-member");
    let gil = subject(GIL);
    let (org, writer) = (ACME.to_owned(), gil);

    let append = pb::AppendPolicyVersionRequest {
        writer: writer.clone(),
        org: org.clone(),
        policy: grn.clone(),
        body: Some(role_link(Role::Member, GIL)),
    };
    let request = service.request(append);
    refused(
        service.grpc.append_policy_version(request).await,
        Code::PermissionDenied,
    );
    let activate = pb::ActivatePolicyRequest {
        writer: writer.clone(),
        org: org.clone(),
        policy: grn.clone(),
    };
    let request = service.request(activate);
    refused(
        service.grpc.activate_policy(request).await,
        Code::PermissionDenied,
    );
    let roll_back = pb::RollBackPolicyRequest {
        writer: writer.clone(),
        org: org.clone(),
        policy: grn.clone(),
        version: 1,
    };
    let request = service.request(roll_back);
    refused(
        service.grpc.roll_back_policy(request).await,
        Code::PermissionDenied,
    );
    let delete = pb::DeletePolicyRequest {
        writer,
        org,
        policy: grn,
    };
    let request = service.request(delete);
    refused(
        service.grpc.delete_policy(request).await,
        Code::PermissionDenied,
    );
    assert!(
        service
            .get(Some(OWEN), ACME, &policy_of("kim-member"))
            .await
            .is_ok()
    );
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn an_administrator_writes_a_guardrail_and_a_member_does_not(pool: PgPool) {
    seed(&pool).await;
    let mut service = platform(pool).await;

    service
        .create(Some(ADA), "no-watch", guardrail(NO_ONE_WATCHES_BOT))
        .await
        .unwrap();
    refused(
        service
            .create(Some(LEE), "no-read", guardrail(NO_ONE_READS_BOT))
            .await,
        Code::PermissionDenied,
    );
    refused(
        service.activate(Some(LEE), "no-watch").await,
        Code::PermissionDenied,
    );
    refused(
        service.delete(Some(LEE), "no-watch").await,
        Code::PermissionDenied,
    );
    service.activate(Some(ADA), "no-watch").await.unwrap();
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn each_kind_is_versioned_activated_rolled_back_and_removed(pool: PgPool) {
    seed(&pool).await;
    let mut service = platform(pool).await;
    let kinds = [
        (
            "a-link",
            role_link(Role::Member, KIM),
            role_link(Role::Admin, KIM),
        ),
        (
            "a-guardrail",
            guardrail(NO_ONE_WATCHES_BOT),
            guardrail(NO_ONE_READS_BOT),
        ),
        ("a-permit", permit(LEE_WATCHES_BOT), permit(KIM_WATCHES_BOT)),
    ];
    for (slug, first, second) in kinds {
        let grn = policy_of(slug);
        service
            .create(Some(OWEN), slug, first.clone())
            .await
            .unwrap();
        assert_eq!(
            service
                .append(Some(OWEN), slug, second.clone())
                .await
                .unwrap(),
            2
        );
        // A policy keeps the kind its first version has.
        let other = if matches!(first.body, Some(Wire::Guardrail(_))) {
            permit(LEE_WATCHES_BOT)
        } else {
            guardrail(NO_ONE_READS_BOT)
        };
        refused(
            service.append(Some(OWEN), slug, other).await,
            Code::InvalidArgument,
        );

        let read = service.get(Some(OWEN), ACME, &grn).await.unwrap();
        let summary = read.policy.unwrap();
        assert_eq!(summary.status, pb::PolicyStatus::Draft as i32);
        assert_eq!(summary.default_version, 1);
        assert_eq!(read.versions.len(), 2);
        assert_eq!(read.versions[0].body.as_ref(), Some(&first));
        assert_eq!(read.versions[1].body.as_ref(), Some(&second));
        assert_eq!(read.versions[1].created_by, OWEN.1);

        service.roll_back(Some(OWEN), slug, 2).await.unwrap();
        service.activate(Some(OWEN), slug).await.unwrap();
        let moved = service.get(Some(OWEN), ACME, &grn).await.unwrap();
        let summary = moved.policy.unwrap();
        assert_eq!(summary.status, pb::PolicyStatus::Active as i32);
        assert_eq!(summary.default_version, 2);
        service.roll_back(Some(OWEN), slug, 1).await.unwrap();
        let back = service.get(Some(OWEN), ACME, &grn).await.unwrap();
        assert_eq!(back.policy.unwrap().default_version, 1);
        refused(service.roll_back(Some(OWEN), slug, 3).await, Code::NotFound);

        let listed = service.list(Some(OWEN), ACME).await.unwrap();
        assert!(listed.iter().any(|p| p.policy == grn));
        service.delete(Some(OWEN), slug).await.unwrap();
        refused(service.get(Some(OWEN), ACME, &grn).await, Code::NotFound);
        refused(service.delete(Some(OWEN), slug).await, Code::NotFound);
    }
    refused(
        service
            .create(Some(OWEN), "a-link", role_link(Role::Member, KIM))
            .await
            .and(
                service
                    .create(Some(OWEN), "a-link", role_link(Role::Member, KIM))
                    .await,
            ),
        Code::AlreadyExists,
    );
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_list_and_a_read_are_refused_to_a_writer_of_another_organization(pool: PgPool) {
    seed(&pool).await;
    let mut service = platform(pool).await;
    service
        .create(Some(OWEN), "kept", guardrail(NO_ONE_WATCHES_BOT))
        .await
        .unwrap();

    refused(service.list(Some(GIL), ACME).await, Code::PermissionDenied);
    refused(
        service.get(Some(GIL), ACME, &policy_of("kept")).await,
        Code::PermissionDenied,
    );
    // Controls: the owner and an administrator read, and
    // a writer of another organization writes nothing here.
    assert_eq!(service.list(Some(OWEN), ACME).await.unwrap().len(), 5);
    service
        .get(Some(ADA), ACME, &policy_of("kept"))
        .await
        .unwrap();
    // `iam:policy.read` is the administrators' and the owner's.
    refused(
        service.get(Some(LEE), ACME, &policy_of("kept")).await,
        Code::PermissionDenied,
    );
    refused(
        service
            .create(Some(GIL), "gil-guardrail", guardrail(NO_ONE_WATCHES_BOT))
            .await,
        Code::PermissionDenied,
    );
    refused(service.list(None, ACME).await, Code::PermissionDenied);
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_write_with_no_writer_is_for_a_memberships_role_link_alone(pool: PgPool) {
    seed(&pool).await;
    let mut service = platform(pool).await;

    service
        .create(None, "membership", role_link(Role::Member, KIM))
        .await
        .unwrap();
    service.activate(None, "membership").await.unwrap();
    refused(
        service
            .append(None, "membership", role_link(Role::Admin, KIM))
            .await,
        Code::PermissionDenied,
    );
    service
        .append(Some(OWEN), "membership", role_link(Role::Admin, KIM))
        .await
        .unwrap();
    refused(
        service.roll_back(None, "membership", 2).await,
        Code::PermissionDenied,
    );
    service.delete(None, "membership").await.unwrap();

    refused(
        service
            .create(None, "g", guardrail(NO_ONE_WATCHES_BOT))
            .await,
        Code::PermissionDenied,
    );
    refused(
        service.create(None, "p", permit(LEE_WATCHES_BOT)).await,
        Code::PermissionDenied,
    );
    service
        .create(Some(OWEN), "kept-g", guardrail(NO_ONE_WATCHES_BOT))
        .await
        .unwrap();
    service
        .create(Some(OWEN), "kept-p", permit(LEE_WATCHES_BOT))
        .await
        .unwrap();
    for slug in ["kept-g", "kept-p"] {
        refused(service.activate(None, slug).await, Code::PermissionDenied);
        refused(service.delete(None, slug).await, Code::PermissionDenied);
    }
    refused(service.list(None, ACME).await, Code::PermissionDenied);
    refused(
        service.get(None, ACME, &policy_of("kept-g")).await,
        Code::PermissionDenied,
    );
}

#[sqlx::test(migrator = "MIGRATIONS")]
#[ignore = "needs Postgres: make -C policy stack-up suites"]
async fn a_scoped_caller_is_refused_every_write(pool: PgPool) {
    seed(&pool).await;
    let mut platform = platform(pool.clone()).await;
    platform
        .create(Some(OWEN), "kept", guardrail(NO_ONE_WATCHES_BOT))
        .await
        .unwrap();

    for sub in [CODEHUB, COWORK] {
        let mut scoped = as_caller(pool.clone(), sub).await;
        let who = Some(OWEN);
        refused(
            scoped.create(who, "x", guardrail(NO_ONE_READS_BOT)).await,
            Code::PermissionDenied,
        );
        refused(
            scoped.create(None, "x", role_link(Role::Member, KIM)).await,
            Code::PermissionDenied,
        );
        // The member's own identifier and body, which the platform
        // writes (ADR-0161), is the platform's alone.
        refused(
            scoped
                .create(None, LEE_ADMIN, role_link(Role::Admin, LEE))
                .await,
            Code::PermissionDenied,
        );
        refused(
            scoped
                .append(who, "kept", guardrail(NO_ONE_READS_BOT))
                .await,
            Code::PermissionDenied,
        );
        refused(scoped.activate(who, "kept").await, Code::PermissionDenied);
        refused(
            scoped.roll_back(who, "kept", 1).await,
            Code::PermissionDenied,
        );
        refused(scoped.delete(who, "kept").await, Code::PermissionDenied);
        refused(
            scoped.get(who, ACME, &policy_of("kept")).await,
            Code::PermissionDenied,
        );
        refused(scoped.list(who, ACME).await, Code::PermissionDenied);
    }
    // Control: nothing a scoped caller sent was stored, and the
    // platform's own policy is still there, and the platform writes
    // the link a scoped caller was refused.
    let stored = platform.list(Some(OWEN), ACME).await.unwrap();
    assert_eq!(stored.len(), 5);
    platform
        .create(None, LEE_ADMIN, role_link(Role::Admin, LEE))
        .await
        .unwrap();
}
