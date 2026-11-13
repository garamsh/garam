//! The identifiers the platform writes a member's role links under,
//! which are reserved to it (ADR-0161).

use decision::{OrgGrn, Role, RoleScope};
use sha2::{Digest, Sha256};

use crate::body::Content;

/// What a reserved identifier opens with, one prefix per role. The
/// tail is not read here: [`names_link`] admits the one tail a member
/// has, so a near miss is held by nobody.
const PREFIXES: [&str; 3] = ["link-owner-", "link-admin-", "link-member-"];

/// How many bytes of the member's hash the identifier keeps.
const HASH_BYTES: usize = 16;

/// The policy that holds `role` for `member` in `org`:
/// `grn:<org>/iam/policy/link-<role>-<hex>`, with `<hex>` the first 16
/// bytes of the SHA-256 of the member's GRN. The platform names a link
/// this way, so a link written again is found again.
pub fn link_policy(org: &OrgGrn, role: Role, member: &str) -> String {
    let hex: String = Sha256::digest(member.as_bytes())
        .iter()
        .take(HASH_BYTES)
        .map(|byte| format!("{byte:02x}"))
        .collect();
    let role = match role {
        Role::Owner => "owner",
        Role::Admin => "admin",
        Role::Member => "member",
    };
    format!("grn:{}/iam/policy/link-{role}-{hex}", org.slug())
}

/// Whether `grn` is an identifier of `org` that only the platform's
/// write of a member's link may hold.
pub fn reserved(org: &OrgGrn, grn: &str) -> bool {
    org.owns_policy(grn)
        && grn
            .rsplit('/')
            .next()
            .is_some_and(|id| PREFIXES.iter().any(|prefix| id.starts_with(prefix)))
}

/// Whether `content` is the link `grn` names: the role of the
/// identifier, held in the organization's scope by the user whose hash
/// it carries, on the organization.
pub fn names_link(org: &OrgGrn, grn: &str, content: &Content) -> bool {
    let Content::RoleLink {
        role,
        scope: RoleScope::Organization,
        principal: (principal_type, member),
        resource: (resource_type, resource),
    } = content
    else {
        return false;
    };
    principal_type == "Iam::User"
        && resource_type == "Iam::Org"
        && resource == org.as_str()
        && grn == link_policy(org, *role, member)
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
}
