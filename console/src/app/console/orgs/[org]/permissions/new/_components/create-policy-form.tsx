"use client";

import { useActionState, useState } from "react";

import { FormField } from "@/components/form-field";
import { Refusal } from "@/components/refusal";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { createPolicy, type CreatePolicyState } from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";
import {
  formatPolicyDocument,
  parsePolicyDocument,
  type DocumentProblem,
} from "@/lib/policy-document";

import { PolicyEditor } from "../../_components/policy-editor";

const initialState: CreatePolicyState = { status: "idle" };

/**
 * Creates a policy from a name and its first document. garam stores it
 * as a draft, so what is typed here grants nothing until the policy is
 * activated. Both are held by the form, so a refusal leaves them as
 * typed, and it is not sent before it can hold them. The starting
 * document names the resource the API authorizes a project write
 * against, so it matches what it says.
 */
export function CreatePolicyForm({ org }: { org: string }) {
  const hydrated = useHydrated();
  const [name, setName] = useState("");
  const [document, setDocument] = useState(() =>
    formatPolicyDocument({
      Version: "2026-07-25",
      Statement: [
        {
          Effect: "Deny",
          Action: ["project:update"],
          Resource: [`grn:${org}:default:project:*`],
        },
      ],
    }),
  );
  // The text a refusal was about: once edited, the refusal is stale.
  const [sent, setSent] = useState<string | null>(null);
  const [state, formAction, pending] = useActionState(
    (previous: CreatePolicyState, formData: FormData) => {
      setSent(String(formData.get("document")));
      return createPolicy(previous, formData);
    },
    initialState,
  );
  const documentProblem: DocumentProblem | null =
    state.status === "error" && state.field === "document" && sent === document
      ? { message: state.message, path: state.path }
      : null;
  const formProblem =
    state.status === "error" && state.field === undefined
      ? state.message
      : null;
  const parsed = parsePolicyDocument(document);

  return (
    <form action={formAction} className="flex flex-col gap-6">
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="document" value={document} />
      <FormField
        label="Policy name"
        hint="What people will know it by, such as “Deny project deletion”."
        error={
          state.status === "error" && state.field === "name"
            ? state.message
            : undefined
        }
        className="max-w-sm"
      >
        {(control) => (
          <Input
            {...control}
            name="name"
            required
            autoComplete="off"
            value={name}
            onChange={(event) => setName(event.target.value)}
          />
        )}
      </FormField>
      <PolicyEditor
        value={document}
        onChange={setDocument}
        problem={documentProblem}
        hint="Statements of Effect, Action and Resource. An explicit Deny wins over every Allow; a Condition is stored and not evaluated."
      />
      <div className="flex flex-wrap items-center gap-3 border-t pt-4">
        <Button
          type="submit"
          disabled={!hydrated || pending || parsed.status === "error"}
        >
          {pending ? "Creating…" : "Create draft"}
        </Button>
        <p className="text-sm text-muted-foreground">
          A draft grants no access until you activate it.
        </p>
      </div>
      {formProblem !== null && <Refusal>{formProblem}</Refusal>}
    </form>
  );
}
