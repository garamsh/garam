"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { createPolicy, type CreatePolicyState } from "@/features/orgs/actions";
import {
  formatPolicyDocument,
  parsePolicyDocument,
} from "@/lib/policy-document";

const initialState: CreatePolicyState = { status: "idle" };

/**
 * Creates a policy from a name and its first document. garam stores it
 * as a draft, so what is typed here grants nothing until the policy is
 * activated. Both fields are held by the form, so a refusal leaves
 * them as typed.
 */
export function CreatePolicyForm({ org }: { org: string }) {
  const [state, formAction, pending] = useActionState(
    createPolicy,
    initialState,
  );
  const [name, setName] = useState("");
  const [document, setDocument] = useState(() =>
    formatPolicyDocument({
      Version: "2026-07-25",
      Statement: [
        {
          Effect: "Allow",
          Action: ["org:read"],
          Resource: [`grn:${org}:*`],
        },
      ],
    }),
  );
  const parsed = parsePolicyDocument(document);
  const message =
    parsed.status === "error"
      ? parsed.message
      : state.status === "error"
        ? state.message
        : null;

  return (
    <form action={formAction} className="flex max-w-2xl flex-col gap-6">
      <input type="hidden" name="org" value={org} />
      <div className="flex flex-col gap-2">
        <Label htmlFor="name">Policy name</Label>
        <Input
          id="name"
          name="name"
          required
          value={name}
          onChange={(event) => setName(event.target.value)}
          className="max-w-sm"
        />
      </div>
      <div className="flex flex-col gap-2">
        <Label htmlFor="document">Document</Label>
        <Textarea
          id="document"
          name="document"
          rows={14}
          spellCheck={false}
          value={document}
          onChange={(event) => setDocument(event.target.value)}
          className="font-mono text-sm"
          aria-describedby="document-hint"
        />
        <p id="document-hint" className="text-sm text-muted-foreground">
          Statements of Effect, Action and Resource. An explicit Deny wins over
          every Allow; a Condition is stored and not evaluated.
        </p>
      </div>
      <p className="text-sm text-muted-foreground">
        Saved as a draft. A draft grants no access until you activate it.
      </p>
      {message !== null && (
        <p role="alert" className="text-sm text-destructive">
          {message}
        </p>
      )}
      <div>
        <Button type="submit" disabled={pending || parsed.status === "error"}>
          {pending ? "Saving…" : "Create draft"}
        </Button>
      </div>
    </form>
  );
}
