//! A caller bound to one organization, over a real gRPC connection
//! with the stores faked: this suite reads no database. Its token's
//! `sub` is a service principal's GRN, and what it writes and asks
//! stays within that organization and its own service (ADR-0140).

#![cfg(test)]

mod caller_tokens;

use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering};

use caller_tokens::*;
use cedar_policy::{EntityUid, PolicyId};
use decision::{
    Cause, Engine, EntitySlice, EntitySource, OrgGrn, OrgPolicies, PolicySource, Role, RoleLink,
    RoleScope,
};
use entity::{Applied, EntityChange as StoreChange, Error};
use serde_json::{Value, json};
use server::proto::entity_change::Operation;
use server::proto::policy_service_client::PolicyServiceClient;
use server::proto::{
    Action, ApplyEntityChangesRequest, ApplyEntityChangesResponse, EntityChange, EntityDelete,
    EntityRef, EntityUpsert, EvaluateRequest, Evaluation, EvaluationsRequest, Resource, Subject,
};
use server::{EntitySink, Service, Verifier, authenticated};
use tokio::net::TcpListener;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::{Channel, Server};
use tonic::{Code, Request, Status};

const ACME: &str = "grn:root/iam/org/acme";
const GLOBEX: &str = "grn:root/iam/org/globex";
const COWORK: &str = "grn:acme/iam/service/cowork";
const PROJECT: &str = "grn:acme/iam/project/web";
const CHANNEL: &str = "grn:acme/cowork/project/web/channel/general";

/// Counts what reaches the store, and writes it nowhere. It holds no
/// policy and no entity unless a test hands it some.
#[derive(Clone)]
struct Counting {
    engine: Engine,
    reached: Arc<AtomicUsize>,
    links: Vec<RoleLink>,
    directory: Value,
    org: Option<OrgGrn>,
}

impl PolicySource for Counting {
    async fn active(&self, _: &OrgGrn) -> Result<OrgPolicies, Cause> {
        Ok(OrgPolicies {
            links: self.links.clone(),
            ..OrgPolicies::default()
        })
    }
}

impl EntitySource for Counting {
    async fn slice(&self, _: &EntityUid, _: &EntityUid) -> Result<EntitySlice, Cause> {
        Ok(EntitySlice {
            entities: self.engine.entities(self.directory.clone())?,
            org: self.org.clone(),
        })
    }
}

impl EntitySink for Counting {
    async fn apply(&self, changes: &[StoreChange]) -> Result<Applied, Error> {
        self.reached.fetch_add(changes.len(), Ordering::SeqCst);
        Ok(Applied {
            written: changes.len(),
            dropped: 0,
        })
    }
}

async fn served() -> (PolicyServiceClient<Channel>, Arc<AtomicUsize>) {
    serving(Vec::new(), json!([]), None).await
}

async fn serving(
    links: Vec<RoleLink>,
    directory: Value,
    org: Option<OrgGrn>,
) -> (PolicyServiceClient<Channel>, Arc<AtomicUsize>) {
    let engine = Engine::load().unwrap();
    let reached = Arc::new(AtomicUsize::new(0));
    let stores = Counting {
        engine: engine.clone(),
        reached: reached.clone(),
        links,
        directory,
        org,
    };
    let service = Service::new(engine, stores.clone(), stores);
    let verifier = Verifier::from_jwks(&jwks(&[signer()]), ISSUER).unwrap();
    let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
    let address = listener.local_addr().unwrap();
    tokio::spawn(
        Server::builder()
            .add_service(authenticated(service, verifier))
            .serve_with_incoming(TcpListenerStream::new(listener)),
    );
    let channel = Channel::from_shared(format!("http://{address}"))
        .unwrap()
        .connect()
        .await
        .unwrap();
    (PolicyServiceClient::new(channel), reached)
}

fn bearer<T>(message: T, sub: &str) -> Request<T> {
    let token = signer().sign(&Claims {
        sub: sub.to_owned(),
        ..claims()
    });
    let mut request = Request::new(message);
    request
        .metadata_mut()
        .insert("authorization", format!("Bearer {token}").parse().unwrap());
    request
}

async fn apply(
    client: &mut PolicyServiceClient<Channel>,
    sub: &str,
    changes: Vec<EntityChange>,
) -> Result<ApplyEntityChangesResponse, Status> {
    client
        .apply_entity_changes(bearer(ApplyEntityChangesRequest { changes }, sub))
        .await
        .map(tonic::Response::into_inner)
}

fn named(entity_type: &str, id: &str) -> EntityRef {
    EntityRef {
        r#type: entity_type.into(),
        id: id.into(),
    }
}

