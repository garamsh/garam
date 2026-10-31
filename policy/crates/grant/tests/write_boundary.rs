//! What reaches the store: only a body the boundary admitted.

#![cfg(test)]

use std::str::FromStr;

use cedar_policy::{EntityId, EntityTypeName, EntityUid};
use decision::{Engine, EntityRef, Evaluation, OrgGrn, Role, RoleScope};
use grant::{Body, Error};

const ACME: &str = "grn:root:default:org:acme";

fn uid(entity_type: &str, id: &str) -> EntityUid {
    EntityUid::from_type_name_and_id(
        EntityTypeName::from_str(entity_type).unwrap(),
        EntityId::new(id),
    )
}

fn acme() -> OrgGrn {
    OrgGrn::new(ACME)
}

fn org() -> EntityUid {
    uid("Org", ACME)
}

fn user() -> EntityUid {
    uid("User", "grn:acme:default:user:lee")
}

fn team() -> EntityUid {
    uid("Team", "grn:acme:default:group:eng")
}

#[test]
fn an_org_as_a_links_principal_is_refused() {
    let engine = Engine::load().unwrap();

    let refused = Body::role_link(
        &engine,
        &acme(),
        Role::Member,
        RoleScope::Organization,
        org(),
        org(),
    );
    assert!(
        matches!(
            refused,
            Err(Error::Refused(decision::Error::RoleLinkPrincipal { .. }))
        ),
        "{refused:?}"
    );

    for principal in [user(), team()] {
        let control = Body::role_link(
            &engine,
            &acme(),
            Role::Member,
            RoleScope::Organization,
            principal,
            org(),
        );
        assert!(control.is_ok(), "{control:?}");
    }
}

