//! AuthZEN's HTTP binding over the listener `serve` opens, as a client
//! with no HTTP/2 reaches it. This suite reads no database: what it
//! refuses never reaches a store, and the pool below points at nothing,
//! so a request that is not refused answers 503 and shows that it got
//! past the check under test.

#![cfg(test)]

use std::time::Duration;

mod binding_client;
mod caller_tokens;
mod listening;

use binding_client::{Listening, listening, token_of};
use caller_tokens::{Claims, claims, signer};
use listening::trusting;
use serde_json::{Value, json};
use server::proto::policy_service_client::PolicyServiceClient;
use server::proto::{Action, EvaluateRequest, Resource, Subject};
use sqlx::PgPool;
use sqlx::postgres::PgPoolOptions;
use tonic::{Code, Request};

const EVALUATION: &str = "/access/v1/evaluation";
const EVALUATIONS: &str = "/access/v1/evaluations";
const CHANGES: &str = "/garam/policy/v1/entity-changes";
const COWORK: &str = "grn:acme/iam/service/cowork";
const CHANNEL: &str = "grn:acme/cowork/project/web/channel/general";

fn unreachable_pool() -> PgPool {
    PgPoolOptions::new()
        .acquire_timeout(Duration::from_secs(2))
        .connect_lazy("postgres://nobody@127.0.0.1:1/none?sslmode=disable")
        .unwrap()
}

async fn serving() -> Listening {
    listening(unreachable_pool()).await
}

fn ask(action: &str, resource_type: &str, resource: &str) -> Value {
    json!({
        "subject": {"type": "Iam::User", "id": "grn:acme/iam/user/lee"},
        "action": {"name": action},
        "resource": {"type": resource_type, "id": resource},
    })
}

fn agent_read() -> Value {
    ask("iam:agent.read", "Iam::Agent", "grn:acme/iam/agent/bot")
}

#[tokio::test]
async fn the_metadata_names_the_two_endpoints_and_asks_for_no_token() {
    let service = serving().await;

    let reply = service
        .call("GET", "/.well-known/authzen-configuration", None, &[], None)
        .await;

    assert_eq!(reply.status, 200, "{}", reply.body);
    let base = format!("https://localhost:{}", service.address.port());
    assert_eq!(
        reply.json(),
        json!({
            "policy_decision_point": base,
            "access_evaluation_endpoint": format!("{base}{EVALUATION}"),
            "access_evaluations_endpoint": format!("{base}{EVALUATIONS}"),
        })
    );
}

#[tokio::test]
async fn a_request_id_is_echoed_on_an_answer_and_on_a_refusal() {
    let service = serving().await;
    let id = [("X-Request-ID", "req-1809")];

    let metadata = service
        .call("GET", "/.well-known/authzen-configuration", None, &id, None)
        .await;
    let refused = service
        .call("POST", EVALUATION, None, &id, Some(&agent_read()))
        .await;
    let unsent = service
        .call("POST", EVALUATION, None, &[], Some(&agent_read()))
        .await;

    assert_eq!(refused.status, 401);
    assert_eq!(metadata.header("x-request-id"), Some("req-1809"));
    assert_eq!(refused.header("x-request-id"), Some("req-1809"));
    assert_eq!(unsent.header("x-request-id"), None);
}

#[tokio::test]
async fn no_token_and_a_token_of_another_audience_are_refused() {
    let service = serving().await;
    let other = signer().sign(&Claims {
        aud: "codehub".to_owned(),
        ..claims()
    });
    let right = token_of("platform");

    for path in [EVALUATION, EVALUATIONS, CHANGES] {
        let body = match path {
            CHANGES => json!({"changes": []}),
            _ => agent_read(),
        };
        let none = service.call("POST", path, None, &[], Some(&body)).await;
        let wrong = service
            .call("POST", path, Some(&other), &[], Some(&body))
            .await;
        assert_eq!((none.status, wrong.status), (401, 401), "{path}");
        assert_eq!(none.header("www-authenticate"), Some("Bearer"), "{path}");
    }

    // Control: the right audience is past the check, and reaches the
    // store that is out of reach.
    let reached = service
        .call("POST", EVALUATION, Some(&right), &[], Some(&agent_read()))
        .await;
    assert_eq!(reached.status, 503, "{}", reached.body);
}

#[tokio::test]
async fn the_token_is_checked_before_the_body_is_read() {
    let service = serving().await;
    let garbage = Value::String("not an object".to_owned());

    let unauthenticated = service
        .call("POST", EVALUATION, None, &[], Some(&garbage))
        .await;
    let malformed = service
        .call(
            "POST",
            EVALUATION,
            Some(&token_of("platform")),
            &[],
            Some(&garbage),
        )
        .await;

    assert_eq!(unauthenticated.status, 401);
    assert_eq!(malformed.status, 400);
}

