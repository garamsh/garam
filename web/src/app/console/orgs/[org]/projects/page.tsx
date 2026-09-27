import type { Metadata } from "next";
import { FolderGit2 } from "lucide-react";

import { Separator } from "@/components/ui/separator";
import { listProjects } from "@/features/orgs";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { EmptyState } from "../../../_components/empty-state";
import { PageHeader } from "../../../_components/page-header";
import { CreateProjectForm } from "./_components/create-project-form";
import { ProjectList } from "./_components/project-list";

/** Head metadata for the projects route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Projects — garam" };

type Params = { params: Promise<{ org: string }> };

/**
 * The organization's projects, and the form that creates one more.
 *
 * Creating is granted by the `admin` role, which `api/openapi.yaml`
 * names on the operation, while every member reads the list. The form
 * is rendered for all of them and the API refuses a caller that holds
 * only the read — the same shape the operators section takes.
 */
export default async function ProjectsPage({ params }: Params) {
  const { org } = await params;
  const projects = await listProjects(org);

  return (
    <>
      <PageHeader
        title="Projects"
        description="The units of work this organization owns."
      />
      {projects.status === "unreachable" ? (
        <ApiUnreachable subject="this organization's projects" />
      ) : projects.data.length === 0 ? (
        <EmptyState
          icon={FolderGit2}
          title="No projects"
          description="This organization has no projects."
        />
      ) : (
        <ProjectList projects={projects.data} />
      )}
      <Separator />
      <CreateProjectForm org={org} />
    </>
  );
}
