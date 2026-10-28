"use client";

import { Button } from "@garam/design/ui/button";
import {
  deleteProject,
  type DeleteProjectState,
} from "@/features/orgs/actions";

import { ActionDialog } from "../../../../../_components/action-dialog";

const initialState: DeleteProjectState = { status: "idle" };

/**
 * Deletes the project once its name is typed. garam brings a deleted
 * project back by no route, and an agent's GRN names the project it
 * was minted in (ADR-0001), so one click is not enough. A deleted
 * project opens the list.
 */
export function DeleteProjectDialog({
  org,
  project,
  projectName,
  memberships,
}: {
  org: string;
  project: string;
  projectName: string;
  /** How many memberships go with it, as a phrase. */
  memberships: string;
}) {
  return (
    <ActionDialog
      trigger={
        <Button type="button" variant="destructive">
          Delete project
        </Button>
      }
      title={`Delete ${projectName}?`}
      description={<p>Removes its {memberships}. Its members stay.</p>}
      action={deleteProject}
      initialState={initialState}
      fields={{ org, project }}
      typed={{ phrase: projectName }}
      command={{
        label: "Delete project",
        consequence: "Removes the project and its memberships, for good.",
      }}
      confirmLabel="Delete project"
      pendingLabel="Deleting…"
      destructive
      done={(state) => state.status !== "error"}
    />
  );
}
