use std::str::FromStr;

use cedar_policy::{
    Authorizer, Context, Effect, Entities, EntityId, EntityTypeName, EntityUid, EvalResult, Policy,
    PolicyId, PolicySet, Request, Schema, ValidationMode, Validator,
};
use manifest::Manifests;

use crate::decision::Decision;
use crate::entity_source::{EntitySlice, EntitySource};
use crate::error::Error;
use crate::policy_source::{OrgGrn, OrgPolicies, PolicySource};
use crate::role_link::RoleLink;
use crate::system_tier;

/// The entity type of a hosted operator.
const OPERATOR: &str = "Iam::Operator";

/// The action that sends, the one a send across organizations is
/// asked as (ADR-0147).
const MESSAGE_SEND: &str = "iam:message.send";

/// An entity as a caller names it.
#[derive(Debug, Clone, Copy)]
pub struct EntityRef<'a> {
    /// An entity type of the schema, qualified: `Iam::Org`.
    pub entity_type: &'a str,
    /// The entity's GRN.
    pub id: &'a str,
}

/// One question, as a caller asks it.
#[derive(Debug, Clone)]
pub struct Evaluation<'a> {
    /// Who asks.
    pub subject: EntityRef<'a>,
    /// An action of a manifest, `<service>:<action>`.
    pub action: &'a str,
    /// What the action is asked of.
    pub resource: EntityRef<'a>,
    /// The request's context, a JSON object the schema constrains.
    pub context: serde_json::Value,
}

/// The system tier joined with one organization's policies. Only
/// [`Engine::compose`] makes one, so no decision omits the system tier.
#[derive(Debug, Clone)]
pub struct Policies(PolicySet);

impl Policies {
    /// The composed set, as Cedar's analysis reads it.
    pub fn policy_set(&self) -> &PolicySet {
        &self.0
    }
}

/// Holds the schema and the system tier, and decides.
#[derive(Debug, Clone)]
pub struct Engine {
    manifests: Manifests,
    schema: Schema,
    system_tier: PolicySet,
}

impl Engine {
    /// Loads the embedded manifests, merged into one schema, and the
    /// system tier.
    pub fn load() -> Result<Self, Error> {
        let manifests = Manifests::load().map_err(|error| Error::Schema(error.into()))?;
        let schema = manifests.schema().clone();
        let system_tier = system_tier::load(&schema)?;
        Ok(Self {
            manifests,
            schema,
            system_tier,
        })
    }

    /// The manifests the schema was merged from, which keep what the
    /// merged schema drops: each service's name and its annotations.
    pub fn manifests(&self) -> &Manifests {
        &self.manifests
    }

    /// The vocabulary a decision can name.
    pub fn schema(&self) -> &Schema {
        &self.schema
    }

    /// Parses a question, refusing what the schema does not name.
    pub fn request(&self, evaluation: &Evaluation<'_>) -> Result<Request, Error> {
        let refused = |error: Box<dyn std::error::Error + Send + Sync>| Error::Evaluation(error);
        let principal = entity_uid(evaluation.subject).map_err(refused)?;
        let resource = entity_uid(evaluation.resource).map_err(refused)?;
        let action = self
            .manifests
            .action(evaluation.action)
            .map_err(|error| refused(error.into()))?;
        let context = Context::from_json_value(evaluation.context.clone(), None)
            .map_err(|error| refused(error.into()))?;
        Request::new(principal, action, resource, context, Some(&self.schema))
            .map_err(|error| refused(error.into()))
    }

    /// Parses entities from Cedar's JSON format, refusing a type, an
    /// attribute or a parent the schema does not declare.
    pub fn entities(&self, json: serde_json::Value) -> Result<Entities, Error> {
        Entities::from_json_value(json, Some(&self.schema))
            .map_err(|error| Error::Entities(error.into()))
    }

    /// Joins an organization's role links and its own policies, the
    /// guardrails and permits it wrote, to the system tier.
    pub fn compose(&self, links: &[RoleLink], own: &[Policy]) -> Result<Policies, Error> {
        let mut policies = self.system_tier.clone();
        for link in links {
            link.check_scope()?;
            policies
                .link(link.template_id(), link.id.clone(), link.slots())
                .map_err(|error| Error::Composition {
                    policy: link.id.to_string(),
                    source: error.into(),
                })?;
        }
        for policy in own {
            policies
                .add(policy.clone())
                .map_err(|error| Error::Composition {
                    policy: policy.id().to_string(),
                    source: error.into(),
                })?;
        }
        Ok(Policies(policies))
    }

