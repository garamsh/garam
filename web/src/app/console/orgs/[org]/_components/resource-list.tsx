import { Badge } from "@/components/ui/badge";
import { Card, CardTitle } from "@/components/ui/card";

/** A group or project: both carry a name and the caller's scoped roles. */
type Resource = { grn: string; name: string; roles: string[] };

/**
 * Renders the groups or projects of an organization. Groups and
 * projects share a shape in the API, so they share a list here.
 */
export function ResourceList({ resources }: { resources: Resource[] }) {
  return (
    <ul className="flex flex-col gap-3">
      {resources.map((resource) => (
        <li key={resource.grn}>
          <Card className="flex-row items-center justify-between gap-4 p-4">
            <CardTitle className="text-base">{resource.name}</CardTitle>
            {resource.roles.length > 0 && (
              <div className="flex flex-wrap justify-end gap-1.5">
                {resource.roles.map((role) => (
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
