//! Cedar's symbolic analysis of the system tier (ADR-0130 §5): each
//! property below is proved by cvc5 for every request and every entity
//! store the schema admits, not for the fixture's. It holds of a link
//! of each template, so it covers an organization's links as far as a
//! link differs only in the entities it names. It says nothing of
//! what the store holds, or of a request the schema refuses: those are
//! `tests/` beside this file. `make -C policy suites` runs it.

#![cfg(test)]

mod fixture;

use cedar_policy::{
    Authorizer, Decision, EntityUid, Policy, PolicyId, PolicySet, RequestEnv, Schema,
};
use cedar_policy_symcc::solver::LocalSolver;
use cedar_policy_symcc::{CedarSymCompiler, CompiledPolicy, CompiledPolicySet};
use decision::{Engine, Role, RoleLink, RoleScope};
use fixture::{ACME, ENG, LEE, Named, World, link, policy, uid};

type Compiler = CedarSymCompiler<LocalSolver>;

const EVERYTHING: &str = "permit (principal, action, resource);";
const MEMBERS_ONLY: &str =
    "permit (principal, action, resource) when { principal in resource.org.members };";
/// ADR-0137's rejected first draft of tenant isolation.
const CONTAINMENT: &str =
    "forbid (principal, action, resource) unless { principal in resource.org };";
const ROLES: [Role; 3] = [Role::Member, Role::Admin, Role::Owner];

const ROLE_PAIRS: [(Role, Role); 2] = [(Role::Member, Role::Admin), (Role::Admin, Role::Owner)];

fn compiler() -> Compiler {
    CedarSymCompiler::new(LocalSolver::cvc5().expect("CVC5 names cvc5")).expect("cvc5 starts")
}

fn set(sources: &[&str]) -> PolicySet {
    let mut policies = PolicySet::new();
    for (index, source) in sources.iter().enumerate() {
        let id = PolicyId::new(format!("policy{index}"));
        policies
            .add(Policy::parse(Some(id), *source).expect("the policy parses"))
            .expect("the id is free");
    }
    policies
}

/// A link of every template, as an organization's links reach a
/// composition.
fn every_link() -> Vec<RoleLink> {
    let named = |prefix: &str, role: Role, base: RoleLink| RoleLink {
        id: PolicyId::new(format!("{prefix}-{role:?}")),
        ..base
    };
    ROLES
        .into_iter()
        .flat_map(|role| {
            [
                named("org", role, link(role, RoleScope::Organization, ENG, ACME)),
                named("team", role, link(role, RoleScope::TeamOrProject, LEE, ENG)),
            ]
        })
        .collect()
}

fn composed(world: &World, links: &[RoleLink], guardrails: &[Policy]) -> PolicySet {
    world.compose(links, guardrails).policy_set().clone()
}

fn every_request(schema: &Schema) -> Vec<RequestEnv> {
    let requests: Vec<RequestEnv> = schema.request_envs().collect();
    println!("analysis: {} request environments", requests.len());
    assert!(!requests.is_empty(), "the schema admits no request");
    requests
}

fn compiled(policies: &PolicySet, request: &RequestEnv, schema: &Schema) -> CompiledPolicySet {
    CompiledPolicySet::compile(policies, request, schema).expect("the policies compile")
}

/// A `forbid` that errors is skipped, so isolation is only as strong as
/// this (ADR-0137). Every policy of the tier and every link is proved
/// never to error, since the engine would answer such an error Deny.
#[tokio::test]
#[ignore = "needs cvc5: make -C policy suites"]
async fn no_system_policy_and_no_link_can_error_on_a_request_the_schema_admits() {
    let world = World::new();
    let schema = world.engine.schema();
    let policies = composed(&world, &every_link(), &[]);
    let mut proved = 0;
    let mut solver = compiler();
    for request in every_request(schema) {
        for policy in policies.policies() {
            let compiled =
                CompiledPolicy::compile(policy, &request, schema).expect("the policy compiles");
            let never = solver.check_never_errors_opt(&compiled).await.unwrap();
            assert!(never, "{} errors on {request:?}", policy.id());
            proved += 1;
        }
    }
    println!("analysis: {proved} proofs");
}

/// ADR-0130 §5's global question: can anything outside an organization
/// reach a resource? The forbid is held against `permit` on everything,
/// which is wider than any set of links an organization can hold, so
/// the answer covers every organization's policies.
#[tokio::test]
#[ignore = "needs cvc5: make -C policy suites"]
async fn nothing_reaches_a_resource_but_a_member_of_its_organization() {
    let world = World::new();
    let schema = world.engine.schema();
    let widest = composed(&world, &every_link(), &[policy("everything", EVERYTHING)]);
    let members = set(&[MEMBERS_ONLY]);
    let mut solver = compiler();
    assert!(implied_everywhere(&mut solver, schema, &widest, &members).await);
}

/// The control: the same question of the form ADR-0137 rejected is
/// refuted, and what the solver found is a real Allow.
#[tokio::test]
#[ignore = "needs cvc5: make -C policy suites"]
async fn the_analysis_refutes_isolation_read_through_containment() {
    let world = World::new();
    let schema = world.engine.schema();
    let contained = set(&[EVERYTHING, CONTAINMENT]);
    let members = set(&[MEMBERS_ONLY]);
    let mut solver = compiler();
    let request = request_on(schema, "agent:read", "User", "Agent");

    let counterexample = solver
        .check_implies_with_counterexample_opt(
            &compiled(&contained, &request, schema),
            &compiled(&members, &request, schema),
        )
        .await
        .unwrap()
        .expect("containment admits someone outside the members");

    let answer = Authorizer::new().is_authorized(
        &counterexample.request,
        &contained,
        &counterexample.entities,
    );
    assert_eq!(answer.decision(), Decision::Allow);
    let denied = Authorizer::new().is_authorized(
        &counterexample.request,
        world
            .compose(&[], &[policy("everything", EVERYTHING)])
            .policy_set(),
        &counterexample.entities,
    );
    assert_eq!(denied.decision(), Decision::Deny);
}

