"use client";

import { useRouter } from "next/navigation";
import { useActionState, useEffect, useId, useState } from "react";

import { Banner } from "@/components/banner";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import type { OrgDeletionPreview } from "@/features/orgs";
import { deleteOrg, type DeleteOrgState } from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

const initialState: DeleteOrgState = { status: "idle" };

type Counts = OrgDeletionPreview["counts"];

/** What each counted kind is called, in the console's own words. */
const KIND: Record<keyof Counts, [one: string, many: string]> = {
  members: ["membership", "memberships"],
  groups: ["team", "teams"],
  projects: ["project", "projects"],
  agents: ["agent", "agents"],
  agentDefinitions: ["agent definition", "agent definitions"],
  agentTemplates: ["agent template", "agent templates"],
  operators: ["operator", "operators"],
  policies: ["policy", "policies"],
  invitations: ["invitation", "invitations"],
  consents: [
    "consent relation, which the other organization loses too",
    "consent relations, which the other organizations lose too",
  ],
};

/**
 * What the dialog was handed of the preview: garam's answer, or why
 * there is none. Without one the dialog has no confirm field, so the
 * delete cannot be confirmed: the act is confirmed against what it
 * destroys (ADR-0103 §Patterns).
 */
export type DeletionPreviewRead =
  | { status: "ok"; preview: OrgDeletionPreview }
  | { status: "forbidden" }
  | { status: "unreachable" };

/**
 * Deletes the organization, which nothing undoes. It is slow on
 * purpose: the dialog lists what garam counted the deletion takes
 * (ADR-0065), and the person types the organization's name. A refusal
 * and the typed name go when the dialog closes.
 */
export function DeleteOrgDialog(props: Props) {
  const hydrated = useHydrated();
  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <Button variant="destructive" disabled={!hydrated}>
          Delete organization
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent className="data-[size=default]:sm:max-w-md">
        <DeleteForm {...props} />
      </AlertDialogContent>
    </AlertDialog>
  );
}

type Props = { org: string; name: string; read: DeletionPreviewRead };

/**
 * The dialog's form, mounted only while the dialog is open. Opening it
 * reads the preview again, so a page left open does not confirm the
 * deletion against what the organization held when it loaded.
 */
function DeleteForm({ org, name, read }: Props) {
  const [state, formAction, pending] = useActionState(deleteOrg, initialState);
  const [typed, setTyped] = useState("");
  const confirmId = useId();
  const router = useRouter();
  useEffect(() => {
    router.refresh();
  }, [router]);

  return (
    <form action={formAction} className="contents">
      <input type="hidden" name="org" value={org} />
      <AlertDialogHeader>
        <AlertDialogTitle>Delete {name}?</AlertDialogTitle>
        <AlertDialogDescription>
          This deletes the organization and everything it holds. It can&apos;t
          be undone.
        </AlertDialogDescription>
      </AlertDialogHeader>
      {read.status === "ok" ? (
        <>
          <DeletionList counts={read.preview.counts} />
          {read.preview.certificateAuthority && (
            <Banner tone="danger" title="Its certificate authority goes too">
              Nothing restores it, and every certificate it signed, an
              operator&apos;s included, stops verifying for good.
            </Banner>
          )}
          <div className="flex flex-col gap-2">
            <Label htmlFor={confirmId}>
              {/* One span, so the label's gap does not split the
                  sentence around the name; the name keeps its spaces as
                  stored, so what is shown is what is typed. */}
              <span>
                Type{" "}
                <span className="font-semibold whitespace-pre-wrap">
                  {name}
                </span>{" "}
                to confirm
              </span>
            </Label>
            <Input
              id={confirmId}
              value={typed}
              onChange={(event) => setTyped(event.target.value)}
              autoComplete="off"
              spellCheck={false}
            />
          </div>
        </>
      ) : (
        <Banner tone="info" title="What this deletes is not known">
          {read.status === "forbidden"
            ? "garam answers what a deletion takes only to whoever may delete the organization, and it did not answer you."
            : "garam can't be reached right now, so it can't say what this deletes. Try again in a moment."}
        </Banner>
      )}
      {state.status === "error" && (
        <p role="alert" className="text-destructive">
          {state.message}
        </p>
      )}
      <AlertDialogFooter>
        <AlertDialogCancel disabled={pending}>Cancel</AlertDialogCancel>
        <Button
          type="submit"
          variant="destructive"
          disabled={!sameName(typed, name) || pending}
        >
          {pending ? "Deleting…" : "Delete organization"}
        </Button>
      </AlertDialogFooter>
    </form>
  );
}

/**
 * Whether what was typed is the name, exactly, once both are in one
 * Unicode form: Hangul stored decomposed (NFD) is typed composed, and
 * would otherwise never match.
 */
function sameName(typed: string, name: string): boolean {
  return typed.normalize("NFC") === name.normalize("NFC");
}

/** The kinds the organization holds, by count; a zero is left out. */
function DeletionList({ counts }: { counts: Counts }) {
  const held = (Object.keys(KIND) as (keyof Counts)[]).filter(
    (kind) => counts[kind] > 0,
  );
  if (held.length === 0) {
    return <p>It holds nothing else yet.</p>;
  }
  return (
    <div className="flex flex-col gap-2">
      <p>It takes with it:</p>
      <ul className="flex flex-col gap-1">
        {held.map((kind) => (
          <li key={kind} className="flex items-baseline gap-2">
            <span className="min-w-6 text-right font-mono tabular-nums">
              {counts[kind]}
            </span>
            <span>{KIND[kind][counts[kind] === 1 ? 0 : 1]}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
