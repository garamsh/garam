/// Who writes an entity type, as its `@caller` says (ADR-0143 item 1).
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Writer {
    /// `@caller("none")`: no caller writes it.
    Nobody,
    /// The `sub` of the caller that writes it.
    Caller(String),
    /// No `@caller` at all, which a check refuses.
    Missing,
}

/// One entity type of a manifest.
#[derive(Debug, Clone)]
pub struct EntityType {
    /// The qualified name, `Iam::Team`.
    pub name: String,
    /// The GRN `type` segment, none for a type no GRN names.
    pub grn: Option<String>,
    /// Who writes it.
    pub writer: Writer,
    /// The qualified names of the types it sits in.
    pub parents: Vec<String>,
}

/// The types an action applies to, qualified.
#[derive(Debug, Clone, Default)]
pub struct AppliesTo {
    /// The principal types.
    pub principals: Vec<String>,
    /// The resource types.
    pub resources: Vec<String>,
}

/// One action of a manifest.
#[derive(Debug, Clone)]
pub struct Action {
    /// The wire name, `<service>:<id>`.
    pub name: String,
    /// The identifier inside the namespace, `<type>.<verb>`.
    pub id: String,
    /// The groups it sits in, by wire name.
    pub parents: Vec<String>,
    /// What it applies to, none for a group.
    pub applies_to: Option<AppliesTo>,
}

/// One manifest: a namespace and the service it declares.
#[derive(Debug, Clone)]
pub struct Service {
    /// The `@service` annotation, the service's one name.
    pub name: String,
    /// The Cedar namespace.
    pub namespace: String,
    /// What the namespace declares as entity types.
    pub entity_types: Vec<EntityType>,
    /// What the namespace declares as actions.
    pub actions: Vec<Action>,
}
