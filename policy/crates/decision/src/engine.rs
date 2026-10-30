use std::str::FromStr;

use cedar_policy::{
    Authorizer, Context, Entities, EntityId, EntityTypeName, EntityUid, Policy, PolicySet, Request,
    Schema,
};

use crate::decision::Decision;
use crate::error::Error;
use crate::role_link::RoleLink;
use crate::system_tier;

/// An entity as a caller names it.
#[derive(Debug, Clone, Copy)]
pub struct EntityRef<'a> {
    /// An entity type of the schema.
    pub entity_type: &'a str,
    /// The entity's GRN.
    pub id: &'a str,
}

/// One question, as a caller asks it.
#[derive(Debug, Clone)]
pub struct Evaluation<'a> {
    /// Who asks.
    pub subject: EntityRef<'a>,
    /// An action of the schema.
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

/// Holds the schema and the system tier, and decides.
#[derive(Debug, Clone)]
pub struct Engine {
    schema: Schema,
    system_tier: PolicySet,
}

impl Engine {
    /// Loads the embedded schema and system tier.
    pub fn load() -> Result<Self, Error> {
        let (schema, _warnings) = Schema::from_cedarschema_str(system_tier::SCHEMA)
            .map_err(|error| Error::Schema(error.into()))?;
        let system_tier = system_tier::load(&schema)?;
        Ok(Self {
            schema,
            system_tier,
        })
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
        let action = EntityUid::from_type_name_and_id(
            EntityTypeName::from_str("Action").map_err(|error| refused(error.into()))?,
            EntityId::new(evaluation.action),
        );
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

    /// Joins an organization's role links and guardrails to the system
    /// tier.
    pub fn compose(&self, links: &[RoleLink], guardrails: &[Policy]) -> Result<Policies, Error> {
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
        for guardrail in guardrails {
            policies
                .add(guardrail.clone())
                .map_err(|error| Error::Composition {
                    policy: guardrail.id().to_string(),
                    source: error.into(),
                })?;
        }
        Ok(Policies(policies))
    }

    /// Decides one request, failing closed on any evaluation error.
    pub fn decide(&self, request: &Request, policies: &Policies, entities: &Entities) -> Decision {
        let response = Authorizer::new().is_authorized(request, &policies.0, entities);
        Decision::from_response(&response)
    }
}

fn entity_uid(
    entity: EntityRef<'_>,
) -> Result<EntityUid, Box<dyn std::error::Error + Send + Sync>> {
    Ok(EntityUid::from_type_name_and_id(
        EntityTypeName::from_str(entity.entity_type)?,
        EntityId::new(entity.id),
    ))
}
