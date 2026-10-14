"use client";

import { useActionState, useState } from "react";

import { JsonDiff } from "@/components/json-diff";
import { StatusDot } from "@/components/status-dot";
import { Button } from "@/components/ui/button";
import {
  addPolicyVersion,
  publishPolicyVersion,
  type AddPolicyVersionState,
  type PublishPolicyVersionState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";
import {
  formatPolicyDocument,
  hasCondition,
  parsePolicyDocument,
  type DocumentProblem,
} from "@/lib/policy-document";

import { ActionDialog } from "../../_components/action-dialog";
import { ConditionNotice } from "../../_components/condition-notice";
import {
  OrgWideConfirmation,
  ScopeConfirmationNotice,
} from "../../_components/org-wide-confirmation";
import { Outcome, useReportOutcome } from "../../_components/outcome";
import { PolicyEditor } from "../../_components/policy-editor";
import { MakeDefaultDialog } from "./make-default-dialog";

const savedInitial: AddPolicyVersionState = { status: "idle" };
const publishInitial: PublishPolicyVersionState = { status: "idle" };

/** Whether a document's text carries a Condition on any statement. */
function conditionedText(text: string): boolean {
  const parsed = parsePolicyDocument(text);
  return parsed.status === "ok" && hasCondition(parsed.value);
}

/** A refusal of the document, as the editor shows it. */
function documentProblemOf(
  state: AddPolicyVersionState | PublishPolicyVersionState,
): DocumentProblem | null {
  return state.status === "error" && state.field === "document"
    ? { message: state.message, path: state.path }
    : null;
}

/**
 * The policy's document, opened on its default version, and the two
 * ways a change leaves the page: published as the next version — saved
 * and made the default in one confirmed step, after a diff against the
 * version it replaces — or saved without publishing, which changes no
 * check. Where the server denies changing the policy the document is
 * shown read-only and neither is offered (ADR-0103).
 *
 * A refusal of the document stays on the editor until the text it was
 * about is edited.
 */
export function DocumentPanel({
  org,
  policy,
  policyName,
  active,
  defaultVersion,
  nextVersion,
  defaultText,
  reach,
  orgWide,
  canUpdate,
}: {
  org: string;
  policy: string;
  policyName: string;
  active: boolean;
  defaultVersion: number;
  /** The number garam will give the next version, where it is known. */
  nextVersion: number | null;
  /** The default version's document, or null where it was not read. */
  defaultText: string | null;
  reach: string;
  orgWide: boolean;
  canUpdate: boolean;
}) {
  const hydrated = useHydrated();
  const report = useReportOutcome();
  const base = defaultText ?? "";
  const [document, setDocument] = useState(base);
  // A default that moves under an untouched editor moves the editor
  // too, and ends a half-done publish, which the move completed.
  const [shownBase, setShownBase] = useState(base);
  // The answer that last refused the document, and the text it refused.
  const [refused, setRefused] = useState<{
    text: string;
    problem: DocumentProblem;
  } | null>(null);
  // A version garam saved whose making it the default was refused.
  const [unpublished, setUnpublished] = useState<{
    version: number;
    text: string;
    message: string;
  } | null>(null);
  if (shownBase !== base) {
    setShownBase(base);
    if (document === shownBase) setDocument(base);
    setUnpublished(null);
  }
  const remember = (
    text: string,
    state: AddPolicyVersionState | PublishPolicyVersionState,
  ) => {
    const problem = documentProblemOf(state);
    setRefused(problem === null ? null : { text, problem });
  };

  const [saved, saveAction, saving] = useActionState(
    async (previous: AddPolicyVersionState, formData: FormData) => {
      const next = await addPolicyVersion(previous, formData);
      remember(String(formData.get("document")), next);
      return next;
    },
    savedInitial,
  );

  const parsed = parsePolicyDocument(document);
  // The text as garam would store it: a minified paste of the default
  // is no change, and the diff compares the two as stored.
  const normalized =
    parsed.status === "ok" ? formatPolicyDocument(parsed.value) : document;
  const changed = normalized !== base;
  const problem =
    refused !== null && refused.text === document ? refused.problem : null;
  const label = nextVersion === null ? "Publish" : `Publish v${nextVersion}`;
  // Publishing the text of a version saved but not made the default
  // would append it again; making that version the default finishes it.
  const halfDone = unpublished !== null && unpublished.text === normalized;
  const sendable = hydrated && parsed.status === "ok" && changed && !halfDone;

  return (
    <section aria-labelledby="document-title" className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-col gap-0.5">
          <h2 id="document-title" className="text-base">
            {canUpdate ? "Edit the policy" : "Document"}
          </h2>
          <p className="text-muted-foreground">
            {defaultText === null
              ? "The default version could not be read, so the editor starts empty."
              : `Opened on v${defaultVersion}, the default.`}
          </p>
        </div>
        {canUpdate && (
          <StatusDot
            tone={changed ? "info" : "neutral"}
            label={changed ? "Edited, not published" : "No changes"}
          />
        )}
      </div>

      <PolicyEditor
        value={document}
        onChange={setDocument}
        problem={problem}
        readOnly={!canUpdate}
        hint={
          canUpdate
            ? "Publishing saves this as a new version and makes it the default. Earlier versions are kept, and any of them can be made the default again."
            : "Your access lets you read this policy, not change it."
        }
      />

      {canUpdate && (
        <div className="flex flex-col gap-3 border-t pt-4 sm:flex-row sm:flex-wrap sm:items-center">
          <ActionDialog
            trigger={
              <Button type="button" disabled={!sendable}>
                {label}
              </Button>
            }
            title={`${label} of ${policyName}?`}
            description={
              <p>
                {active
                  ? `It becomes the default and applies at once. ${reach}`
                  : "It becomes the default. Nothing is evaluated until the policy is activated."}
              </p>
            }
            action={publishPolicyVersion}
            initialState={publishInitial}
            fields={{ org, policy, document }}
            confirmLabel={label}
            pendingLabel="Publishing…"
            done={(state) =>
              state.status === "published" ||
              state.status === "saved-not-default" ||
              documentProblemOf(state) !== null
            }
            onAnswer={(state) => {
              remember(document, state);
              if (state.status === "published") {
                setDocument(normalized);
                report?.("document", `Published v${state.version}.`);
              } else if (state.status === "saved-not-default") {
                setUnpublished({
                  version: state.version,
                  text: normalized,
                  message: state.message,
                });
              }
            }}
          >
            {(state) => (
              <>
                {defaultText !== null && (
                  <JsonDiff
                    before={defaultText}
                    after={normalized}
                    beforeLabel={`v${defaultVersion} (default)`}
                    afterLabel={
                      nextVersion === null ? "New version" : `v${nextVersion}`
                    }
                  />
                )}
                {parsed.status === "ok" && hasCondition(parsed.value) && (
                  <ConditionNotice />
                )}
                <ScopeConfirmationNotice state={state} />
                {((active && orgWide) ||
                  state.status === "confirm-org-wide" ||
                  state.status === "confirm-unknown-scope") && (
                  <OrgWideConfirmation id="publish-org-wide" />
                )}
              </>
            )}
          </ActionDialog>

          <form
            action={saveAction}
            className="flex flex-wrap items-center gap-3"
          >
            <input type="hidden" name="org" value={org} />
            <input type="hidden" name="policy" value={policy} />
            <input type="hidden" name="document" value={document} />
            <Button
              type="submit"
              variant="outline"
              disabled={!sendable || saving}
            >
              {saving ? "Saving…" : "Save without publishing"}
            </Button>
            <span
              role="status"
              className="text-sm text-muted-foreground empty:hidden"
            >
              {saved.status === "saved" &&
                `Saved as v${saved.version}. v${defaultVersion} is still the default.`}
            </span>
            {saved.status === "error" && saved.field === undefined && (
              <p role="alert" className="text-sm text-destructive">
                {saved.message}
              </p>
            )}
          </form>

          <Outcome area="document" />

          {changed && (
            <Button
              type="button"
              variant="ghost"
              className="sm:ml-auto"
              onClick={() => {
                setDocument(base);
                setUnpublished(null);
              }}
            >
              Discard changes
            </Button>
          )}
        </div>
      )}

      {canUpdate && unpublished !== null && (
        <div
          role="alert"
          className="flex flex-wrap items-center gap-3 rounded-xl border border-warn/40 bg-warn/10 px-4 py-3"
        >
          <p className="min-w-0 flex-1">
            Saved as v{unpublished.version}, but it is not the default:{" "}
            {unpublished.message}
          </p>
          <MakeDefaultDialog
            org={org}
            policy={policy}
            current={defaultVersion}
            currentText={defaultText}
            target={unpublished.version}
            targetText={unpublished.text}
            active={active}
            reach={reach}
            orgWide={orgWide}
            conditioned={conditionedText(unpublished.text)}
            area="document"
          />
        </div>
      )}
    </section>
  );
}
