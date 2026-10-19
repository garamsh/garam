"use client";

import { useRouter } from "next/navigation";

import { Banner } from "@/components/banner";
import { Button } from "@/components/ui/button";
import type { OrgDeletionPreview } from "@/features/orgs";
import { deleteOrg, type DeleteOrgState } from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

import { ActionDialog } from "../../../../_components/action-dialog";

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
 * (ADR-0065), and the person types the organization's name.
 */
export function DeleteOrgDialog({
  org,
  name,
  read,
}: {
  org: string;
  name: string;
  read: DeletionPreviewRead;
}) {
  const hydrated = useHydrated();
  const router = useRouter();
  return (
    <ActionDialog
      trigger={
        <Button variant="destructive" disabled={!hydrated}>
          Delete organization
        </Button>
      }
      title={`Delete ${name}?`}
      action={deleteOrg}
      initialState={initialState}
      fields={{ org }}
      command={{
        label: "Delete organization",
        consequence:
          "Deletes the organization and everything it holds, for good.",
      }}
      confirmLabel="Delete organization"
      pendingLabel="Deleting…"
      destructive
      typed={{ phrase: name }}
      blocked={read.status !== "ok"}
      done={(state) => state.status !== "error"}
      // Opening reads the preview again, so a page left open does not
      // confirm the deletion against what it held when it loaded.
      onOpen={() => router.refresh()}
    >
      {() => <DeletionPreview read={read} />}
    </ActionDialog>
  );
}

/** What garam counted the deletion takes, or why it did not say. */
function DeletionPreview({ read }: { read: DeletionPreviewRead }) {
  if (read.status !== "ok") {
    return (
      <Banner tone="info" title="What this deletes is not known">
        {read.status === "forbidden"
          ? "garam answers what a deletion takes only to whoever may delete the organization, and it did not answer you."
          : "garam can't be reached right now, so it can't say what this deletes. Try again in a moment."}
      </Banner>
    );
  }
  return (
    <>
      <DeletionList counts={read.preview.counts} />
      {read.preview.certificateAuthority && (
        <Banner tone="danger" title="Its certificate authority goes too">
          Nothing restores it, and every certificate it signed, an
          operator&apos;s included, stops verifying for good.
        </Banner>
      )}
    </>
  );
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