    /// Composes for one principal. No organization `permit` reaches an
    /// `Iam::Operator`: its reach is its delegation alone (ADR-0146
    /// §A hosted operator's delegation), so its composition holds the
    /// organization's `forbid`s and no `permit`, whatever the text of a
    /// `permit` says.
    pub fn compose_for(
        &self,
        principal: &EntityUid,
        links: &[RoleLink],
        own: &[Policy],
    ) -> Result<Policies, Error> {
        if principal.type_name().to_string() != OPERATOR {
            return self.compose(links, own);
        }
        let forbids: Vec<Policy> = own
            .iter()
            .filter(|policy| policy.effect() == Effect::Forbid)
            .cloned()
            .collect();
        self.compose(links, &forbids)
    }

    /// Refuses what the schema does not admit, with the checks a
    /// composition makes. A write runs this before it stores anything.
    pub fn validate(&self, links: &[RoleLink], own: &[Policy]) -> Result<(), Error> {
        let policies = self.compose(links, own)?;
        let validation =
            Validator::new(self.schema.clone()).validate(&policies.0, ValidationMode::Strict);
        if validation.validation_passed() {
            return Ok(());
        }
        let errors: Vec<String> = validation
            .validation_errors()
            .map(ToString::to_string)
            .collect();
        Err(Error::Validation(errors.join("; ")))
    }

    /// Reads what a question needs from the service's own stores and
    /// decides it. No policy reaches a resource the store does not
    /// hold, and the system tier denies it.
    pub async fn evaluate<P: PolicySource, E: EntitySource>(
        &self,
        policies: &P,
        entities: &E,
        evaluation: &Evaluation<'_>,
    ) -> Result<Decision, Error> {
        let request = self.request(evaluation)?;
        let (Some(principal), Some(resource)) = (request.principal(), request.resource()) else {
            return Err(Error::Evaluation("a concrete entity is required".into()));
        };
        let slice = entities
            .slice(principal, resource)
            .await
            .map_err(Error::EntityRead)?;
        let org_policies = match &slice.org {
            Some(org) => policies.active(org).await.map_err(Error::PolicyRead)?,
            None => OrgPolicies::default(),
        };
        // Named by the organization they came from: two organizations
        // choose ids alone, and a shared id would fail the composition.
        let sender_guardrails: Vec<Policy> = match self.sender_org(&request, &slice) {
            Some(org) => policies
                .active(&org)
                .await
                .map_err(Error::PolicyRead)?
                .guardrails
                .into_iter()
                .map(|guardrail| {
                    let id = PolicyId::new(format!("{org}/{}", guardrail.id()));
                    guardrail.new_id(id)
                })
                .collect(),
            None => Vec::new(),
        };
        let own = [
            org_policies.guardrails.as_slice(),
            org_policies.permits.as_slice(),
            sender_guardrails.as_slice(),
        ]
        .concat();
        let composed = self.compose_for(principal, &org_policies.links, &own)?;
        Ok(self.decide(&request, &composed, &slice.entities))
    }

    /// The sender's organization, where the request is a send across
    /// organizations: its guardrails are read too, its `forbid`s alone
    /// (ADR-0147 item 4). Any other request reads the resource's
    /// organization only.
    fn sender_org(&self, request: &Request, slice: &EntitySlice) -> Option<OrgGrn> {
        let send = self.manifests.action(MESSAGE_SEND).ok()?;
        if request.action() != Some(&send) {
            return None;
        }
        let sender = slice.entities.get(request.principal()?)?;
        let Ok(EvalResult::EntityUid(org)) = sender.attr("org")? else {
            return None;
        };
        let org = OrgGrn::new(org.id().unescaped());
        (slice.org.as_ref()? != &org).then_some(org)
    }

    /// Decides one request, failing closed on any evaluation error.
    pub fn decide(&self, request: &Request, policies: &Policies, entities: &Entities) -> Decision {
        let response = Authorizer::new().is_authorized(request, &policies.0, entities);
        Decision::from_response(&response)
    }
}

/// Refuses a NUL as the caller's mistake: no store can hold one, so it
/// would otherwise surface as the store failing.
fn entity_uid(
    entity: EntityRef<'_>,
) -> Result<EntityUid, Box<dyn std::error::Error + Send + Sync>> {
    if entity.id.contains('\0') {
        return Err("an entity id holds a NUL".into());
    }
    Ok(EntityUid::from_type_name_and_id(
        EntityTypeName::from_str(entity.entity_type)?,
        EntityId::new(entity.id),
    ))
}