fn upsert(entity: EntityRef, org: Option<EntityRef>, parents: Vec<EntityRef>) -> EntityChange {
    EntityChange {
        entity: Some(entity),
        sequence: 1,
        operation: Some(Operation::Upsert(EntityUpsert {
            org,
            members: None,
            parents,
            ..EntityUpsert::default()
        })),
    }
}

/// A channel of acme under acme's project, as cowork writes it.
fn channel() -> EntityChange {
    upsert(
        named("Cowork::Channel", CHANNEL),
        Some(named("Iam::Org", ACME)),
        vec![named("Iam::Org", ACME), named("Iam::Project", PROJECT)],
    )
}

async fn refused_whole(change: EntityChange) {
    let (mut client, reached) = served().await;
    let refused = apply(&mut client, COWORK, vec![channel(), change])
        .await
        .unwrap_err();
    assert_eq!(refused.code(), Code::PermissionDenied);
    assert_eq!(reached.load(Ordering::SeqCst), 0);
}

#[tokio::test]
async fn a_scoped_caller_writes_what_it_owns_in_its_organization() {
    let (mut client, reached) = served().await;

    let written = apply(&mut client, COWORK, vec![channel()]).await.unwrap();

    assert_eq!(written.written, 1);
    assert!(written.refusals.is_empty());
    assert_eq!(reached.load(Ordering::SeqCst), 1);
}

#[tokio::test]
async fn an_entity_of_another_organization_refuses_the_whole_batch() {
    refused_whole(upsert(
        named("Cowork::Channel", "grn:globex/cowork/channel/general"),
        Some(named("Iam::Org", ACME)),
        vec![named("Iam::Org", ACME)],
    ))
    .await;
}

#[tokio::test]
async fn an_org_of_another_organization_refuses_the_whole_batch() {
    refused_whole(upsert(
        named("Cowork::Channel", "grn:acme/cowork/channel/random"),
        Some(named("Iam::Org", GLOBEX)),
        vec![named("Iam::Org", ACME)],
    ))
    .await;
}

#[tokio::test]
async fn an_org_that_is_not_the_organizations_own_org_refuses_the_whole_batch() {
    refused_whole(upsert(
        named("Cowork::Channel", "grn:acme/cowork/channel/random"),
        Some(named("Iam::OrgMembers", ACME)),
        vec![named("Iam::Org", ACME)],
    ))
    .await;
}

#[tokio::test]
async fn a_parent_of_another_organization_refuses_the_whole_batch() {
    for parent in [
        named("Iam::Project", "grn:globex/iam/project/web"),
        named("Iam::Org", GLOBEX),
    ] {
        refused_whole(upsert(
            named("Cowork::Channel", "grn:acme/cowork/channel/random"),
            Some(named("Iam::Org", ACME)),
            vec![named("Iam::Org", ACME), parent],
        ))
        .await;
    }
}

#[tokio::test]
async fn a_delete_of_another_organizations_entity_refuses_the_whole_batch() {
    refused_whole(EntityChange {
        entity: Some(named(
            "Cowork::Channel",
            "grn:globex/cowork/channel/general",
        )),
        sequence: 1,
        operation: Some(Operation::Delete(EntityDelete {})),
    })
    .await;
}

#[tokio::test]
async fn a_type_a_scoped_caller_does_not_own_refuses_the_whole_batch() {
    refused_whole(upsert(
        named("Iam::Team", "grn:acme/iam/team/eng"),
        Some(named("Iam::Org", ACME)),
        vec![named("Iam::Org", ACME)],
    ))
    .await;
}

/// A consent is an organization's half of ADR-0147's, and an operator's
/// operations are ADR-0146's grant: only the platform writes either, so
/// a scoped caller can neither give an organization its other half nor
/// hold an operator to an operation.
#[tokio::test]
async fn a_scoped_caller_writes_neither_a_consent_nor_an_operators_operations() {
    let org = || Some(named("Iam::Org", ACME));
    let globex = || vec![named("Iam::Org", GLOBEX)];
    let consenting = EntityChange {
        entity: Some(named("Iam::Org", ACME)),
        sequence: 1,
        operation: Some(Operation::Upsert(EntityUpsert {
            org: org(),
            members: Some(named("Iam::OrgMembers", ACME)),
            consents: globex(),
            ..EntityUpsert::default()
        })),
    };
    let delegated = EntityChange {
        entity: Some(named("Iam::Operator", "grn:acme/iam/operator/hosting")),
        sequence: 1,
        operation: Some(Operation::Upsert(EntityUpsert {
            org: org(),
            parents: vec![named("Iam::OrgMembers", ACME)],
            operations: vec!["iam:agent.activate".to_owned()],
            ..EntityUpsert::default()
        })),
    };
    refused_whole(consenting).await;
    refused_whole(delegated).await;
}

