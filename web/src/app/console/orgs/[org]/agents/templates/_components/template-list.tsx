import Link from "next/link";

import { Card, CardTitle } from "@/components/ui/card";
import { slugFromGrn } from "@/features/orgs";
import type { AgentTemplate } from "@/features/orgs";

/**
 * The organization's templates, each linking to its own page. A
 * template's values are shown there rather than here: the list is for
 * choosing one.
 */
export function TemplateList({
  org,
  templates,
}: {
  org: string;
  templates: readonly AgentTemplate[];
}) {
  return (
    <ul className="flex flex-col gap-3">
      {templates.map((template) => {
        const count = Object.keys(template.values).length;
        return (
          <li key={template.grn}>
            <Card className="gap-2 p-4">
              <CardTitle className="text-base">
                <Link
                  href={`/console/orgs/${org}/agents/templates/${slugFromGrn(template.grn)}`}
                  className="underline-offset-4 hover:underline"
                >
                  {template.name}
                </Link>
              </CardTitle>
              {template.description !== "" && (
                <p className="text-sm text-muted-foreground">
                  {template.description}
                </p>
              )}
              <p className="text-xs text-muted-foreground">
                {count === 1 ? "1 value" : `${count} values`} · updated{" "}
                <time dateTime={template.updatedAt}>
                  {template.updatedAt.slice(0, 10)}
                </time>
              </p>
            </Card>
          </li>
        );
      })}
    </ul>
  );
}
