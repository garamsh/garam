//! What an organization owns is read off a GRN's first segment after
//! the prefix (ADR-0142), and a name that only begins like it is not it.

use decision::OrgGrn;

fn acme() -> OrgGrn {
    OrgGrn::new("grn:root/iam/org/acme")
}

#[test]
fn an_organization_owns_what_names_it_first() {
    let org = acme();
    assert_eq!(org.slug(), "acme");
    assert!(org.owns_grn("grn:acme/iam/user/kim"));
    assert!(org.owns_grn("grn:acme/cowork/channel/general"));
    for other in [
        "grn:acme2/iam/user/kim",
        "grn:ac/iam/user/kim",
        "grn:globex/iam/user/kim",
        "grn:acme",
        "acme/iam/user/kim",
        concat!("grn:acme", ":default", ":user", ":kim"),
    ] {
        assert!(!org.owns_grn(other), "{other}");
    }
}

#[test]
fn an_organization_is_named_by_its_slug() {
    assert_eq!(OrgGrn::named("acme"), acme());
    assert_eq!(OrgGrn::named("acme").slug(), "acme");
}

#[test]
fn an_organization_with_no_name_owns_nothing() {
    for nameless in ["", "grn:acme/iam/org/acme", "grn:root/iam/org/"] {
        assert!(
            !OrgGrn::new(nameless).owns_grn("grn:/iam/user/kim"),
            "{nameless}"
        );
        assert!(
            !OrgGrn::new(nameless).owns_grn("grn:acme/iam/user/kim"),
            "{nameless}"
        );
    }
}

#[test]
fn a_policy_is_one_id_under_the_organizations_iam_policies() {
    let org = acme();
    assert!(org.owns_policy("grn:acme/iam/policy/lee-member"));
    for refused in [
        "grn:acme/iam/policy/",
        "grn:acme/iam/policy/a/b",
        "grn:acme/iam/policy/a#b",
        "grn:acme/iam/group/eng",
        "grn:acme/cowork/policy/x",
        "grn:globex/iam/policy/x",
        "role-org-owner",
    ] {
        assert!(!org.owns_policy(refused), "{refused}");
    }
}
