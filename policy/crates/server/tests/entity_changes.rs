//! What `ApplyEntityChanges` admits, over a real gRPC connection with
//! the stores faked: this suite reads no database.

#![cfg(test)]

mod caller_tokens;

use std::sync::Arc;
use std::sync::atomic::{AtomicUsize, Ordering};

use caller_tokens::*;
use cedar_policy::{EntityUid, Schema};
use decision::{Cause, Engine, EntitySlice, EntitySource, OrgGrn, OrgPolicies, PolicySource};
use entity::{Applied, EntityChange as StoreChange, Error};
use serde_json::json;
use server::proto::entity_change::Operation;
use server::proto::policy_service_client::PolicyServiceClient;
use server::proto::{
    ApplyEntityChangesRequest, ApplyEntityChangesResponse, EntityChange, EntityDelete, EntityRef,
    EntityUpsert,
};
use server::{Caller, EntitySink, Service, Verifier, authenticated};
use tokio::net::TcpListener;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::{Channel, Server};
use tonic::{Code, Request, Status};

const ACME: &str = "grn:root:default:org:acme";
const ENG: &str = "grn:acme:default:group:eng";

/// Counts what reaches the store, and writes it nowhere.
#[derive(Clone)]
struct Counting {
    engine: Engine,
    reached: Arc<AtomicUsize>,
}

impl PolicySource for Counting {
    async fn active(&self, _: &OrgGrn) -> Result<OrgPolicies, Cause> {
        Ok(OrgPolicies::default())
    }
}