#[tokio::test]
async fn another_fault_of_a_scoped_caller_is_still_its_entry_s_alone() {
    let (mut client, reached) = served().await;
    let no_org = upsert(
        named("Cowork::Channel", "grn:acme/cowork/channel/random"),
        None,
        vec![named("Iam::Org", ACME)],
    );

    let answer = apply(&mut client, COWORK, vec![no_org, channel()])
        .await
        .unwrap();

    let refused: Vec<u32> = answer.refusals.iter().map(|r| r.index).collect();
    assert_eq!(refused, [0]);
    assert_eq!(answer.written, 1);
    assert_eq!(reached.load(Ordering::SeqCst), 1);
}

#[tokio::test]
async fn the_platform_still_writes_every_organization() {
    let (mut client, reached) = served().await;
    let team = |org: &str, grn: &str| {
        upsert(
            named("Iam::Team", grn),
            Some(named("Iam::Org", org)),
            vec![named("Iam::Org", org)],
        )
    };

    let answer = apply(
        &mut client,
        "platform",
        vec![
            team(ACME, "grn:acme/iam/team/eng"),
            team(GLOBEX, "grn:globex/iam/team/eng"),
        ],
    )
    .await
    .unwrap();

    assert_eq!(answer.written, 2);
    assert_eq!(reached.load(Ordering::SeqCst), 2);
}

fn question(action: &str, resource_type: &str, resource: &str) -> EvaluateRequest {
    EvaluateRequest {
        subject: Some(Subject {
            r#type: "Iam::User".into(),
            id: "grn:globex/iam/user/lee".into(),
        }),
        action: Some(Action {
            name: action.into(),
        }),
        resource: Some(Resource {
            r#type: resource_type.into(),
            id: resource.into(),
            properties: None,
        }),
        context: None,
        read: 0,
    }
}

async fn evaluate(sub: &str, request: EvaluateRequest) -> Result<bool, Code> {
    let (mut client, _) = served().await;
    client
        .evaluate(bearer(request, sub))
        .await
        .map(|answer| answer.into_inner().decision)
        .map_err(|status| status.code())
}

#[tokio::test]
async fn a_scoped_caller_asks_its_services_actions_of_its_organization() {
    let ask = question("cowork:channel.post", "Cowork::Channel", CHANNEL);
    // Nothing is granted in these stores, so the answer is a denial the
    // engine decided, not a refusal of the caller.
    assert_eq!(evaluate(COWORK, ask).await, Ok(false));

    let org = question("cowork:channel.read", "Iam::Org", ACME);
    assert_eq!(evaluate(COWORK, org).await, Ok(false));
}

#[tokio::test]
async fn a_scoped_caller_is_denied_what_is_not_its_services_or_organization() {
    let iam = question("iam:agent.read", "Iam::Agent", "grn:acme/iam/agent/bot");
    assert_eq!(evaluate(COWORK, iam).await, Err(Code::PermissionDenied));

    let foreign = question(
        "cowork:channel.post",
        "Cowork::Channel",
        "grn:globex/cowork/channel/general",
    );
    assert_eq!(evaluate(COWORK, foreign).await, Err(Code::PermissionDenied));

    let foreign_org = question("cowork:channel.read", "Iam::Org", GLOBEX);
    assert_eq!(
        evaluate(COWORK, foreign_org).await,
        Err(Code::PermissionDenied)
    );

    // Control: the platform asks the same of any organization.
    let iam = question("iam:agent.read", "Iam::Agent", "grn:acme/iam/agent/bot");
    assert_eq!(evaluate("platform", iam).await, Ok(false));
}

#[tokio::test]
async fn one_question_out_of_scope_in_a_batch_refuses_the_batch() {
    let (mut client, _) = served().await;
    let entry = |action: &str, resource_type: &str, id: &str| Evaluation {
        subject: None,
        action: Some(Action {
            name: action.into(),
        }),
        resource: Some(Resource {
            r#type: resource_type.into(),
            id: id.into(),
            properties: None,
        }),
        context: None,
    };
    let batch = |evaluations| EvaluationsRequest {
        subject: Some(Subject {
            r#type: "Iam::User".into(),
            id: "grn:acme/iam/user/lee".into(),
        }),
        action: None,
        resource: None,
        context: None,
        evaluations,
        read: 0,
    };

    let own = entry("cowork:channel.post", "Cowork::Channel", CHANNEL);
    let answered = client
        .evaluations(bearer(batch(vec![own.clone()]), COWORK))
        .await
        .unwrap();
    assert_eq!(answered.into_inner().evaluations.len(), 1);

    let foreign = entry("iam:org.read", "Iam::Org", ACME);
    let refused = client
        .evaluations(bearer(batch(vec![own, foreign]), COWORK))
        .await
        .unwrap_err();
    assert_eq!(refused.code(), Code::PermissionDenied);
}

