import { Badge } from "@/components/ui/badge";
import { Card, CardTitle } from "@/components/ui/card";
import type { Project } from "@/features/orgs";

/** Renders the projects of an organization by name and scoped roles. */
export function ProjectList({ projects }: { projects: Project[] }) {
  return (
    <ul className="flex flex-col gap-3">
      {projects.map((project) => (
        <li key={project.grn}>
          <Card className="flex-row items-center justify-between gap-4 p-4">
            <CardTitle className="text-base">{project.name}</CardTitle>
            {project.roles.length > 0 && (
              <div className="flex flex-wrap justify-end gap-1.5">
                {project.roles.map((role) => (
                  <Badge key={role} variant="secondary">
                    {role}
                  </Badge>
                ))}
              </div>
            )}
          </Card>
        </li>
      ))}
    </ul>
  );
}
