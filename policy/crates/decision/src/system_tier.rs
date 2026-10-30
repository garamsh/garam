use cedar_policy::{Policy, PolicyId, PolicySet, Schema, Template, ValidationMode, Validator};

use crate::error::Error;

pub(crate) const SCHEMA: &str = include_str!("../../../schema/garam.cedarschema");

/// Each file holds one policy, and its stem is that policy's id, which
/// is what a decision's reasons name.
const POLICIES: [(&str, &str); 2] = [
    (
        "tenant-isolation",
        include_str!("../../../system-policies/tenant-isolation.cedar"),
    ),
    (
        "agent-message-send",
        include_str!("../../../system-policies/agent-message-send.cedar"),
    ),
];

const TEMPLATES: [(&str, &str); 6] = [
    (
        "role-org-owner",
        include_str!("../../../system-policies/role-org-owner.cedar"),
    ),
    (
        "role-org-admin",
        include_str!("../../../system-policies/role-org-admin.cedar"),
    ),
    (
        "role-org-member",
        include_str!("../../../system-policies/role-org-member.cedar"),
    ),
    (
        "role-scoped-owner",
        include_str!("../../../system-policies/role-scoped-owner.cedar"),
    ),
    (
        "role-scoped-admin",
        include_str!("../../../system-policies/role-scoped-admin.cedar"),
    ),
    (
        "role-scoped-member",
        include_str!("../../../system-policies/role-scoped-member.cedar"),
    ),
];

/// Parses the system tier and validates it strictly against the schema.
pub(crate) fn load(schema: &Schema) -> Result<PolicySet, Error> {
    let mut tier = PolicySet::new();
    for (id, source) in POLICIES {
        let policy =
            Policy::parse(Some(PolicyId::new(id)), source).map_err(|error| refused(id, &error))?;
        tier.add(policy).map_err(|error| refused(id, &error))?;
    }
    for (id, source) in TEMPLATES {
        let template = Template::parse(Some(PolicyId::new(id)), source)
            .map_err(|error| refused(id, &error))?;
        tier.add_template(template)
            .map_err(|error| refused(id, &error))?;
    }
    let validation = Validator::new(schema.clone()).validate(&tier, ValidationMode::Strict);
    if validation.validation_passed() {
        return Ok(tier);
    }
    let errors: Vec<String> = validation
        .validation_errors()
        .map(ToString::to_string)
        .collect();
    Err(Error::SystemTier(errors.join("; ")))
}

fn refused(id: &str, error: &dyn std::error::Error) -> Error {
    Error::SystemTier(format!("{id}: {error}"))
}

#[cfg(test)]
mod tests {
    use std::collections::BTreeSet;
    use std::fs;

    use super::{POLICIES, TEMPLATES};

    #[test]
    fn every_file_in_system_policies_is_loaded() {
        let folder = concat!(env!("CARGO_MANIFEST_DIR"), "/../../system-policies");
        let on_disk: BTreeSet<String> = fs::read_dir(folder)
            .unwrap()
            .map(|entry| entry.unwrap().path())
            .filter(|path| path.extension().is_some_and(|ext| ext == "cedar"))
            .map(|path| path.file_stem().unwrap().to_string_lossy().into_owned())
            .collect();
        let loaded: BTreeSet<String> = POLICIES
            .iter()
            .chain(TEMPLATES.iter())
            .map(|(id, _)| (*id).to_string())
            .collect();

        assert!(!on_disk.is_empty());
        assert_eq!(on_disk, loaded);
    }
}