const PAGE: &str = "grn:acme/cowork/page/home";
const MEMBERS: &str = "grn:acme/iam/user/lee";
const NOT_A_MEMBER: &str = "grn:globex/iam/user/mia";

fn entry(entity_type: &str, id: &str, attrs: Value, parents: &[(&str, &str)]) -> Value {
    let parents: Vec<Value> = parents
        .iter()
        .map(|(t, i)| json!({"type": t, "id": i}))
        .collect();
    json!({"uid": {"type": entity_type, "id": id}, "attrs": attrs, "parents": parents})
}

/// Acme with its members, its page, a user who is a member and one who
/// is not, and a role link that makes the member a `Member` of acme.
fn acme() -> (Vec<RoleLink>, Value) {
    let org = json!({"__entity": {"type": "Iam::Org", "id": ACME}});
    let members = json!({"__entity": {"type": "Iam::OrgMembers", "id": ACME}});
    let directory = json!([
        entry("Iam::OrgMembers", ACME, json!({}), &[]),
        entry(
            "Iam::Org",
            ACME,
            json!({"org": org, "members": members}),
            &[]
        ),
        entry(
            "Cowork::Page",
            PAGE,
            json!({"org": org}),
            &[("Iam::Org", ACME)]
        ),
        entry(
            "Iam::User",
            MEMBERS,
            json!({}),
            &[("Iam::OrgMembers", ACME)]
        ),
        entry("Iam::User", NOT_A_MEMBER, json!({}), &[]),
    ]);
    let uid = |t: &str, id: &str| format!("{t}::\"{id}\"").parse::<EntityUid>().unwrap();
    let link = RoleLink {
        id: PolicyId::new("link-lee-acme"),
        role: Role::Member,
        scope: RoleScope::Organization,
        principal: uid("Iam::User", MEMBERS),
        resource: uid("Iam::Org", ACME),
    };
    (vec![link], directory)
}

fn page_read_by(subject: &str) -> EvaluateRequest {
    EvaluateRequest {
        subject: Some(Subject {
            r#type: "Iam::User".into(),
            id: subject.into(),
        }),
        ..question("cowork:page.read", "Cowork::Page", PAGE)
    }
}

/// Prints what the service answered cowork, and asserts each thing it
/// prints. Run it with `--nocapture` to read the answers.
#[tokio::test]
async fn what_the_service_answers_the_cowork_principal() {
    let (links, directory) = acme();
    let (mut client, reached) = serving(links, directory, Some(OrgGrn::new(ACME))).await;

    let allowed = client
        .evaluate(bearer(page_read_by(MEMBERS), COWORK))
        .await
        .unwrap()
        .into_inner();
    let policy_ids = |answer: &server::proto::EvaluateResponse| -> Vec<String> {
        answer
            .context
            .iter()
            .flat_map(|context| &context.reasons)
            .map(|reason| reason.policy_id.clone())
            .collect()
    };
    println!("sub: {COWORK}");
    println!("evaluate member: cowork:page.read on {PAGE} for {MEMBERS}");
    println!("  decision: {}", allowed.decision);
    println!("  reason policy_ids: {:?}", policy_ids(&allowed));
    assert!(allowed.decision);
    assert_eq!(policy_ids(&allowed), ["link-lee-acme"]);

    let denied = client
        .evaluate(bearer(page_read_by(NOT_A_MEMBER), COWORK))
        .await
        .unwrap()
        .into_inner();
    println!("evaluate non-member: cowork:page.read on {PAGE} for {NOT_A_MEMBER}");
    println!("  decision: {}", denied.decision);
    println!("  reason policy_ids: {:?}", policy_ids(&denied));
    assert!(!denied.decision);
    assert!(policy_ids(&denied).contains(&"tenant-isolation".to_owned()));

    let foreign = upsert(
        named("Iam::Team", "grn:acme/iam/team/eng"),
        Some(named("Iam::Org", ACME)),
        vec![named("Iam::Org", ACME)],
    );
    let refused = apply(&mut client, COWORK, vec![foreign]).await.unwrap_err();
    println!("apply_entity_changes: write Iam::Team grn:acme/iam/team/eng");
    println!("  code: {:?}", refused.code());
    println!("  message: {}", refused.message());
    assert_eq!(refused.code(), Code::PermissionDenied);
    assert_eq!(reached.load(Ordering::SeqCst), 0);
}
