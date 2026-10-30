#![cfg(test)]

mod fixture;

use std::collections::{BTreeMap, BTreeSet};
use std::fs;

use decision::{Outcome, Role, RoleScope};
use fixture::{ACME, API, BOT, ENG, KIM, LEE, WEB, World, link, policy};

const ROLE_GO: &str = concat!(
    env!("CARGO_MANIFEST_DIR"),
    "/../../../platform/internal/org/role.go"
);

/// What platform/internal/org/role.go grants, read as text: the
/// platform's package is internal, so nothing outside its module can
/// import it. Roles leave the platform in #1689's fourth slice, which
/// removes this table and `tooling/ci/modules`' `policy platform` row.
struct RoleTable {
    /// Every `Action…` constant, by identifier.
    actions: BTreeMap<String, String>,
    /// Each role's patterns, by its name in the `Role.Actions` switch.
    roles: BTreeMap<String, Vec<String>>,
}

fn quoted(text: &str) -> Option<&str> {
    text.split('"').nth(1)
}

fn identifiers(text: &str) -> impl Iterator<Item = &str> {
    text.split(|c: char| !c.is_alphanumeric())
        .filter(|word| word.starts_with("Action") && word.len() > "Action".len())
}

fn block<'a>(source: &'a str, opening: &str) -> &'a str {
    let (_, rest) = source
        .split_once(opening)
        .unwrap_or_else(|| panic!("role.go no longer holds {opening}"));
    rest.split_once("\n}")
        .unwrap_or_else(|| panic!("{opening} has no end"))
        .0
}

impl RoleTable {
    fn read() -> Self {
        let source = fs::read_to_string(ROLE_GO).expect("role.go is readable");

        let actions: BTreeMap<String, String> = source
            .lines()
            .filter_map(|line| {
                let (name, value) = line.trim().split_once(" = ")?;
                let name = name.trim();
                let is_constant = name.starts_with("Action") && !name.contains(' ');
                Some((
                    name.to_string(),
                    quoted(value).filter(|_| is_constant)?.to_string(),
                ))
            })
            .collect();

        let mut machine: BTreeMap<bool, Vec<String>> = BTreeMap::new();
        for row in block(&source, "member bool\n}{").lines() {
            let Some(action) = identifiers(row).next() else {
                continue;
            };
            let name = actions[action].clone();
            if row.contains("true") {
                machine.entry(true).or_default().push(name.clone());
            }
            machine.entry(false).or_default().push(name);
        }

        let cowork: Vec<String> = identifiers(block(&source, "var coworkActions = []string{"))
            .map(|action| actions[action].clone())
            .collect();

        let mut roles = BTreeMap::new();
        let switch = block(&source, "func (r Role) Actions() []string {");
        for case in switch.split("case Role").skip(1) {
            let (name, body) = case.split_once(':').expect("a case holds a colon");
            let mut patterns: Vec<String> = identifiers(body)
                .map(|action| actions[action].clone())
                .collect();
            let wildcards = body.split('"').skip(1).step_by(2);
            patterns.extend(wildcards.map(str::to_string));
            if body.contains("machineActionsHeld(false)") {
                patterns.extend(machine[&false].iter().cloned());
            }
            if body.contains("machineActionsHeld(true)") {
                patterns.extend(machine[&true].iter().cloned());
            }
            if body.contains("coworkActions") {
                patterns.extend(cowork.iter().cloned());
            }
            roles.insert(name.to_string(), patterns);
        }

        Self { actions, roles }
    }

    fn patterns(&self, role: &str) -> &[String] {
        &self.roles[role]
    }
}

fn grants(patterns: &[String], action: &str) -> bool {
    patterns
        .iter()
        .any(|pattern| match pattern.strip_suffix('*') {
            Some(prefix) => action.starts_with(prefix),
            None => pattern == action,
        })
}

const ROLES: [(&str, Role); 3] = [
    ("Owner", Role::Owner),
    ("Admin", Role::Admin),
    ("Member", Role::Member),
];

#[test]
fn role_go_is_read_rather_than_parsed_into_nothing() {
    let table = RoleTable::read();

    assert!(table.actions.len() > 50, "{} actions", table.actions.len());
    assert_eq!(table.actions["ActionAgentWatch"], "agent:watch");
    assert_eq!(
        table.roles.keys().map(String::as_str).collect::<Vec<_>>(),
        vec!["Admin", "Member", "Owner"]
    );
    for (name, _) in ROLES {
        assert!(!table.patterns(name).is_empty(), "{name} parsed empty");
    }
    assert!(grants(table.patterns("Owner"), "org:delete"));
    assert!(!grants(table.patterns("Admin"), "org:delete"));
    assert!(grants(table.patterns("Member"), "operator:read"));
    assert!(!grants(table.patterns("Member"), "operator:register"));
}

