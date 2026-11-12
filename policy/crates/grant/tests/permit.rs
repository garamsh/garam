//! Writing a `permit` (ADR-0144): the right to write one is asked of
//! the engine, and the body is scoped to the organization. Over
//! in-memory stores, so it needs no Postgres.

#![cfg(test)]

mod fixture;

use cedar_policy::{Policy, PolicyId};
use decision::{EntityRef, Evaluation, OrgPolicies, Outcome};
use fixture::*;
use grant::{Body, Error};
use serde_json::json;

#[tokio::test]
async fn only_the_owner_holds_the_right_to_write_a_permit() {
    let world = World::new();

    assert!(world.right(OWNER).await.is_ok());
    for refused in [ADMIN, MEMBER, STRANGER] {
        let right = world.right(refused).await;
        assert!(
            matches!(right, Err(Error::PermitDenied { .. })),
            "{refused:?}: {right:?}"
        );
    }

    // The administrator holds `policy-*`, so it is the action's own
    // place outside that group that refuses it.
    let evaluation = |action| Evaluation {
        subject: EntityRef {
            entity_type: ADMIN.0,
            id: ADMIN.1,
        },
        action,
        resource: EntityRef {
            entity_type: ORG.0,
            id: ORG.1,
        },
        context: json!({}),
    };
    for (action, outcome) in [
        ("iam:policy-update", Outcome::Allow),
        ("iam:policy-permit", Outcome::Deny),
    ] {
        let decision = world
            .engine
            .evaluate(&world, &world, &evaluation(action))
            .await
            .unwrap();
        assert_eq!(decision.outcome, outcome, "{action}: {decision:?}");
    }
}

