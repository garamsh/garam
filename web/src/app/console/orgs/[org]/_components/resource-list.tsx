import { Badge } from "@/components/ui/badge";
import { Card, CardDescription, CardTitle } from "@/components/ui/card";

/** A group or project: both carry a name, a GRN and scoped roles. */
type Resource = { grn: string; name: string; roles: string[] };

/**
 * Renders the groups or projects of an organization. Groups and
 * projects share a shape in the API, so they share a list here.
 */
export function ResourceList({
  title,
  emptyMessage,
  resources,
}: {
  title: string;
  emptyMessage: string;
  resources: Resource[];
}) {
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-semibold">{title}</h2>
      {resources.length === 0 ? (
        <p className="text-sm text-muted-foreground">{emptyMessage}</p>
      ) : (
        <ul className="flex flex-col gap-3">
          {resources.map((resource) => (
            <li key={resource.grn}>
              <Card className="gap-2 p-4">
                <CardTitle className="text-base">{resource.name}</CardTitle>
                <CardDescription className="font-mono text-xs">
                  {resource.grn}
                </CardDescription>
                {resource.roles.length > 0 && (
                  <div className="flex flex-wrap gap-1.5 pt-1">
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
      )}
    </section>
  );
}
