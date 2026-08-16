import type { Metadata } from "next";
import { FolderGit2 } from "lucide-react";

import { listProjects, orgNotFound } from "@/features/orgs/api/orgs";

import { EmptyState } from "../../../_components/empty-state";
import { PageHeader } from "../../../_components/page-header";
import { ResourceList } from "../_components/resource-list";

export const metadata: Metadata = { title: "Projects — garam" };

type Params = { params: Promise<{ org: string }> };

export default async function ProjectsPage({ params }: Params) {
  const { org } = await params;
  const projects = await listProjects(org).catch(orgNotFound);

  return (
    <>
      <PageHeader
        title="Projects"
        description="The units of work this organization owns."
      />
      {projects.length === 0 ? (
        <EmptyState
          icon={FolderGit2}
          title="No projects"
          description="This organization has no projects."
        />
      ) : (
        <ResourceList resources={projects} />
      )}
    </>
  );
}
