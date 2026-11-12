use std::collections::{BTreeSet, HashMap};
use std::str::FromStr;

use cedar_policy::{EntityId, EntityTypeName, EntityUid, Schema, SchemaFragment};
use serde_json::Value;

use crate::error::Error;
use crate::service::{Action, AppliesTo, EntityType, Service, Writer};

/// The manifests the service embeds, one per service (ADR-0143 item 1).
const EMBEDDED: [&str; 2] = [
    include_str!("../../../schema/iam.cedarschema"),
    include_str!("../../../schema/cowork.cedarschema"),
];

/// The manifests the service embeds, as source. A manifest in
/// `policy/schema/` that is not among them is loaded by nothing.
pub fn embedded() -> &'static [&'static str] {
    &EMBEDDED
}

/// The most a segment of a GRN holds (ADR-0142 item 4).
const SEGMENT_MAX: usize = 64;

/// Every manifest, and the one schema they merge into.
#[derive(Debug, Clone)]
pub struct Manifests {
    schema: Schema,
    services: Vec<Service>,
}

impl Manifests {
    /// Loads the embedded manifests.
    pub fn load() -> Result<Self, Error> {
        Self::from_sources(&EMBEDDED)
    }

    /// Reads manifests from their Cedar source. A merged [`Schema`]
    /// drops the annotations, so each file is read as a fragment first.
    pub fn from_sources(sources: &[&str]) -> Result<Self, Error> {
        let mut fragments = Vec::new();
        let mut services = Vec::new();
        for source in sources {
            let (fragment, _warnings) = SchemaFragment::from_cedarschema_str(source)
                .map_err(|error| Error::Schema(error.into()))?;
            let json = fragment
                .clone()
                .to_json_value()
                .map_err(|error| Error::Schema(error.into()))?;
            services.extend(read_namespaces(&json)?);
            fragments.push(fragment);
        }
        let schema = Schema::from_schema_fragments(fragments)
            .map_err(|error| Error::Schema(error.into()))?;
        let services = resolve_groups(services);
        Ok(Self { schema, services })
    }

    /// The manifests merged into the schema a decision is validated
    /// against.
    pub fn schema(&self) -> &Schema {
        &self.schema
    }

    /// The services, in the order their manifests were read.
    pub fn services(&self) -> &[Service] {
        &self.services
    }

    /// Whether `caller` writes entities of the qualified type: its
    /// `@caller` names that `sub`. A type no manifest holds is owned by
    /// no one.
    pub fn owns(&self, caller: &str, entity_type: &str) -> bool {
        self.services
            .iter()
            .flat_map(|service| &service.entity_types)
            .any(|found| {
                found.name == entity_type && found.writer == Writer::Caller(caller.to_owned())
            })
    }

    /// The Cedar action an action name on the wire is: `iam:org.read`
    /// is `Iam::Action::"org.read"`. A name with no service, or one no
    /// manifest declares, is refused (ADR-0143 item 3).
    pub fn action(&self, wire: &str) -> Result<EntityUid, Error> {
        let refused = || Error::Action(wire.to_owned());
        let (service, _) = wire.split_once(':').ok_or_else(refused)?;
        let service = self
            .services
            .iter()
            .find(|found| found.name == service)
            .ok_or_else(refused)?;
        let action = service
            .actions
            .iter()
            .find(|found| found.name == wire)
            .ok_or_else(refused)?;
        let action_type = format!("{}::Action", service.namespace);
        Ok(EntityUid::from_type_name_and_id(
            EntityTypeName::from_str(&action_type).map_err(|_| refused())?,
            EntityId::new(&action.id),
        ))
    }