#[tokio::test]
async fn a_scoped_caller_asking_outside_its_bound_is_answered_as_grpc_answers() {
    let service = serving().await;
    let token = token_of(COWORK);
    let own = ask("cowork:channel.post", "Cowork::Channel", CHANNEL);
    let foreign = [
        ask(
            "cowork:channel.post",
            "Cowork::Channel",
            "grn:globex/cowork/project/web/channel/general",
        ),
        ask("iam:agent.read", "Iam::Agent", "grn:acme/iam/agent/bot"),
    ];

    let mut grpc = PolicyServiceClient::new(trusting(&service.cert, service.address).await);
    for body in &foreign {
        let over_http = service
            .call("POST", EVALUATION, Some(&token), &[], Some(body))
            .await;
        let over_grpc = grpc
            .evaluate(bearing(&token, evaluate_request(body)))
            .await
            .unwrap_err();
        assert_eq!(over_grpc.code(), Code::PermissionDenied);
        assert_eq!(over_http.status, 403, "{}", over_http.body);
        assert_eq!(over_http.body, over_grpc.message());
    }

    // Control: its own organization is past the check and reaches the
    // store that is out of reach.
    let reached = service
        .call("POST", EVALUATION, Some(&token), &[], Some(&own))
        .await;
    assert_eq!(reached.status, 503, "{}", reached.body);

    // One entry outside refuses a batch, as it does over gRPC.
    let batch = json!({
        "subject": {"type": "Iam::User", "id": "grn:acme/iam/user/lee"},
        "evaluations": [
            {"action": {"name": "cowork:channel.post"},
             "resource": {"type": "Cowork::Channel",
                          "id": "grn:globex/cowork/project/web/channel/general"}},
            {"action": {"name": "cowork:channel.post"},
             "resource": {"type": "Cowork::Channel", "id": CHANNEL}},
        ],
    });
    let refused = service
        .call("POST", EVALUATIONS, Some(&token), &[], Some(&batch))
        .await;
    assert_eq!(refused.status, 403, "{}", refused.body);
}

fn evaluate_request(body: &Value) -> EvaluateRequest {
    let text = |pointer: &str| body.pointer(pointer).unwrap().as_str().unwrap().into();
    EvaluateRequest {
        subject: Some(Subject {
            r#type: text("/subject/type"),
            id: text("/subject/id"),
        }),
        action: Some(Action {
            name: text("/action/name"),
        }),
        resource: Some(Resource {
            r#type: text("/resource/type"),
            id: text("/resource/id"),
            properties: None,
        }),
        context: None,
        read: 0,
        probe: false,
    }
}

fn bearing<T>(token: &str, message: T) -> Request<T> {
    let mut request = Request::new(message);
    request
        .metadata_mut()
        .insert("authorization", format!("Bearer {token}").parse().unwrap());
    request
}

#[tokio::test]
async fn an_evaluations_semantic_other_than_execute_all_is_a_bad_request() {
    let service = serving().await;
    let token = token_of("platform");
    let with = |semantic: Option<&str>| {
        let mut body = agent_read();
        if let Some(semantic) = semantic {
            body["options"] = json!({"evaluations_semantic": semantic});
        }
        body
    };

    for semantic in ["deny_on_first_deny", "permit_on_first_permit", "other"] {
        let reply = service
            .call(
                "POST",
                EVALUATIONS,
                Some(&token),
                &[],
                Some(&with(Some(semantic))),
            )
            .await;
        assert_eq!(reply.status, 400, "{semantic}: {}", reply.body);
    }

    // Control: execute_all, named or left to the default, is served
    // and reaches the store that is out of reach.
    for semantic in [Some("execute_all"), None] {
        let reply = service
            .call(
                "POST",
                EVALUATIONS,
                Some(&token),
                &[],
                Some(&with(semantic)),
            )
            .await;
        assert_eq!(reply.status, 503, "{semantic:?}: {}", reply.body);
    }
}

#[tokio::test]
async fn a_body_that_is_not_a_request_is_a_bad_request() {
    let service = serving().await;
    let token = token_of("platform");
    let mut grounds = agent_read();
    grounds["subject"]["properties"] = json!({"role": "owner"});
    let mut unknown = agent_read();
    unknown["unrecognised"] = json!(true);

    for body in [json!([]), json!({"subject": "lee"}), grounds] {
        let reply = service
            .call("POST", EVALUATION, Some(&token), &[], Some(&body))
            .await;
        assert_eq!(reply.status, 400, "{body}: {}", reply.body);
    }

    // A change is an upsert or a delete, not both.
    let both = json!({"changes": [{
        "entity": {"type": "Cowork::Channel", "id": CHANNEL},
        "sequence": "1",
        "upsert": {},
        "delete": {},
    }]});
    let reply = service
        .call("POST", CHANGES, Some(&token), &[], Some(&both))
        .await;
    assert_eq!(reply.status, 400, "{}", reply.body);

    // Control: a field the binding does not know is ignored, as the
    // spec requires, and the request reaches the store.
    let reply = service
        .call("POST", EVALUATION, Some(&token), &[], Some(&unknown))
        .await;
    assert_eq!(reply.status, 503, "{}", reply.body);
}

