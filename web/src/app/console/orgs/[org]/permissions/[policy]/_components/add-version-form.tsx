"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  addPolicyVersion,
  type AddPolicyVersionState,
} from "@/features/orgs/actions";
import { parsePolicyDocument } from "@/lib/policy-document";

const initialState: AddPolicyVersionState = { status: "idle" };

/**
 * Saves a new version from an editor opened on the default one.
 * Versions are append-only and the policy goes on evaluating its
 * default, so saving changes no check; the answer says which version
 * is still in use. The editor is held by the form, so a refusal leaves
 * the document as typed.
 */
export function AddVersionForm({
  org,
  policy,
  defaultVersion,
  initialDocument,
}: {
  org: string;
  policy: string;
  defaultVersion: number;
  initialDocument: string;
}) {
  const [state, formAction, pending] = useActionState(
    addPolicyVersion,
    initialState,
  );
  const [document, setDocument] = useState(initialDocument);
  const parsed = parsePolicyDocument(document);
  const message =
    parsed.status === "error"
      ? parsed.message
      : state.status === "error"
        ? state.message
        : null;

  return (
    <form action={formAction} className="flex max-w-2xl flex-col gap-3">
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="policy" value={policy} />
      <Label htmlFor="version-document">New version</Label>
      <Textarea
        id="version-document"
        name="document"
        rows={12}
        spellCheck={false}
        value={document}
        onChange={(event) => setDocument(event.target.value)}
        className="font-mono text-sm"
      />
      {message !== null && (
        <p role="alert" className="text-sm text-destructive">
          {message}
        </p>
      )}
      {state.status === "saved" &&
        state.version !== defaultVersion &&
        message === null && (
          <p role="status" className="text-sm">
            Saved as version {state.version}. Version {defaultVersion} is still
            the default; make version {state.version} the default to use it.
          </p>
        )}
      <div>
        <Button type="submit" disabled={pending || parsed.status === "error"}>
          {pending ? "Saving…" : "Save version"}
        </Button>
      </div>
    </form>
  );
}
