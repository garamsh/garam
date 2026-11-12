#![cfg(test)]

use manifest::{Manifests, Writer};

const IAM: &str = include_str!("../../../schema/iam.cedarschema");
const COWORK: &str = include_str!("../../../schema/cowork.cedarschema");

/// The shipped manifests with one line of cowork's changed. `from` must
/// exist, so a test cannot pass on a replacement that matched nothing.
fn cowork_with(from: &str, to: &str) -> Manifests {
    assert!(COWORK.contains(from), "cowork no longer holds {from:?}");
    Manifests::from_sources(&[IAM, &COWORK.replacen(from, to, 1)]).unwrap()
}

/// The shipped manifests with one line of the platform's changed, by
/// the same rule as [`cowork_with`].
fn iam_with(from: &str, to: &str) -> Manifests {
    assert!(IAM.contains(from), "iam no longer holds {from:?}");
    Manifests::from_sources(&[&IAM.replacen(from, to, 1), COWORK]).unwrap()
}

#[test]
fn the_shipped_manifests_break_no_rule() {
    let manifests = Manifests::load().unwrap();

    assert_eq!(manifests.problems(), Vec::<String>::new());
    let names: Vec<_> = manifests.services().iter().map(|s| &s.name).collect();
    assert_eq!(names, ["iam", "cowork"]);
}

