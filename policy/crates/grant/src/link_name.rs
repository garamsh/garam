//! The identifiers the platform writes its role links under, which
//! are reserved to it: a member's (ADR-0161) and the scoped link of a
//! team or a project (ADR-0163).

use decision::{OrgGrn, Role, RoleScope};
use sha2::{Digest, Sha256};

use crate::body::Content;

/// What a reserved identifier opens with: one prefix per role of a
/// member's link, and one for the scoped link of a team or a project.
/// The tail is not read here: [`names_link`] admits the one tail each
/// link has, so a near miss is held by nobody.
const PREFIXES: [&str; 4] = ["link-owner-", "link-admin-", "link-member-", "link-scoped-"];

/// How many bytes of the member's hash the identifier keeps.
const HASH_BYTES: usize = 16;

/// The first [`HASH_BYTES`] bytes of the SHA-256 of `input`, in hex.
fn hex_of(input: &[u8]) -> String {
    Sha256::digest(input)
        .iter()
        .take(HASH_BYTES)
        .map(|byte| format!("{byte:02x}"))
        .collect()
}

fn word_of(role: Role) -> &'static str {
    match role {
        Role::Owner => "owner",
        Role::Admin => "admin",
        Role::Member => "member",
    }
}

/// The policy that holds `role` for `member` in `org`:
/// `grn:<org>/iam/policy/link-<role>-<hex>`, with `<hex>` the first 16
/// bytes of the SHA-256 of the member's GRN. The platform names a link
/// this way, so a link written again is found again.
pub fn link_policy(org: &OrgGrn, role: Role, member: &str) -> String {
    format!(
        "grn:{}/iam/policy/link-{}-{}",
        org.slug(),
        word_of(role),
        hex_of(member.as_bytes())
    )
}

/// The policy that holds `role` for `principal` on `resource`, the
/// scoped link of a team or a project in `org`:
/// `grn:<org>/iam/policy/link-scoped-<role>-<hex>`, with `<hex>` the
/// first 16 bytes of the SHA-256 of five fields in this order: the
/// role's word (`owner`, `admin` or `member`), the principal's type
/// and id, the resource's type and id. Each field is its length in
/// bytes as 8 bytes big-endian, then its bytes, so no two different
/// inputs make one byte string.
pub fn scoped_link_policy(
    org: &OrgGrn,
    role: Role,
    principal: (&str, &str),
    resource: (&str, &str),
) -> String {
    let word = word_of(role);
    let mut input = Vec::new();
    for field in [word, principal.0, principal.1, resource.0, resource.1] {
        input.extend_from_slice(&(field.len() as u64).to_be_bytes());
        input.extend_from_slice(field.as_bytes());
    }
    format!(
        "grn:{}/iam/policy/link-scoped-{word}-{}",
        org.slug(),
        hex_of(&input)
    )
}

/// Whether `grn` is an identifier of `org` that only the platform's
/// write of a link it derives may hold.
pub fn reserved(org: &OrgGrn, grn: &str) -> bool {
    org.owns_policy(grn)
        && grn
            .rsplit('/')
            .next()
            .is_some_and(|id| PREFIXES.iter().any(|prefix| id.starts_with(prefix)))
}