fn request_on(schema: &Schema, action: &str, principal: &str, resource: &str) -> RequestEnv {
    schema
        .request_envs()
        .find(|request| {
            request.action().id().unescaped() == action
                && request.principal().to_string() == principal
                && request.resource().to_string() == resource
        })
        .expect("the schema admits this request")
}

fn action_of(request: &RequestEnv) -> String {
    request.action().id().unescaped().to_owned()
}

/// With no link, the only thing the tier allows is an agent's sending
/// (ADR-0024), so every other grant is a link someone can read.
#[tokio::test]
#[ignore = "needs cvc5: make -C policy suites"]
async fn the_tier_alone_allows_only_an_agents_sending() {
    let world = World::new();
    let schema = world.engine.schema();
    let tier = composed(&world, &[], &[]);
    let mut solver = compiler();
    let mut sending = 0;
    let mut proved = 0;
    for request in every_request(schema) {
        let denied = solver
            .check_always_denies_opt(&compiled(&tier, &request, schema))
            .await
            .unwrap();
        if action_of(&request) == "message:send" {
            assert!(!denied, "the tier stopped allowing an agent's sending");
            sending += 1;
        } else {
            assert!(denied, "the tier alone allows on {request:?}");
            proved += 1;
        }
    }
    assert_eq!(sending, 1);
    println!("analysis: {proved} proofs");
}

/// `agent:watch` is carried by a grant or by nothing (ADR-0022): no
/// role's link reaches it.
#[tokio::test]
#[ignore = "needs cvc5: make -C policy suites"]
async fn no_role_holds_agent_watch() {
    let world = World::new();
    let schema = world.engine.schema();
    let linked = composed(&world, &every_link(), &[]);
    let mut solver = compiler();
    let requests: Vec<RequestEnv> = every_request(schema)
        .into_iter()
        .filter(|request| action_of(request) == "agent:watch")
        .collect();
    assert_eq!(requests.len(), 4);
    for request in requests {
        let denied = solver
            .check_always_denies_opt(&compiled(&linked, &request, schema))
            .await
            .unwrap();
        assert!(denied, "a role reaches agent:watch on {request:?}");
    }
}

/// One link, with nothing else, on the same subject and resource.
fn one_link(engine: &Engine, role: Role, scope: RoleScope, resource: Named) -> PolicySet {
    let mut policies = engine
        .compose(&[link(role, scope, ENG, resource)], &[])
        .expect("the link composes")
        .policy_set()
        .clone();
    for id in ["tenant-isolation", "agent-message-send"] {
        policies
            .remove_static(PolicyId::new(id))
            .expect("the tier holds it");
    }
    policies
}

async fn implied_everywhere(
    solver: &mut Compiler,
    schema: &Schema,
    narrow: &PolicySet,
    wide: &PolicySet,
) -> bool {
    for request in every_request(schema) {
        let narrow = compiled(narrow, &request, schema);
        let wide = compiled(wide, &request, schema);
        if !solver.check_implies_opt(&narrow, &wide).await.unwrap() {
            return false;
        }
    }
    true
}

/// Each role holds all the one below it holds, which nothing in the
/// schema states: the action groups say it only action by action.
#[tokio::test]
#[ignore = "needs cvc5: make -C policy suites"]
async fn each_role_holds_what_the_role_below_it_holds() {
    let world = World::new();
    let engine = &world.engine;
    let schema = engine.schema();
    let mut solver = compiler();
    for (scope, resource) in [
        (RoleScope::Organization, ACME),
        (RoleScope::TeamOrProject, ENG),
    ] {
        for (below, above) in ROLE_PAIRS {
            let narrow = one_link(engine, below, scope, resource);
            let wide = one_link(engine, above, scope, resource);
            assert!(
                implied_everywhere(&mut solver, schema, &narrow, &wide).await,
                "{above:?} does not hold what {below:?} holds"
            );
            // The control: the order matters, so the proof is not of
            // two sets that happen to be equal.
            assert!(
                !implied_everywhere(&mut solver, schema, &wide, &narrow).await,
                "{below:?} holds what {above:?} holds"
            );
        }
    }
}

/// A team's or a project's role reaches what it names and nothing
/// inside it (ADR-0137), where an organization's reaches what it
/// contains.
#[tokio::test]
#[ignore = "needs cvc5: make -C policy suites"]
async fn a_scoped_role_reaches_only_what_it_names() {
    let world = World::new();
    let engine = &world.engine;
    let schema = engine.schema();
    let team: EntityUid = uid(ENG);
    let organization: EntityUid = uid(ACME);
    let only_team = set(&[&format!("permit (principal, action, resource == {team});")]);
    let only_organization = set(&[&format!(
        "permit (principal, action, resource == {organization});"
    )]);
    let mut solver = compiler();

    let scoped = one_link(engine, Role::Owner, RoleScope::TeamOrProject, ENG);
    assert!(implied_everywhere(&mut solver, schema, &scoped, &only_team).await);

    let whole = one_link(engine, Role::Owner, RoleScope::Organization, ACME);
    assert!(!implied_everywhere(&mut solver, schema, &whole, &only_organization).await);
}