#[test]
fn a_links_resource_outside_its_template_set_is_refused() {
    let engine = Engine::load().unwrap();

    let refused = Body::role_link(
        &engine,
        &acme(),
        Role::Admin,
        RoleScope::Organization,
        user(),
        team(),
    );
    assert!(
        matches!(
            refused,
            Err(Error::Refused(decision::Error::RoleLinkScope { .. }))
        ),
        "{refused:?}"
    );

    let control = Body::role_link(
        &engine,
        &acme(),
        Role::Admin,
        RoleScope::TeamOrProject,
        user(),
        team(),
    );
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn a_permit_guardrail_is_refused() {
    let engine = Engine::load().unwrap();

    let refused = Body::guardrail(&engine, &acme(), "permit (principal, action, resource);");
    assert!(
        matches!(refused, Err(Error::GuardrailPermits)),
        "{refused:?}"
    );

    let control = Body::guardrail(&engine, &acme(), "forbid (principal, action, resource);");
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn a_guardrail_the_schema_does_not_admit_is_refused() {
    let engine = Engine::load().unwrap();

    let refused = Body::guardrail(
        &engine,
        &acme(),
        r#"forbid (principal, action == Action::"no:such", resource);"#,
    );
    assert!(
        matches!(refused, Err(Error::Refused(decision::Error::Validation(_)))),
        "{refused:?}"
    );

    let refused = Body::guardrail(
        &engine,
        &acme(),
        "forbid (principal is User, action, resource) when { principal.org == resource.org };",
    );
    assert!(
        matches!(refused, Err(Error::Refused(decision::Error::Validation(_)))),
        "{refused:?}"
    );

    let control = Body::guardrail(
        &engine,
        &acme(),
        r#"forbid (principal is Agent, action == Action::"message:send", resource is Agent)
           when { principal.org != resource.org };"#,
    );
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn text_that_is_not_one_policy_is_refused() {
    let engine = Engine::load().unwrap();

    let refused = Body::guardrail(&engine, &acme(), "forbid (principal");
    assert!(
        matches!(refused, Err(Error::GuardrailSyntax(_))),
        "{refused:?}"
    );

    let two = "forbid (principal, action, resource); forbid (principal, action, resource);";
    let refused = Body::guardrail(&engine, &acme(), two);
    assert!(
        matches!(refused, Err(Error::GuardrailSyntax(_))),
        "{refused:?}"
    );
}

#[test]
fn a_link_naming_another_organizations_entity_is_refused() {
    let engine = Engine::load().unwrap();
    let link = |principal, resource, scope| {
        Body::role_link(&engine, &acme(), Role::Member, scope, principal, resource)
    };

    let globex = uid("Org", "grn:root:default:org:globex");
    let their_team = uid("Team", "grn:globex:default:group:secret");
    for refused in [
        link(user(), globex, RoleScope::Organization),
        link(user(), their_team.clone(), RoleScope::TeamOrProject),
        link(their_team, org(), RoleScope::Organization),
        link(
            uid("Agent", "grn:globex:default:agent:rival"),
            org(),
            RoleScope::Organization,
        ),
    ] {
        assert!(
            matches!(refused, Err(Error::ForeignEntity(_))),
            "{refused:?}"
        );
    }

    // A user belongs to no one organization, whatever its GRN names.
    let elsewhere = uid("User", "grn:globex:default:user:mia");
    let control = link(elsewhere, org(), RoleScope::Organization);
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn a_guardrail_naming_another_organizations_entity_is_refused() {
    let engine = Engine::load().unwrap();

    for source in [
        r#"forbid (principal, action, resource in Org::"grn:root:default:org:globex");"#,
        r#"forbid (principal in Team::"grn:globex:default:group:secret", action, resource);"#,
        r#"forbid (principal, action, resource)
           when { principal in Team::"grn:globex:default:group:secret" };"#,
    ] {
        let refused = Body::guardrail(&engine, &acme(), source);
        assert!(
            matches!(refused, Err(Error::ForeignEntity(_))),
            "{refused:?}"
        );
    }

    let control = Body::guardrail(
        &engine,
        &acme(),
        r#"forbid (principal in Team::"grn:acme:default:group:eng", action, resource in Org::"grn:root:default:org:acme");"#,
    );
    assert!(control.is_ok(), "{control:?}");
}

#[test]
fn a_guardrail_past_the_bounds_is_refused_before_it_is_parsed() {
    let engine = Engine::load().unwrap();
    let forbid = |condition: String| {
        let source = format!("forbid (principal, action, resource) when {{ {condition} }};");
        Body::guardrail(&engine, &acme(), &source)
    };

    let nested = format!("{}true{}", "(".repeat(200), ")".repeat(200));
    assert!(matches!(forbid(nested), Err(Error::GuardrailTooDeep)));
    let branches = format!(
        "{}true{}",
        "if true then ".repeat(100),
        " else false".repeat(100)
    );
    assert!(matches!(forbid(branches), Err(Error::GuardrailTooDeep)));

    let long = vec!["true"; 700].join(" && ");
    assert!(matches!(forbid(long), Err(Error::GuardrailTooDeep)));

    let control = forbid(format!("{}true{}", "(".repeat(8), ")".repeat(8)));
    assert!(control.is_ok(), "{control:?}");
    let control = forbid(vec!["true"; 400].join(" && "));
    assert!(control.is_ok(), "{control:?}");
}

/// A guardrail filled to a bound, then parsed, validated, composed and
/// decided on half a task's 2 MiB stack, in the unoptimised build,
/// which recurses deeper. An overflow here aborts the test binary.
#[test]
fn a_guardrail_at_the_bounds_decides_on_half_a_tasks_stack() {
    const LONGEST: usize = 4 * 1024 - 60;
    let chain = |term: &str, joiner: &str, last: &str| {
        let terms = (LONGEST - last.len()) / (term.len() + joiner.len());
        format!("{}{last}", format!("{term}{joiner}").repeat(terms))
    };
    let shapes = [
        format!("{}true{}", "(".repeat(11), ")".repeat(11)),
        format!(
            "{}true{}",
            "if true then ".repeat(11),
            " else false".repeat(11)
        ),
        format!("{}[]{}", "[".repeat(10), "]".repeat(10)),
        chain("true", "&&", "true"),
        chain("false", "||", "false"),
        format!("{}==0", chain("1", "+", "1")),
        format!("{}==0", chain("1", "*", "1")),
        format!("{}==0", chain("1", "-", "1")),
        format!("[]{}.isEmpty()", ".contains(1)".repeat((LONGEST - 12) / 12)),
        format!("principal{}==1", ".a".repeat((LONGEST - 20) / 2)),
    ];
    for shape in shapes {
        let source = format!("forbid (principal, action, resource) when {{ {shape} }};");
        let decided = std::thread::Builder::new()
            .stack_size(1024 * 1024)
            .spawn(move || decide_on(&source))
            .unwrap()
            .join();
        assert!(decided.is_ok());
    }
}

fn decide_on(source: &str) {
    let engine = Engine::load().unwrap();
    let body = Body::guardrail(&engine, &acme(), source);
    assert!(
        !matches!(body, Err(Error::GuardrailTooDeep)),
        "the shape did not reach the parser: {} bytes",
        source.len()
    );
    let policy = cedar_policy::Policy::parse(None, source)
        .unwrap_or_else(|error| panic!("{}: {error}", &source[..80]));
    let composed = engine.compose(&[], &[policy]).unwrap();
    let entities = engine
        .entities(serde_json::json!([
            {"uid": {"type": "OrgMembers", "id": ACME}, "attrs": {}, "parents": []},
            {
                "uid": {"type": "Org", "id": ACME},
                "attrs": {
                    "org": {"__entity": {"type": "Org", "id": ACME}},
                    "members": {"__entity": {"type": "OrgMembers", "id": ACME}},
                },
                "parents": [],
            },
            {
                "uid": {"type": "User", "id": "u"},
                "attrs": {},
                "parents": [{"type": "OrgMembers", "id": ACME}],
            },
        ]))
        .unwrap();
    let request = engine
        .request(&Evaluation {
            subject: EntityRef {
                entity_type: "User",
                id: "u",
            },
            action: "org:read",
            resource: EntityRef {
                entity_type: "Org",
                id: ACME,
            },
            context: serde_json::json!({}),
        })
        .unwrap();
    engine.decide(&request, &composed, &entities);
}