/// Whether `content` is the link `grn` names. A member's link is the
/// role of the identifier, held in the organization's scope by the
/// user whose hash it carries, on the organization. A scoped link is
/// the role of the identifier, held by a team or a project on itself,
/// as the platform's group and project roles are, under the
/// identifier [`scoped_link_policy`] computes from them.
pub fn names_link(org: &OrgGrn, grn: &str, content: &Content) -> bool {
    let Content::RoleLink {
        role,
        scope,
        principal: (principal_type, principal),
        resource: (resource_type, resource),
    } = content
    else {
        return false;
    };
    match scope {
        RoleScope::Organization => {
            principal_type == "Iam::User"
                && resource_type == "Iam::Org"
                && resource == org.as_str()
                && grn == link_policy(org, *role, principal)
        }
        RoleScope::TeamOrProject => {
            matches!(principal_type.as_str(), "Iam::Team" | "Iam::Project")
                && principal_type == resource_type
                && principal == resource
                && grn
                    == scoped_link_policy(
                        org,
                        *role,
                        (principal_type, principal),
                        (resource_type, resource),
                    )
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const ACME: &str = "grn:root/iam/org/acme";
    const LEE: &str = "grn:acme/iam/user/lee";

    fn link(role: Role, scope: RoleScope, principal: (&str, &str), resource: &str) -> Content {
        Content::RoleLink {
            role,
            scope,
            principal: (principal.0.to_owned(), principal.1.to_owned()),
            resource: ("Iam::Org".to_owned(), resource.to_owned()),
        }
    }

    /// The hex is `printf 'grn:acme/iam/user/lee' | sha256sum | cut -c1-32`,
    /// the platform's `linkPolicy` over the same member.
    #[test]
    fn an_identifier_is_the_role_and_the_first_sixteen_bytes_of_the_members_hash() {
        let org = OrgGrn::new(ACME);
        assert_eq!(
            link_policy(&org, Role::Admin, LEE),
            "grn:acme/iam/policy/link-admin-fadf4f482ade12a917dd444d084659a0"
        );
        assert_ne!(
            link_policy(&org, Role::Admin, LEE),
            link_policy(&org, Role::Member, LEE)
        );
    }

    #[test]
    fn only_the_three_role_prefixes_of_an_organizations_policies_are_reserved() {
        let org = OrgGrn::new(ACME);
        for id in ["link-owner-x", "link-admin-", "link-member-a-b"] {
            assert!(reserved(&org, &format!("grn:acme/iam/policy/{id}")), "{id}");
        }
        for id in [
            "link-x",
            "link-owne",
            "linkadmin-x",
            "a-link-admin-x",
            "Link-admin-x",
        ] {
            assert!(
                !reserved(&org, &format!("grn:acme/iam/policy/{id}")),
                "{id}"
            );
        }
        assert!(!reserved(&org, "grn:globex/iam/policy/link-admin-x"));
        assert!(!reserved(&org, "grn:acme/iam/policy/link-admin-x/y"));
    }

    #[test]
    fn a_body_names_its_identifier_only_when_every_part_agrees() {
        let org = OrgGrn::new(ACME);
        let grn = link_policy(&org, Role::Member, LEE);
        let user = ("Iam::User", LEE);
        let own = link(Role::Member, RoleScope::Organization, user, ACME);
        assert!(names_link(&org, &grn, &own));

        let others = [
            link(Role::Admin, RoleScope::Organization, user, ACME),
            link(Role::Member, RoleScope::TeamOrProject, user, ACME),
            link(
                Role::Member,
                RoleScope::Organization,
                ("Iam::User", "grn:acme/iam/user/kim"),
                ACME,
            ),
            link(
                Role::Member,
                RoleScope::Organization,
                ("Iam::Agent", LEE),
                ACME,
            ),
            link(
                Role::Member,
                RoleScope::Organization,
                user,
                "grn:root/iam/org/globex",
            ),
            Content::Guardrail("forbid (principal, action, resource);".to_owned()),
            Content::Permit("permit (principal, action, resource);".to_owned()),
        ];
        for other in &others {
            assert!(!names_link(&org, &grn, other), "{other:?}");
        }
        let near = "grn:acme/iam/policy/link-member-fadf4f48";
        assert!(!names_link(&org, near, &own));
    }

    const ENG: (&str, &str) = ("Iam::Team", "grn:acme/iam/group/eng");
    const WEB: (&str, &str) = ("Iam::Project", "grn:acme/iam/project/web");

    fn scoped(role: Role, principal: (&str, &str), resource: (&str, &str)) -> Content {
        Content::RoleLink {
            role,
            scope: RoleScope::TeamOrProject,
            principal: (principal.0.to_owned(), principal.1.to_owned()),
            resource: (resource.0.to_owned(), resource.1.to_owned()),
        }
    }

    /// The hex is the SHA-256 of the five length-prefixed fields of
    /// ADR-0163, which the platform recomputes in Go.
    #[test]
    fn a_scoped_identifier_is_fixed_to_the_hash_of_its_five_length_prefixed_fields() {
        let org = OrgGrn::new(ACME);
        assert_eq!(
            scoped_link_policy(&org, Role::Admin, ENG, ENG),
            "grn:acme/iam/policy/link-scoped-admin-057a36867e8b5d54bdfba284ee2f3a02"
        );
        assert_eq!(
            scoped_link_policy(&org, Role::Member, WEB, WEB),
            "grn:acme/iam/policy/link-scoped-member-0e9bfafcc0092e41678c97122d4c76be"
        );
    }

    #[test]
    fn two_different_roles_principals_or_resources_never_share_a_scoped_identifier() {
        let org = OrgGrn::new(ACME);
        let id = |role, principal, resource| scoped_link_policy(&org, role, principal, resource);
        let base = id(Role::Admin, ENG, ENG);
        assert_eq!(
            base,
            id(Role::Admin, ENG, ENG),
            "written again, found again"
        );
        let team = |name| ("Iam::Team", name);
        let others = [
            id(Role::Owner, ENG, ENG),
            id(Role::Admin, WEB, ENG),
            id(Role::Admin, ENG, WEB),
            id(Role::Admin, ENG, team("grn:acme/iam/group/en")),
            // The same bytes cut at another field boundary.
            id(Role::Admin, team("ab"), team("c")),
            id(Role::Admin, team("a"), team("bc")),
            id(Role::Admin, ("Iam::Teama", "b"), ("Iam::Team", "c")),
        ];
        for (at, other) in others.iter().enumerate() {
            assert_ne!(&base, other, "{at}");
            for later in &others[at + 1..] {
                assert_ne!(other, later, "{at}");
            }
        }
    }

    #[test]
    fn the_scoped_prefix_is_reserved_beside_the_three_of_a_member() {
        let org = OrgGrn::new(ACME);
        for id in ["link-scoped-", "link-scoped-admin-x"] {
            assert!(reserved(&org, &format!("grn:acme/iam/policy/{id}")), "{id}");
        }
        for id in ["link-scope-x", "link-scopedx", "scoped-admin-x"] {
            assert!(
                !reserved(&org, &format!("grn:acme/iam/policy/{id}")),
                "{id}"
            );
        }
    }

    #[test]
    fn a_body_names_a_scoped_identifier_only_when_the_team_or_project_holds_its_own_role() {
        let org = OrgGrn::new(ACME);
        let grn = scoped_link_policy(&org, Role::Admin, ENG, ENG);
        assert!(names_link(&org, &grn, &scoped(Role::Admin, ENG, ENG)));
        let project = scoped_link_policy(&org, Role::Member, WEB, WEB);
        assert!(names_link(&org, &project, &scoped(Role::Member, WEB, WEB)));

        let own = scoped(Role::Admin, ENG, ENG);
        // Under another identifier than the one the fields compute.
        for other in [
            scoped(Role::Member, ENG, ENG),
            scoped(Role::Admin, WEB, WEB),
            Content::Guardrail("forbid (principal, action, resource);".to_owned()),
        ] {
            assert!(!names_link(&org, &grn, &other), "{other:?}");
        }
        // Under the identifier its own fields compute, so that only
        // what the form admits refuses it: a team or a project holds
        // the role on itself and nothing else does.
        let user = ("Iam::User", LEE);
        let team_of_project = ("Iam::Team", WEB.1);
        for (principal, resource) in [
            (ENG, ("Iam::Team", "grn:acme/iam/group/ops")),
            (ENG, WEB),
            (team_of_project, WEB),
            (user, ENG),
            (("Iam::Agent", LEE), ENG),
            (user, user),
            (("Iam::Org", ACME), ("Iam::Org", ACME)),
        ] {
            let held = scoped(Role::Admin, principal, resource);
            let id = scoped_link_policy(&org, Role::Admin, principal, resource);
            assert!(!names_link(&org, &id, &held), "{held:?}");
        }
        let in_org_scope = Content::RoleLink {
            role: Role::Admin,
            scope: RoleScope::Organization,
            principal: (ENG.0.to_owned(), ENG.1.to_owned()),
            resource: (ENG.0.to_owned(), ENG.1.to_owned()),
        };
        assert!(!names_link(&org, &grn, &in_org_scope));
        // The same body under the identifier of a member's link.
        let members = link_policy(&org, Role::Admin, ENG.1);
        assert!(!names_link(&org, &members, &own));
    }
}