#[test]
fn a_grn_segment_used_twice_in_a_service_is_named() {
    let manifests = cowork_with(r#"@grn("page")"#, r#"@grn("task")"#);

    let problems = manifests.problems();
    assert_eq!(problems.len(), 1, "{problems:?}");
    assert!(problems[0].contains("Cowork::Task"), "{problems:?}");
    assert!(problems[0].contains("used twice"), "{problems:?}");
}

#[test]
fn a_grn_segment_that_is_not_a_slug_is_named() {
    let manifests = cowork_with(r#"@grn("page")"#, r#"@grn("Page_1")"#);

    let problems = manifests.problems();
    assert_eq!(problems.len(), 1, "{problems:?}");
    assert!(problems[0].contains("not a slug"), "{problems:?}");
}

#[test]
fn a_type_without_a_caller_is_named() {
    let manifests = cowork_with(r#"@grn("page") @caller("cowork")"#, r#"@grn("page")"#);

    let problems = manifests.problems();
    assert_eq!(problems, ["Cowork::Page: no @caller"]);
}

#[test]
fn an_action_that_is_not_type_verb_is_named() {
    // A bare verb, the hyphen form ADR-0143 wrote, a second dot, and a
    // role group with no name.
    for refused in ["read", "page-read", "page.read.all", "role-"] {
        let manifests = cowork_with(
            r#"action "page.read", "page.write""#,
            &format!(r#"action "{refused}", "page.write""#),
        );

        let problems = manifests.problems();
        assert_eq!(problems.len(), 1, "{refused}: {problems:?}");
        assert!(
            problems[0].contains(&format!("{refused:?}")),
            "{refused}: {problems:?}"
        );
    }
    // Control: the accepted forms, with a role group. The shipped
    // `<type>.*` groups are the control for the group form.
    let manifests = cowork_with(
        r#"action "page.read", "page.write""#,
        r#"action "page.read", "page.write", "role-viewer""#,
    );
    assert_eq!(manifests.problems(), Vec::<String>::new());
}

#[test]
fn a_type_group_missing_one_of_its_types_actions_is_named() {
    let manifests = iam_with(
        r#"action "org.delete" in ["org.*"]"#,
        r#"action "org.delete""#,
    );

    assert_eq!(
        manifests.problems(),
        ["iam:org.*: iam:org.delete is not in its group"]
    );
}

#[test]
fn a_type_group_holding_an_action_of_another_type_is_named() {
    let manifests = iam_with(
        "  action \"agent.watch\"\n",
        "  action \"agent.watch\" in [\"org.*\"]\n",
    );

    assert_eq!(
        manifests.problems(),
        ["iam:org.*: holds iam:agent.watch, which is not of type org"]
    );
}

#[test]
fn a_group_is_no_wider_than_its_type_even_where_a_prefix_would_match() {
    // `agent-template.read` is not of type `agent`, whatever a prefix
    // match on `agent` says, so `agent.*` may not hold it.
    let manifests = iam_with(
        r#"action "agent-template.read", "execution-profile.read""#,
        r#"action "agent.*";
  action "agent-template.read", "execution-profile.read""#,
    );
    assert!(
        manifests
            .problems()
            .iter()
            .all(|p| !p.contains("agent-template.read")),
        "{:?}",
        manifests.problems()
    );
}

#[test]
fn a_service_declared_by_two_manifests_is_named() {
    let again = COWORK.replacen("namespace Cowork", "namespace Other", 1);
    let again = again.replace("Cowork", "Other");
    let manifests = Manifests::from_sources(&[IAM, COWORK, &again]).unwrap();

    let problems = manifests.problems();
    assert!(
        problems.iter().any(|p| p.contains("declared twice")),
        "{problems:?}"
    );
}

#[test]
fn a_manifest_declaring_two_namespaces_is_refused() {
    let two = format!("{COWORK}\n@service(\"extra\")\nnamespace Extra {{}}\n");

    assert!(Manifests::from_sources(&[IAM, &two]).is_err());
}

#[test]
fn a_namespace_without_a_service_is_refused() {
    let anonymous = COWORK.replacen("@service(\"cowork\")\n", "", 1);

    assert!(Manifests::from_sources(&[IAM, &anonymous]).is_err());
}

#[test]
fn an_action_is_its_service_and_its_name_and_nothing_else() {
    let manifests = Manifests::load().unwrap();

    assert_eq!(
        manifests.action("iam:org.read").unwrap().to_string(),
        r#"Iam::Action::"org.read""#
    );
    assert_eq!(
        manifests.action("cowork:page.read").unwrap().to_string(),
        r#"Cowork::Action::"page.read""#
    );
    // Controls: no service, the unqualified form of a declared name, the
    // hyphen and old colon forms, a service no manifest declares, and
    // a name its service lacks.
    for refused in [
        "org.read",
        "org:read",
        "iam:org-read",
        "billing:org.read",
        "iam:no-such",
    ] {
        assert!(manifests.action(refused).is_err(), "{refused} resolved");
    }
}

#[test]
fn a_caller_owns_the_types_whose_annotation_names_it() {
    let manifests = Manifests::load().unwrap();

    assert!(manifests.owns("platform", "Iam::Org"));
    assert!(!manifests.owns("platform", "Iam::Policy"));
    assert!(!manifests.owns("platform", "Org"));
    assert!(!manifests.owns("someone-else", "Iam::Org"));
    let policy = manifests
        .services()
        .iter()
        .flat_map(|s| &s.entity_types)
        .find(|t| t.name == "Iam::Policy")
        .unwrap();
    assert_eq!(policy.writer, Writer::Nobody);
}

#[test]
fn what_an_action_applies_to_is_read_and_a_group_applies_to_nothing() {
    let manifests = Manifests::load().unwrap();
    let iam = &manifests.services()[0];

    let read = iam
        .actions
        .iter()
        .find(|a| a.name == "iam:org.read")
        .unwrap();
    let applies = read.applies_to.as_ref().unwrap();
    assert!(applies.resources.contains(&"Iam::Org".to_owned()));
    assert!(read.parents.contains(&"iam:org.*".to_owned()));
    let group = iam
        .actions
        .iter()
        .find(|a| a.name == "iam:role-owner")
        .unwrap();
    assert!(group.applies_to.is_none());
}

/// The ten operations migration 000072 lets a hosted operator hold
/// (ADR-0146), and no other action in the manifests.
#[test]
fn an_operator_may_hold_the_ten_delegable_operations_and_no_other_action() {
    const TEN: [&str; 10] = [
        "iam:agent.create",
        "iam:agent.configure",
        "iam:agent.activate",
        "iam:agent.recover",
        "iam:agent-template.read",
        "iam:agent-template.publish",
        "iam:execution-profile.read",
        "iam:agent.execution-read",
        "iam:operator.read",
        "iam:agent.replace",
    ];
    let manifests = Manifests::load().unwrap();

    let delegable: Vec<&str> = manifests
        .services()
        .iter()
        .flat_map(|service| &service.actions)
        .map(|action| action.name.as_str())
        .filter(|name| manifests.delegable(name))
        .collect();
    let mut found = delegable.clone();
    found.sort_unstable();
    found.dedup();
    let mut expected = TEN.to_vec();
    expected.sort_unstable();
    assert_eq!(found, expected);

    for not in [
        "iam:agent.read",
        "iam:message.send",
        "iam:role-owner",
        "nope:x.y",
    ] {
        assert!(!manifests.delegable(not), "{not}");
    }
}
