"use client";

import Link from "next/link";
import { useActionState, useState } from "react";

import { Refusal } from "@garam/design/components/refusal";
import { Button } from "@garam/design/ui/button";
import type { IamPolicyKind } from "@/features/orgs";
import {
  createPolicyVersion,
  type CreatePolicyVersionState,
} from "@/features/orgs/actions";

import { orgHref } from "../../../../../../_components/navigation";
import { CedarEditor } from "../../../_components/cedar-editor";

const initialState: CreatePolicyVersionState = { status: "idle" };

/**
 * Writes the next version of a guardrail or a permit, starting from the
 * text of the version the policy decides by. A stored version is not
 * the default, and the form says so beside the link that returns to the
 * policy, where the default is moved. A refusal is the editor's error when
 * the policy service refused the text, and above the buttons otherwise.
 * The text is controlled so that the reset React runs after an action
 * leaves what was typed.
 */
export function NewVersionForm({
  org,
  policy,
  kind,
  startingText,
}: {
  org: string;
  policy: string;
  kind: Exclude<IamPolicyKind, "role-link">;
  startingText: string;
}) {
  const [state, formAction, pending] = useActionState(
    createPolicyVersion,
    initialState,
  );
  const [text, setText] = useState(startingText);
  const policyHref = `${orgHref(org, "permissions")}/${policy}`;

  return (
    <form action={formAction} className="flex flex-col gap-4">
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="policy" value={policy} />
      <input type="hidden" name="kind" value={kind} />
      <CedarEditor
        value={text}
        onChange={setText}
        error={
          state.status === "error" && state.at === "body"
            ? state.message
            : undefined
        }
      />
      {state.status === "error" && state.at !== "body" && (
        <Refusal>{state.message}</Refusal>
      )}
      {state.status === "stored" && (
        <p role="status">
          Stored as v{state.version}. It is not the default yet: make it the
          default from the policy&apos;s versions.
        </p>
      )}
      <div className="flex flex-wrap gap-2">
        <Button type="submit" disabled={pending}>
          {pending ? "Storing…" : "Store version"}
        </Button>
        <Button asChild variant="ghost">
          <Link href={policyHref}>
            {state.status === "stored" ? "Back to the policy" : "Cancel"}
          </Link>
        </Button>
      </div>
    </form>
  );
}
