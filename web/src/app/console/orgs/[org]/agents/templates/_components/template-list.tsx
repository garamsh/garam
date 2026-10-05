import Link from "next/link";

import { Card, CardTitle } from "@/components/ui/card";
import { slugFromGrn } from "@/features/orgs";
import type { AgentTemplate } from "@/features/orgs";

/**
 * The organization's archived templates, each linking to the page that
 * shows its record. A template's values are shown there rather than
 * here.
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
              <div className="flex flex-col gap-0.5">
                <CardTitle className="text-base">
                  <Link
                    href={`/console/orgs/${org}/agents/templates/${slugFromGrn(template.grn)}`}
                    className="wrap-anywhere underline-offset-4 hover:underline"
                  >
                    {template.name}
                  </Link>
                </CardTitle>
                {/* Names repeat, so the identifier tells two apart. */}
                <p className="font-mono text-xs text-muted-foreground">
                  {slugFromGrn(template.grn)}
                </p>
              </div>
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