#[test]
fn the_schema_names_every_action_role_go_defines_and_no_other() {
    let world = World::new();
    let table = RoleTable::read();

    let mut expected: BTreeSet<String> = table.actions.values().cloned().collect();
    expected.insert("message:send".to_string());

    let declared: BTreeSet<String> = world
        .engine
        .schema()
        .actions()
        .map(|action| AsRef::<str>::as_ref(action.id()).to_string())
        .filter(|name| !name.ends_with('*') && !name.starts_with("role-"))
        .collect();

    assert_eq!(declared, expected);
}

#[test]
fn each_organization_role_grants_exactly_what_role_go_grants() {
    let world = World::new();
    let table = RoleTable::read();

    for (name, role) in ROLES {
        let policies = world.compose(&[link(role, RoleScope::Organization, LEE, ACME)], &[]);
        let patterns = table.patterns(name);
        let mut granted = 0;
        for action in table.actions.values() {
            let decision = world.ask(&policies, LEE, action, ACME);
            let expected = if grants(patterns, action) {
                granted += 1;
                Outcome::Allow
            } else {
                Outcome::Deny
            };
            assert_eq!(decision.outcome, expected, "{name} on {action}");
        }
        assert!(granted > 0, "{name} granted nothing");
    }
}

#[test]
fn no_role_reaches_agent_watch_and_a_grant_written_for_it_does() {
    let world = World::new();
    let owner = link(Role::Owner, RoleScope::Organization, LEE, ACME);

    let policies = world.compose(std::slice::from_ref(&owner), &[]);
    let refused = world.ask(&policies, LEE, "agent:watch", BOT);
    assert_eq!(refused.outcome, Outcome::Deny);

    let watch = policy(
        "lee-watches",
        r#"permit (
            principal == User::"grn:acme:default:user:lee",
            action == Action::"agent:watch",
            resource
        );"#,
    );
    let policies = world.compose(&[owner], &[watch]);
    let control = world.ask(&policies, LEE, "agent:watch", BOT);
    assert_eq!(control.outcome, Outcome::Allow);
}

#[test]
fn every_organization_role_reaches_what_the_organization_contains() {
    let world = World::new();

    for (name, role) in ROLES {
        let policies = world.compose(&[link(role, RoleScope::Organization, LEE, ACME)], &[]);
        for (action, what) in [
            ("agent:read", BOT),
            ("group:read", ENG),
            ("project:read", WEB),
        ] {
            let decision = world.ask(&policies, LEE, action, what);
            assert_eq!(decision.outcome, Outcome::Allow, "{name} on {action}");
        }
    }
}

#[test]
fn every_scoped_role_reaches_its_team_and_no_member_agent() {
    let world = World::new();

    for (name, role) in ROLES {
        let policies = world.compose(&[link(role, RoleScope::TeamOrProject, LEE, ENG)], &[]);

        let control = world.ask(&policies, LEE, "group:read", ENG);
        assert_eq!(control.outcome, Outcome::Allow, "{name} on its team");

        let refused = world.ask(&policies, LEE, "agent:read", BOT);
        assert_eq!(refused.outcome, Outcome::Deny, "{name} on a member agent");
    }
}

#[test]
fn every_scoped_role_reaches_its_project_and_no_other() {
    let world = World::new();

    for (name, role) in ROLES {
        let policies = world.compose(&[link(role, RoleScope::TeamOrProject, LEE, WEB)], &[]);

        let control = world.ask(&policies, LEE, "project:read", WEB);
        assert_eq!(control.outcome, Outcome::Allow, "{name} on its project");

        let refused = world.ask(&policies, LEE, "project:read", API);
        assert_eq!(refused.outcome, Outcome::Deny, "{name} on another");
    }
}

#[test]
fn a_team_holds_every_template_for_its_members_and_no_one_else() {
    let world = World::new();

    for (name, role) in ROLES {
        let organization = link(role, RoleScope::Organization, ENG, ACME);
        let scoped = link(role, RoleScope::TeamOrProject, ENG, ENG);
        for (grant, action, what) in [
            (organization, "org:read", ACME),
            (scoped, "group:read", ENG),
        ] {
            let policies = world.compose(&[grant], &[]);

            let member = world.ask(&policies, KIM, action, what);
            assert_eq!(member.outcome, Outcome::Allow, "{name} on {action}");

            let outsider = world.ask(&policies, LEE, action, what);
            assert_eq!(outsider.outcome, Outcome::Deny, "{name} on {action}");
        }
    }
}

#[test]
fn a_direct_link_names_its_subject_alone() {
    let world = World::new();
    let policies = world.compose(
        &[link(Role::Member, RoleScope::Organization, LEE, ACME)],
        &[],
    );

    let named = world.ask(&policies, LEE, "org:read", ACME);
    assert_eq!(named.outcome, Outcome::Allow);

    let other = world.ask(&policies, KIM, "org:read", ACME);
    assert_eq!(other.outcome, Outcome::Deny);
}
