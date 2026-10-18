import type { Metadata } from "next";

import { getOrg, lookupProjects, slugFromGrn } from "@/features/orgs";
import { checkPermissions } from "@/lib/permission-check";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { RefusedRead } from "../../../_components/refused-read";
import { ListPage } from "../../../_components/templates/list-page";
import { CreateProjectDialog } from "./_components/create-project-dialog";
import { ProjectTable } from "./_components/project-table";

/** Head metadata for the projects route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Projects — garam" };

type Params = { params: Promise<{ org: string }> };

const DESCRIPTION = "The units of work this organization owns.";

/**
 * The organization's projects as a list, and the dialog that creates
 * one more. The page asks the server once whether the caller may
 * create, and leaves the control out only on an explicit deny; an
 * unanswered check renders it and the API refuses (ADR-0103).
 *
 * The organization is read beside the list so that a refused list is
 * a state of its own: a caller who is not a member is still answered
 * not-found by that read (ADR-0011).
 */
export default async function ProjectsPage({ params }: Params) {
  const { org } = await params;
  const [organization, projects, permissions] = await Promise.all([
    getOrg(org),
    lookupProjects(org),
    checkPermissions(org, { create: { action: "project:create" } }),
  ]);

  const create =
    permissions.get("create") === "denied" ? undefined : (
      <CreateProjectDialog org={org} />
    );

  if (
    organization.status === "unreachable" ||
    projects.status === "unreachable"
  ) {
    return (
      <ListPage title="Projects" description={DESCRIPTION}>
        <ApiUnreachable subject="this organization's projects" />
      </ListPage>
    );
  }

  return (
    <ListPage
      title="Projects"
      description={DESCRIPTION}
      count={projects.status === "ok" ? projects.data.length : undefined}
      primaryAction={create}
    >
      {projects.status === "forbidden" ? (
        <RefusedRead subject="this organization's projects" />
      ) : (
        <ProjectTable
          rows={projects.data.map((project) => ({
            name: project.name,
            identifier: slugFromGrn(project.grn),
            roles: project.roles,
          }))}
        />
      )}
    </ListPage>
  );
}