#[tokio::test]
async fn a_change_of_a_type_a_scoped_caller_does_not_own_is_refused() {
    let service = serving().await;
    let token = token_of(COWORK);
    let change = |entity_type: &str, id: &str| {
        json!({
            "changes": [{
                "entity": {"type": entity_type, "id": id},
                "sequence": "1",
                "upsert": {
                    "org": {"type": "Iam::Org", "id": "grn:root/iam/org/acme"},
                    "parents": [{"type": "Iam::Org", "id": "grn:root/iam/org/acme"}],
                },
            }],
        })
    };

    let team = change("Iam::Team", "grn:acme/iam/team/eng");
    let refused = service
        .call("POST", CHANGES, Some(&token), &[], Some(&team))
        .await;
    assert_eq!(refused.status, 403, "{}", refused.body);

    // Control: a type it owns, in its organization, reaches the store.
    let owned = change("Cowork::Channel", CHANNEL);
    let reached = service
        .call("POST", CHANGES, Some(&token), &[], Some(&owned))
        .await;
    assert_eq!(reached.status, 503, "{}", reached.body);
}

#[tokio::test]
async fn a_request_over_the_bound_is_a_bad_request() {
    let service = serving().await;
    let token = token_of("platform");
    let entries = |count: usize| {
        let entry = json!({"action": {"name": "iam:agent.read"}});
        json!({
            "subject": {"type": "Iam::User", "id": "grn:acme/iam/user/lee"},
            "resource": {"type": "Iam::Agent", "id": "grn:acme/iam/agent/bot"},
            "evaluations": vec![entry; count],
        })
    };

    let over = service
        .call("POST", EVALUATIONS, Some(&token), &[], Some(&entries(1001)))
        .await;
    assert_eq!(over.status, 400, "{}", over.body);
}

#[tokio::test]
async fn an_unreadable_store_is_service_unavailable() {
    let service = serving().await;

    let reply = service
        .call(
            "POST",
            EVALUATION,
            Some(&token_of("platform")),
            &[],
            Some(&agent_read()),
        )
        .await;

    assert_eq!(reply.status, 503, "{}", reply.body);
}

const CODEHUB: &str = "grn:acme/iam/service/codehub";
const REPO: &str = "grn:acme/codehub/repo/4821";

#[tokio::test]
async fn the_type_grn_is_resolved_before_the_store_is_reached() {
    let service = serving().await;
    let token = token_of(CODEHUB);
    let by = |subject: &str, resource: &str| {
        json!({
            "subject": {"type": "grn", "id": subject},
            "action": {"name": "codehub:repo.read"},
            "resource": {"type": "grn", "id": resource},
        })
    };

    for body in [
        by("grn:acme/iam/user/lee", "grn:acme/billing/invoice/1"),
        by("grn:acme/iam/user/lee", "grn:acme/codehub/branch/main"),
        by("grn:acme/iam/user", REPO),
    ] {
        let reply = service
            .call("POST", EVALUATION, Some(&token), &[], Some(&body))
            .await;
        assert_eq!(reply.status, 400, "{body}: {}", reply.body);
    }

    // Control: declared GRNs, and a Cedar type name, are past the
    // check and reach the store that is out of reach.
    let declared = by("grn:acme/iam/user/lee", REPO);
    let named = ask("codehub:repo.read", "Codehub::Repo", REPO);
    for body in [declared, named] {
        let reply = service
            .call("POST", EVALUATION, Some(&token), &[], Some(&body))
            .await;
        assert_eq!(reply.status, 503, "{body}: {}", reply.body);
    }
}

#[tokio::test]
async fn the_codehub_caller_is_scoped_to_its_service_and_its_type() {
    let service = serving().await;
    let token = token_of(CODEHUB);
    let iam = ask("iam:agent.read", "Iam::Agent", "grn:acme/iam/agent/bot");
    let cowork = ask(
        "cowork:channel.post",
        "Cowork::Channel",
        "grn:acme/cowork/project/web/channel/general",
    );
    let feed = |kind: &str, id: &str| {
        json!({"changes": [{
            "entity": {"type": kind, "id": id},
            "sequence": "1",
            "upsert": {},
        }]})
    };

    for body in [&iam, &cowork] {
        let reply = service
            .call("POST", EVALUATION, Some(&token), &[], Some(body))
            .await;
        assert_eq!(reply.status, 403, "{body}: {}", reply.body);
    }
    for body in [
        feed("Iam::User", "grn:acme/iam/user/new"),
        feed("Cowork::Channel", "grn:acme/cowork/project/web/channel/x"),
        feed("grn", "grn:acme/iam/user/new"),
    ] {
        let reply = service
            .call("POST", CHANGES, Some(&token), &[], Some(&body))
            .await;
        assert_eq!(reply.status, 403, "{body}: {}", reply.body);
    }

    // Control: its own type, named either way, reaches the store.
    for kind in ["Codehub::Repo", "grn"] {
        let reply = service
            .call("POST", CHANGES, Some(&token), &[], Some(&feed(kind, REPO)))
            .await;
        assert_eq!(reply.status, 503, "{kind}: {}", reply.body);
    }
}