#[tokio::test]
async fn a_permit_names_what_a_manifest_declares_inside_the_organization() {
    let world = World::new();
    let right = world.owner_right().await;
    let permit = |source: &str| Body::permit(&world.engine, &right, source);
    let on = |scope: &str| {
        format!(r#"permit (principal, action == Iam::Action::"group-read", resource{scope});"#)
    };

    let inside = [
        on(r#" == Iam::Team::"grn:acme/iam/group/eng""#),
        on(r#" in Iam::Org::"grn:root/iam/org/acme""#),
        on(r#" is Iam::Team in Iam::Org::"grn:root/iam/org/acme""#),
    ];
    for source in &inside {
        let control = permit(source);
        assert!(control.is_ok(), "{source}: {control:?}");
    }

    let foreign = [
        on(r#" in Iam::Org::"grn:root/iam/org/globex""#),
        on(r#" == Iam::Team::"grn:globex/iam/group/secret""#),
        on(r#" is Iam::Team in Iam::Org::"grn:root/iam/org/globex""#),
        on(r#" == Iam::User::"grn:globex/iam/user/mia""#),
        on(r#" in Iam::Org::"grn:root/iam/org/acme-two""#),
    ];
    for source in &foreign {
        let refused = permit(source);
        assert!(
            matches!(refused, Err(Error::ForeignEntity(_))),
            "{source}: {refused:?}"
        );
    }
}

#[tokio::test]
async fn a_permit_with_no_entity_in_its_resource_scope_is_refused() {
    let world = World::new();
    let right = world.owner_right().await;
    let permit = |scope: &str| {
        let source =
            format!(r#"permit (principal, action == Iam::Action::"group-read", {scope});"#);
        Body::permit(&world.engine, &right, &source)
    };

    let unscoped = permit("resource");
    assert!(
        matches!(unscoped, Err(Error::PermitResourceUnscoped)),
        "{unscoped:?}"
    );
    let by_type = permit("resource is Iam::Team");
    assert!(
        matches!(by_type, Err(Error::PermitResourceByType(ref t)) if t == "Iam::Team"),
        "{by_type:?}"
    );

    let control = permit(r#"resource is Iam::Team in Iam::Org::"grn:root/iam/org/acme""#);
    assert!(control.is_ok(), "{control:?}");
}

#[tokio::test]
async fn a_permit_a_manifest_does_not_declare_is_refused() {
    let world = World::new();
    let right = world.owner_right().await;
    let permit = |action: &str, entity_type: &str| {
        let source = format!(
            r#"permit (principal, action == Iam::Action::"{action}", resource in {entity_type}::"{ACME}");"#
        );
        Body::permit(&world.engine, &right, &source)
    };

    for refused in [
        permit("no-such", "Iam::Org"),
        permit("org-read", "Iam::Nope"),
    ] {
        assert!(
            matches!(refused, Err(Error::Refused(decision::Error::Validation(_)))),
            "{refused:?}"
        );
    }

    let control = permit("org-read", "Iam::Org");
    assert!(control.is_ok(), "{control:?}");
}

#[tokio::test]
async fn text_that_is_not_one_permit_is_refused() {
    let world = World::new();
    let right = world.owner_right().await;
    let permit = |source: &str| Body::permit(&world.engine, &right, source);
    let scoped = r#"resource in Iam::Org::"grn:root/iam/org/acme""#;

    let forbids = permit(&format!("forbid (principal, action, {scoped});"));
    assert!(matches!(forbids, Err(Error::PermitForbids)), "{forbids:?}");
    let syntax = permit("permit (principal");
    assert!(matches!(syntax, Err(Error::PermitSyntax(_))), "{syntax:?}");
    let two =
        format!("permit (principal, action, {scoped}); permit (principal, action, {scoped});");
    let syntax = permit(&two);
    assert!(matches!(syntax, Err(Error::PermitSyntax(_))), "{syntax:?}");

    let control = permit(&format!("permit (principal, action, {scoped});"));
    assert!(control.is_ok(), "{control:?}");
}

#[tokio::test]
async fn a_permit_past_the_guardrail_bounds_is_refused_before_it_is_parsed() {
    let world = World::new();
    let right = world.owner_right().await;
    let permit = |condition: String| {
        let source = format!(
            r#"permit (principal, action, resource in Iam::Org::"{ACME}") when {{ {condition} }};"#
        );
        Body::permit(&world.engine, &right, &source)
    };

    let nested = format!("{}true{}", "(".repeat(200), ")".repeat(200));
    assert!(matches!(permit(nested), Err(Error::PermitTooDeep)));
    let long = vec!["true"; 700].join(" && ");
    assert!(matches!(permit(long), Err(Error::PermitTooDeep)));

    let control = permit(format!("{}true{}", "(".repeat(8), ")".repeat(8)));
    assert!(control.is_ok(), "{control:?}");
    let control = permit(vec!["true"; 400].join(" && "));
    assert!(control.is_ok(), "{control:?}");
}

/// Prints the decision and reasons of a request a permit allows, and
/// the error of a write whose resource is in another organization:
/// `cargo test -p grant --test permit
/// the_decision_and_the_refusal_a_report_prints -- --nocapture`.
#[tokio::test]
async fn the_decision_and_the_refusal_a_report_prints() {
    let world = World::new();
    let right = world.owner_right().await;

    Body::permit(&world.engine, &right, WATCH_BOT).unwrap();
    let id = PolicyId::new("grn:acme/iam/policy/watch-bot");
    let permit = Policy::parse(Some(id), WATCH_BOT).unwrap();
    let held = OrgPolicies {
        permits: vec![permit],
        ..world.held.clone()
    };
    let permitted = World { held, ..world };
    let ask = |who: Named| Evaluation {
        subject: EntityRef {
            entity_type: who.0,
            id: who.1,
        },
        action: "iam:agent-watch",
        resource: EntityRef {
            entity_type: BOT.0,
            id: BOT.1,
        },
        context: json!({}),
    };

    let allowed = permitted
        .engine
        .evaluate(&permitted, &permitted, &ask(MEMBER))
        .await
        .unwrap();
    println!("permit allows pat to watch the bot: {allowed:?}");
    assert_eq!(allowed.outcome, Outcome::Allow);

    let outside = permitted
        .engine
        .evaluate(&permitted, &permitted, &ask(STRANGER))
        .await
        .unwrap();
    println!("a principal of globex: {outside:?}");
    assert_eq!(outside.outcome, Outcome::Deny);

    let foreign = format!(
        r#"permit (principal, action == Iam::Action::"org-read", resource in Iam::Org::"{GLOBEX}");"#
    );
    let refused = Body::permit(&permitted.engine, &right, &foreign).unwrap_err();
    println!("a write in another organization: {refused}");
    assert!(matches!(refused, Error::ForeignEntity(_)));
}

const ADMIN_MAY_PERMIT: &str = r#"permit (
    principal == Iam::User::"grn:acme/iam/user/kim",
    action == Iam::Action::"policy-permit",
    resource == Iam::Org::"grn:root/iam/org/acme"
);"#;
const NO_ONE_PERMITS: &str = r#"forbid (
    principal,
    action == Iam::Action::"policy-permit",
    resource == Iam::Org::"grn:root/iam/org/acme"
);"#;

#[tokio::test]
async fn no_permit_confers_the_right_and_a_guardrail_still_takes_it_away() {
    let mut world = World::new();
    let id = PolicyId::new("grn:acme/iam/policy/kim-permits");
    world.held.permits = vec![Policy::parse(Some(id), ADMIN_MAY_PERMIT).unwrap()];

    // The decision honours the permit, so it is `ask` that leaves it out.
    let evaluation = Evaluation {
        subject: EntityRef {
            entity_type: ADMIN.0,
            id: ADMIN.1,
        },
        action: "iam:policy-permit",
        resource: EntityRef {
            entity_type: ORG.0,
            id: ORG.1,
        },
        context: json!({}),
    };
    let decision = world
        .engine
        .evaluate(&world, &world, &evaluation)
        .await
        .unwrap();
    assert_eq!(decision.outcome, Outcome::Allow, "{decision:?}");

    let refused = world.right(ADMIN).await;
    assert!(
        matches!(refused, Err(Error::PermitDenied { .. })),
        "{refused:?}"
    );
    assert!(world.right(OWNER).await.is_ok());

    let id = PolicyId::new("grn:acme/iam/policy/no-permits");
    world.held.guardrails = vec![Policy::parse(Some(id), NO_ONE_PERMITS).unwrap()];
    let taken = world.right(OWNER).await;
    assert!(
        matches!(taken, Err(Error::PermitDenied { .. })),
        "{taken:?}"
    );
}
