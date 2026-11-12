//! AuthZEN's HTTP binding (Authorization API 1.0, §5, §6, §7, §9 and
//! §10) beside the gRPC contract. A body is converted to the
//! contract's request and answered by the same service methods, so
//! the two transports share one decision, one entity write and one
//! caller check.

mod wire;

use axum::Router;
use axum::body::Bytes;
use axum::extract::State;
use axum::extract::rejection::BytesRejection;
use axum::http::header::{AUTHORIZATION, CONTENT_TYPE, HOST, WWW_AUTHENTICATE};
use axum::http::{HeaderMap, HeaderName, HeaderValue, StatusCode, Uri};
use axum::response::{IntoResponse, Response};
use axum::routing::{get, post};
use decision::{EntitySource, PolicySource};
use serde::Serialize;
use tonic::Code;

use crate::caller::{Principal, Verifier};
use crate::entity_sink::EntitySink;
use crate::policy_service::Service;
use crate::status::{ServiceError, to_status};

const EVALUATION: &str = "/access/v1/evaluation";
const EVALUATIONS: &str = "/access/v1/evaluations";
const METADATA: &str = "/.well-known/authzen-configuration";
const ENTITY_CHANGES: &str = "/garam/policy/v1/entity-changes";
const REQUEST_ID: HeaderName = HeaderName::from_static("x-request-id");

/// The one table from a gRPC status to an HTTP one. A status the
/// service never returns is a fault of its own.
fn http_status(code: Code) -> StatusCode {
    match code {
        Code::Unauthenticated => StatusCode::UNAUTHORIZED,
        Code::PermissionDenied => StatusCode::FORBIDDEN,
        Code::InvalidArgument => StatusCode::BAD_REQUEST,
        Code::Unavailable => StatusCode::SERVICE_UNAVAILABLE,
        _ => StatusCode::INTERNAL_SERVER_ERROR,
    }
}

/// What a route's handlers share.
struct Binding<P, E> {
    service: Service<P, E>,
    verifier: Verifier,
}

impl<P: Clone, E: Clone> Clone for Binding<P, E> {
    fn clone(&self) -> Self {
        Self {
            service: self.service.clone(),
            verifier: self.verifier.clone(),
        }
    }
}

impl<P, E> Binding<P, E> {
    fn caller(&self, headers: &HeaderMap) -> Result<Principal, ServiceError> {
        let authorization = headers
            .get(AUTHORIZATION)
            .and_then(|value| value.to_str().ok());
        self.verifier
            .authenticate(authorization)
            .map_err(ServiceError::Unauthenticated)
    }
}

/// The binding's routes, to be served beside the contract.
pub(crate) fn router<P, E>(service: Service<P, E>, verifier: Verifier) -> Router
where
    P: PolicySource + Clone + Send + Sync + 'static,
    E: EntitySource + EntitySink + Clone + Send + Sync + 'static,
{
    Router::new()
        .route(METADATA, get(metadata))
        .route(EVALUATION, post(evaluation))
        .route(EVALUATIONS, post(evaluations))
        .route(ENTITY_CHANGES, post(entity_changes))
        .with_state(Binding { service, verifier })
}

async fn metadata(headers: HeaderMap, uri: Uri) -> Response {
    let outcome = authority(&headers, &uri).and_then(|authority| {
        let base = format!("https://{authority}");
        json(&wire::Metadata {
            access_evaluation_endpoint: format!("{base}{EVALUATION}"),
            access_evaluations_endpoint: format!("{base}{EVALUATIONS}"),
            policy_decision_point: base,
        })
    });
    reply(&headers, outcome)
}

/// The authority a client reached this listener by: the metadata names
/// endpoints there, since no setting states the service's address.
fn authority(headers: &HeaderMap, uri: &Uri) -> Result<String, ServiceError> {
    let named = uri
        .authority()
        .map(|authority| authority.as_str())
        .or_else(|| headers.get(HOST).and_then(|host| host.to_str().ok()))
        .unwrap_or_default();
    let valid = named.parse::<axum::http::uri::Authority>().is_ok();
    if named.is_empty() || !valid {
        return Err(ServiceError::Malformed(
            "the request names no host to state the endpoints under".to_owned(),
        ));
    }
    Ok(named.to_owned())
}

async fn evaluation<P, E>(
    State(binding): State<Binding<P, E>>,
    headers: HeaderMap,
    body: Result<Bytes, BytesRejection>,
) -> Response
where
    P: PolicySource + Send + Sync,
    E: EntitySource + Send + Sync,
{
    let outcome = async {
        let principal = binding.caller(&headers)?;
        let request = wire::read::<wire::Evaluation>(&body_of(body)?)?
            .into_proto(binding.service.manifests())?;
        let answer = binding.service.evaluate_one(&principal, &request).await?;
        json(&wire::Decision::from(&answer))
    }
    .await;
    reply(&headers, outcome)
}

async fn evaluations<P, E>(
    State(binding): State<Binding<P, E>>,
    headers: HeaderMap,
    body: Result<Bytes, BytesRejection>,
) -> Response
where
    P: PolicySource + Send + Sync,
    E: EntitySource + Send + Sync,
{
    let outcome = async {
        let principal = binding.caller(&headers)?;
        let request = wire::read::<wire::Evaluations>(&body_of(body)?)?
            .into_proto(binding.service.manifests())?;
        let answers = binding.service.answer_batch(&principal, &request).await?;
        json(&wire::Decisions::from(&answers))
    }
    .await;
    reply(&headers, outcome)
}

async fn entity_changes<P, E>(
    State(binding): State<Binding<P, E>>,
    headers: HeaderMap,
    body: Result<Bytes, BytesRejection>,
) -> Response
where
    P: Send + Sync,
    E: EntitySink + Send + Sync,
{
    let outcome = async {
        let principal = binding.caller(&headers)?;
        let request = wire::read::<wire::Changes>(&body_of(body)?)?
            .into_proto(binding.service.manifests())?;
        let applied = binding.service.apply_changes(&principal, &request).await?;
        json(&wire::Applied::from(&applied))
    }
    .await;
    reply(&headers, outcome)
}

fn body_of(body: Result<Bytes, BytesRejection>) -> Result<Bytes, ServiceError> {
    body.map_err(|rejection| ServiceError::Malformed(rejection.body_text()))
}

fn json(body: &impl Serialize) -> Result<Vec<u8>, ServiceError> {
    serde_json::to_vec(body).map_err(|error| ServiceError::Malformed(error.to_string()))
}

/// Answers a request, echoing its `X-Request-ID` (§10.1.3) on success
/// and on refusal alike. A refusal is the gRPC status's message in
/// plain text under the status the table gives it.
fn reply(headers: &HeaderMap, outcome: Result<Vec<u8>, ServiceError>) -> Response {
    let mut response = match outcome {
        Ok(body) => ([(CONTENT_TYPE, "application/json")], body).into_response(),
        Err(error) => {
            let status = to_status(&error);
            let code = http_status(status.code());
            let mut response = (code, status.message().to_owned()).into_response();
            if code == StatusCode::UNAUTHORIZED {
                response
                    .headers_mut()
                    .insert(WWW_AUTHENTICATE, HeaderValue::from_static("Bearer"));
            }
            response
        }
    };
    if let Some(id) = headers.get(REQUEST_ID) {
        response.headers_mut().insert(REQUEST_ID, id.clone());
    }
    response
}