impl EntitySource for Counting {
    async fn slice(&self, _: &EntityUid, _: &EntityUid) -> Result<EntitySlice, Cause> {
        Ok(EntitySlice {
            entities: self.engine.entities(json!([]))?,
            org: None,
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
    let engine = Engine::load().unwrap();
    let reached = Arc::new(AtomicUsize::new(0));
    let stores = Counting {
        engine: engine.clone(),
        reached: reached.clone(),
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

async fn apply(
    client: &mut PolicyServiceClient<Channel>,
    changes: Vec<EntityChange>,
) -> Result<ApplyEntityChangesResponse, Status> {
    let mut request = Request::new(ApplyEntityChangesRequest { changes });
    let value = format!("Bearer {}", signer().sign(&claims()));
    request
        .metadata_mut()
        .insert("authorization", value.parse().unwrap());
    client
        .apply_entity_changes(request)
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
        })),
    }
}

fn team() -> EntityChange {
    upsert(
        named("Team", ENG),
        Some(named("Org", ACME)),
        vec![named("Org", ACME)],
    )
}

#[tokio::test]
async fn a_type_the_caller_does_not_own_refuses_the_whole_batch() {
    let (mut client, reached) = served().await;

    for not_owned in ["Channel", "Page", "Task", "Artifact", "Policy"] {
        let foreign = upsert(
            named(not_owned, "grn:acme:default:thing:one"),
            Some(named("Org", ACME)),
            vec![named("Org", ACME)],
        );
        let refused = apply(&mut client, vec![team(), foreign]).await.unwrap_err();
        assert_eq!(refused.code(), Code::PermissionDenied, "{not_owned}");
    }
    assert_eq!(reached.load(Ordering::SeqCst), 0);

    let control = apply(&mut client, vec![team()]).await.unwrap();
    assert_eq!(control.written, 1);
    assert_eq!(reached.load(Ordering::SeqCst), 1);
}

#[tokio::test]
async fn a_delete_of_a_type_the_caller_does_not_own_is_refused_too() {
    let (mut client, reached) = served().await;

    let delete = |entity_type: &str| EntityChange {
        entity: Some(named(entity_type, ENG)),
        sequence: 1,
        operation: Some(Operation::Delete(EntityDelete {})),
    };

    let refused = apply(&mut client, vec![delete("Channel")])
        .await
        .unwrap_err();
    assert_eq!(refused.code(), Code::PermissionDenied);
    assert_eq!(reached.load(Ordering::SeqCst), 0);

    let control = apply(&mut client, vec![delete("Team")]).await.unwrap();
    assert_eq!(control.written, 1);
}

#[tokio::test]
async fn a_change_at_fault_is_refused_where_it_stands_and_the_rest_are_written() {
    let (mut client, reached) = served().await;

    let no_org = upsert(named("Team", ENG), None, vec![named("Org", ACME)]);
    let no_entity = EntityChange {
        entity: None,
        ..team()
    };
    let no_operation = EntityChange {
        operation: None,
        ..team()
    };
    let unset_sequence = EntityChange {
        sequence: 0,
        ..team()
    };
    let negative_sequence = EntityChange {
        sequence: -1,
        ..team()
    };
    let not_itself = upsert(
        named("Org", ACME),
        Some(named("Org", "grn:root:default:org:globex")),
        vec![],
    );
    let nul = upsert(
        named("Team", "grn:acme:\0"),
        Some(named("Org", ACME)),
        vec![],
    );
    let nul_parent = upsert(
        named("Team", ENG),
        Some(named("Org", ACME)),
        vec![named("Org", "grn:\0")],
    );
    let empty = upsert(named("Team", ""), Some(named("Org", ACME)), vec![]);
    let bad_parent_type = upsert(
        named("Team", ENG),
        Some(named("Org", ACME)),
        vec![named("not a type", ACME)],
    );

    let answer = apply(
        &mut client,
        vec![
            no_org,
            team(),
            no_entity,
            no_operation,
            unset_sequence,
            negative_sequence,
            not_itself,
            nul,
            nul_parent,
            empty,
            bad_parent_type,
            team(),
        ],
    )
    .await
    .unwrap();

    let refused: Vec<u32> = answer
        .refusals
        .iter()
        .map(|refusal| refusal.index)
        .collect();
    assert_eq!(refused, [0, 2, 3, 4, 5, 6, 7, 8, 9, 10]);
    assert!(answer.refusals.iter().all(|r| !r.error.is_empty()));
    assert_eq!(answer.written, 2);
    assert_eq!(reached.load(Ordering::SeqCst), 2);
}

#[tokio::test]
async fn a_type_an_id_and_a_parent_list_are_bounded() {
    let (mut client, reached) = served().await;
    let org = || Some(named("Org", ACME));
    let id = |len: usize| format!("grn:acme:{}", "a".repeat(len - "grn:acme:".len()));
    let at_limit = id(512);
    let parents = |count: usize| vec![named("Org", ACME); count];

    let answer = apply(
        &mut client,
        vec![
            upsert(named("Team", &at_limit), org(), vec![]),
            upsert(named("Team", &id(513)), org(), vec![]),
            upsert(named("Team", ENG), org(), vec![named("Org", &id(513))]),
            upsert(named("Team", ENG), Some(named("Org", &id(513))), vec![]),
            upsert(named("Team", ENG), org(), vec![named("Org", &at_limit)]),
            upsert(
                named("Team", ENG),
                org(),
                vec![named(&"T".repeat(65), ACME)],
            ),
            upsert(
                named("Team", ENG),
                org(),
                vec![named(&"T".repeat(64), ACME)],
            ),
            upsert(named("Team", ENG), org(), parents(1000)),
            upsert(named("Team", ENG), org(), parents(1001)),
        ],
    )
    .await
    .unwrap();

    let refused: Vec<u32> = answer.refusals.iter().map(|r| r.index).collect();
    assert_eq!(refused, [1, 2, 3, 5, 6, 8]);
    let too_long = |index: usize| {
        let refusal = answer.refusals.iter().find(|r| r.index == index as u32);
        refusal.is_some_and(|r| r.error.contains("too long"))
    };
    // A type of 64 bytes is refused by the schema, not for its length.
    assert!([1, 2, 3, 5].iter().all(|i| too_long(*i)));
    assert!(!too_long(6));
    assert_eq!(answer.written, 3);
    assert_eq!(reached.load(Ordering::SeqCst), 3);
}

#[tokio::test]
async fn a_batch_past_the_bound_is_refused() {
    let (mut client, reached) = served().await;

    let at_bound = vec![team(); 1000];
    assert_eq!(apply(&mut client, at_bound).await.unwrap().written, 1000);
    reached.store(0, Ordering::SeqCst);

    let past = vec![team(); 1001];
    let refused = apply(&mut client, past).await.unwrap_err();
    assert_eq!(refused.code(), Code::InvalidArgument);
    assert_eq!(reached.load(Ordering::SeqCst), 0);
}

#[tokio::test]
async fn a_write_with_no_token_reaches_no_store() {
    let (mut client, reached) = served().await;

    let refused = client
        .apply_entity_changes(ApplyEntityChangesRequest {
            changes: vec![team()],
        })
        .await
        .unwrap_err();

    assert_eq!(refused.code(), Code::Unauthenticated);
    assert_eq!(reached.load(Ordering::SeqCst), 0);
}

#[test]
fn every_entity_type_of_the_schema_is_owned_or_stated_as_no_callers() {
    // Cowork's types wait for cowork as a caller, and `Policy` is what
    // `grant` writes for itself (ADR-0140).
    const NO_CALLERS: [&str; 5] = ["Channel", "Page", "Task", "Artifact", "Policy"];
    let source = include_str!("../../../schema/garam.cedarschema");
    let (schema, _) = Schema::from_cedarschema_str(source).unwrap();

    let types: Vec<String> = schema.entity_types().map(ToString::to_string).collect();
    assert!(types.iter().any(|name| name == "Org"), "{types:?}");

    for name in &types {
        let owned = Caller::Platform.owns(name);
        let stated = NO_CALLERS.contains(&name.as_str());
        assert!(owned != stated, "{name}: owned {owned}, stated {stated}");
    }
}
