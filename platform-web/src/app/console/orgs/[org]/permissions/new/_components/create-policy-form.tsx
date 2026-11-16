"use client";

import Link from "next/link";
import { useActionState, useState, useTransition } from "react";

import { FormField } from "@garam/design/components/form-field";
import { Refusal } from "@garam/design/components/refusal";
import { Button } from "@garam/design/ui/button";
import { Input } from "@garam/design/ui/input";
import type { IamPolicyKind, RoleLinkChoices } from "@/features/orgs";
import { createPolicy, type CreatePolicyState } from "@/features/orgs/actions";

import { orgHref } from "../../../../../_components/navigation";
import { CedarEditor } from "../../_components/cedar-editor";
import { POLICY_KIND_LABEL } from "../../_components/policy-kind";
import { RoleLinkFields } from "./role-link-fields";

const initialState: CreatePolicyState = { status: "idle" };

/** What each kind is, said beside its choice. */
const KIND_NOTE: Record<IamPolicyKind, string> = {
  guardrail: "Cedar that forbids, whoever else permits.",
  permit: "Cedar that permits. Only an owner writes one.",
  "role-link": "Who holds a role over the organization, a team or a project.",
};

/**
 * Creates a policy of a kind the caller is offered and opens it. It is
 * stored as a draft, so it decides nothing until activated on its own
 * page. A refusal is said where it belongs: the name's at the name, the
 * Cedar text's at the editor, a role link's above the buttons.
 * The fields are controlled, so what was typed survives a refusal.
 */
export function CreatePolicyForm({
  org,
  orgGrn,
  offered,
  choices,
}: {
  org: string;
  orgGrn: string;
  /** The kinds the caller is not known to be denied, in display order. */
  offered: readonly IamPolicyKind[];
  choices: RoleLinkChoices;
}) {
  const [state, formAction, pending] = useActionState(
    createPolicy,
    initialState,
  );
  const [, startTransition] = useTransition();
  const [kind, setKind] = useState<IamPolicyKind>(offered[0] ?? "guardrail");
  const [name, setName] = useState("");
  const [text, setText] = useState("");
  const at = state.status === "error" ? state.at : undefined;
  const message = state.status === "error" ? state.message : undefined;

  return (
    <form
      onSubmit={(event) => {
        // Submitted by hand, not as the form's `action`: React resets
        // an action's form once it returns, which would empty the
        // role form's selects and lose a refused link's choices.
        event.preventDefault();
        const data = new FormData(event.currentTarget);
        startTransition(() => formAction(data));
      }}
      className="flex flex-col gap-4"
    >
      <input type="hidden" name="org" value={org} />
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-1 font-medium">Kind</legend>
        {offered.map((option) => (
          <label
            key={option}
            className="flex items-start gap-2.5 rounded-lg border px-3 py-2.5 has-checked:border-primary"
          >
            <input
              type="radio"
              name="kind"
              value={option}
              checked={kind === option}
              onChange={() => setKind(option)}
              className="mt-0.5 size-4 accent-primary"
            />
            <span className="flex flex-col gap-0.5">
              <span className="font-medium">{POLICY_KIND_LABEL[option]}</span>
              <span className="text-muted-foreground">{KIND_NOTE[option]}</span>
            </span>
          </label>
        ))}
      </fieldset>
      <FormField label="Name" error={at === "name" ? message : undefined}>
        {(control) => (
          <Input
            {...control}
            name="name"
            required
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="Deny project deletion"
            className="max-w-sm"
          />
        )}
      </FormField>
      {kind === "role-link" ? (
        <>
          <RoleLinkFields orgGrn={orgGrn} choices={choices} />
          {at === "body" && <Refusal>{message}</Refusal>}
        </>
      ) : (
        <CedarEditor
          value={text}
          onChange={setText}
          error={at === "body" ? message : undefined}
        />
      )}
      {at === "form" && <Refusal>{message}</Refusal>}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={pending}>
          {pending ? "Creating…" : "Create policy"}
        </Button>
        <Button asChild variant="ghost">
          <Link href={orgHref(org, "permissions")}>Cancel</Link>
        </Button>
      </div>
    </form>
  );
}