    /// What breaks the rules every manifest holds (ADR-0143 item 2):
    /// one namespace and one `@service` each, no service named twice,
    /// a `@grn` unique within its service and a slug, a `@caller` on
    /// every type, an action identifier of the form `<type>.<verb>`,
    /// `<type>.*` or `role-<name>`, and a `<type>.*` group holding
    /// exactly that type's actions (ADR-0145 items 1 to 3). Each entry
    /// names what disagrees.
    pub fn problems(&self) -> Vec<String> {
        let mut problems = Vec::new();
        let mut names = BTreeSet::new();
        for service in &self.services {
            if !is_slug(&service.name) {
                problems.push(format!(
                    "{}: @service {:?} is not a slug",
                    service.namespace, service.name
                ));
            }
            if !names.insert(service.name.as_str()) {
                problems.push(format!(
                    "{}: service {:?} is declared twice",
                    service.namespace, service.name
                ));
            }
            let mut grns = BTreeSet::new();
            for entity in &service.entity_types {
                if let Some(grn) = &entity.grn {
                    if !is_slug(grn) {
                        problems.push(format!("{}: @grn {grn:?} is not a slug", entity.name));
                    }
                    if !grns.insert(grn.as_str()) {
                        problems.push(format!(
                            "{}: @grn {grn:?} is used twice in {}",
                            entity.name, service.name
                        ));
                    }
                }
                if entity.writer == Writer::Missing {
                    problems.push(format!("{}: no @caller", entity.name));
                }
            }
            for action in &service.actions {
                if !is_action_id(&action.id) {
                    problems.push(format!(
                        "{}: action {:?} is not <type>.<verb>, <type>.* or role-<name>",
                        service.name, action.id
                    ));
                }
            }
            problems.extend(self.group_problems(service));
        }
        problems
    }

    /// Each `<type>.*` group of the service against the actions that
    /// name it as a parent: they must be the service's actions of that
    /// type, no more and no fewer (ADR-0145 item 2).
    fn group_problems(&self, service: &Service) -> Vec<String> {
        let mut problems = Vec::new();
        for group in &service.actions {
            let Some(kind) = group.id.strip_suffix(".*") else {
                continue;
            };
            let expected: BTreeSet<&str> = service
                .actions
                .iter()
                .filter(|action| {
                    action_type(&action.id) == Some(kind) && !action.id.ends_with(".*")
                })
                .map(|action| action.name.as_str())
                .collect();
            let held: BTreeSet<&str> = self
                .services
                .iter()
                .flat_map(|other| &other.actions)
                .filter(|action| action.parents.contains(&group.name))
                .map(|action| action.name.as_str())
                .collect();
            for missing in expected.difference(&held) {
                problems.push(format!("{}: {missing} is not in its group", group.name));
            }
            for extra in held.difference(&expected) {
                problems.push(format!(
                    "{}: holds {extra}, which is not of type {kind}",
                    group.name
                ));
            }
        }
        problems
    }
}

/// Reads each namespace of a fragment's JSON form. A manifest holds
/// exactly one, which [`Manifests::problems`] cannot see once they are
/// flattened, so a second is refused here.
fn read_namespaces(json: &Value) -> Result<Vec<Service>, Error> {
    let namespaces = json
        .as_object()
        .ok_or_else(|| Error::Shape("the fragment".to_owned()))?;
    if namespaces.len() != 1 {
        return Err(Error::Shape(format!(
            "a manifest declares one namespace, not {}",
            namespaces.len()
        )));
    }
    namespaces
        .iter()
        .map(|(namespace, body)| read_service(namespace, body))
        .collect()
}

