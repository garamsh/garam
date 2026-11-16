"use client";

import { useState } from "react";

import type { RoleLinkChoices } from "@/features/orgs";

import { ChoiceField } from "./choice-field";

const ROLES = ["owner", "admin", "member"].map((role) => ({
  value: role,
  label: role,
}));

const SCOPES = [
  {
    value: "organization",
    label: "An agent or a team over the organization",
  },
  {
    value: "team-or-project",
    label: "A person over a team or a project",
  },
] as const;

type Scope = (typeof SCOPES)[number]["value"];

/**
 * The fields of a role link: the role, the scope and so who holds it
 * over what. Only links the platform does not derive from a membership
 * are offered (ADR-0173): an agent or a team over the organization, or
 * a person over a team or a project. A person's own organization role
 * is its membership's, so no choice here names one. A change of scope
 * clears the principal and the resource, whose choices differ.
 */
export function RoleLinkFields({
  orgGrn,
  choices,
}: {
  /** What an organization-scope link is over. */
  orgGrn: string;
  choices: RoleLinkChoices;
}) {
  const [role, setRole] = useState("");
  const [scope, setScope] = useState<Scope>("organization");
  const [principal, setPrincipal] = useState("");
  const [resource, setResource] = useState("");
  const organization = scope === "organization";

  return (
    <div className="flex flex-col gap-4">
      <ChoiceField
        label="Role"
        name="role"
        value={role}
        onChange={setRole}
        choices={ROLES}
        unlisted={[]}
        empty=""
      />
      <input type="hidden" name="scope" value={scope} />
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-1 font-medium">Who holds it</legend>
        {SCOPES.map((option) => (
          <label
            key={option.value}
            className="flex items-center gap-2.5 rounded-lg border px-3 py-2 has-checked:border-primary"
          >
            <input
              type="radio"
              name="scope-choice"
              value={option.value}
              checked={scope === option.value}
              onChange={() => {
                setScope(option.value);
                setPrincipal("");
                setResource("");
              }}
              className="size-4 accent-primary"
            />
            {option.label}
          </label>
        ))}
      </fieldset>
      <ChoiceField
        label={organization ? "Team or agent" : "Person"}
        name="principal"
        value={principal}
        onChange={setPrincipal}
        choices={
          organization
            ? choices.organizationPrincipals
            : choices.scopedPrincipals
        }
        unlisted={
          organization
            ? choices.unlisted.organizationPrincipals
            : choices.unlisted.scopedPrincipals
        }
        empty={
          organization
            ? "The organization holds no team or agent."
            : "The organization holds no person."
        }
      />
      {organization ? (
        <input type="hidden" name="resource" value={orgGrn} />
      ) : (
        <ChoiceField
          label="Team or project"
          name="resource"
          value={resource}
          onChange={setResource}
          choices={choices.scopedResources}
          unlisted={choices.unlisted.scopedResources}
          empty="The organization holds no team or project."
        />
      )}
    </div>
  );
}