fn read_service(namespace: &str, body: &Value) -> Result<Service, Error> {
    let shape = |what: &str| Error::Shape(format!("{namespace}: {what}"));
    let name = annotation(body, "service")
        .ok_or_else(|| shape("no @service on the namespace"))?
        .to_owned();
    let entity_types = body
        .get("entityTypes")
        .and_then(Value::as_object)
        .ok_or_else(|| shape("no entityTypes"))?
        .iter()
        .map(|(type_name, entity)| EntityType {
            name: format!("{namespace}::{type_name}"),
            grn: annotation(entity, "grn").map(str::to_owned),
            writer: match annotation(entity, "caller") {
                None => Writer::Missing,
                Some("none") => Writer::Nobody,
                Some(sub) => Writer::Caller(sub.to_owned()),
            },
            parents: strings(entity.get("memberOfTypes"))
                .into_iter()
                .map(|parent| qualify(namespace, &parent))
                .collect(),
        })
        .collect();
    let actions = body
        .get("actions")
        .and_then(Value::as_object)
        .ok_or_else(|| shape("no actions"))?
        .iter()
        .map(|(id, action)| Action {
            name: format!("{name}:{id}"),
            id: id.clone(),
            // A group is held as `(namespace, id)` until every
            // manifest is read, then named by its service.
            parents: groups(namespace, action.get("memberOf")),
            // A group declares no `appliesTo`, which Cedar's JSON form
            // holds as two empty lists.
            applies_to: action
                .get("appliesTo")
                .map(|applies| AppliesTo {
                    principals: strings(applies.get("principalTypes"))
                        .into_iter()
                        .map(|found| qualify(namespace, &found))
                        .collect(),
                    resources: strings(applies.get("resourceTypes"))
                        .into_iter()
                        .map(|found| qualify(namespace, &found))
                        .collect(),
                })
                .filter(|applies| !applies.principals.is_empty() || !applies.resources.is_empty()),
        })
        .collect();
    Ok(Service {
        name,
        namespace: namespace.to_owned(),
        entity_types,
        actions,
    })
}

/// Turns each parent group from `<namespace>\0<id>` into its wire name
/// now that every service is known.
fn resolve_groups(mut services: Vec<Service>) -> Vec<Service> {
    let by_namespace: HashMap<String, String> = services
        .iter()
        .map(|service| (service.namespace.clone(), service.name.clone()))
        .collect();
    for service in &mut services {
        for action in &mut service.actions {
            for parent in &mut action.parents {
                if let Some((namespace, id)) = parent.split_once('\0') {
                    let owner = by_namespace
                        .get(namespace)
                        .map_or(namespace, String::as_str);
                    *parent = format!("{owner}:{id}");
                }
            }
        }
    }
    services
}

fn groups(namespace: &str, member_of: Option<&Value>) -> Vec<String> {
    member_of
        .and_then(Value::as_array)
        .into_iter()
        .flatten()
        .filter_map(|group| {
            let id = group.get("id")?.as_str()?;
            let owner = match group.get("type").and_then(Value::as_str) {
                Some(kind) => kind.strip_suffix("::Action").unwrap_or(kind),
                None => namespace,
            };
            Some(format!("{owner}\0{id}"))
        })
        .collect()
}

fn annotation<'a>(node: &'a Value, key: &str) -> Option<&'a str> {
    node.get("annotations")?.get(key)?.as_str()
}

fn strings(node: Option<&Value>) -> Vec<String> {
    node.and_then(Value::as_array)
        .into_iter()
        .flatten()
        .filter_map(|item| item.as_str().map(str::to_owned))
        .collect()
}

/// A name the manifest wrote inside its namespace is that namespace's.
fn qualify(namespace: &str, name: &str) -> String {
    if name.contains("::") {
        name.to_owned()
    } else {
        format!("{namespace}::{name}")
    }
}

/// One to 64 characters of lowercase letters, digits and `-`
/// (ADR-0142 item 4).
fn is_slug(text: &str) -> bool {
    !text.is_empty()
        && text.len() <= SEGMENT_MAX
        && text
            .bytes()
            .all(|byte| byte.is_ascii_lowercase() || byte.is_ascii_digit() || byte == b'-')
}

/// The type of an action id `<type>.<verb>` or `<type>.*`: the slug
/// before its one dot. A role group has none.
fn action_type(id: &str) -> Option<&str> {
    let (kind, verb) = id.split_once('.')?;
    (is_slug(kind) && (verb == "*" || is_slug(verb))).then_some(kind)
}

/// `<type>.<verb>`, the group `<type>.*`, or the role group
/// `role-<name>`, the one id with no dot (ADR-0145 items 1 to 3).
fn is_action_id(id: &str) -> bool {
    match id.strip_prefix("role-") {
        Some(name) => is_slug(name),
        None => action_type(id).is_some(),
    }
}
