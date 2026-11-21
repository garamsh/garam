import type { IamPolicyBody, IamRoleLink } from "@/features/orgs";

/** Where a role link reaches, as the word the console shows. */
const SCOPE_LABEL: Record<IamRoleLink["scope"], string> = {
  organization: "Organization",
  "team-or-project": "Team or project",
};

/**
 * A version's body as text, for comparing two of them line by line:
 * the Cedar text of a guardrail or a permit, or a role link's four
 * facts.
 */
export function bodyText(body: IamPolicyBody): string {
  switch (body.kind) {
    case "role-link":
      return [
        `role: ${body.roleLink.role}`,
        `scope: ${body.roleLink.scope}`,
        `principal: ${body.roleLink.principal}`,
        `resource: ${body.roleLink.resource}`,
      ].join("\n");
    case "guardrail":
    case "permit":
      return body.text;
  }
}

/**
 * One version's body, read-only: Cedar text as written, or a role
 * link's role, scope, principal and resource. A new version of Cedar
 * is written in the editor, and a role link takes none.
 */
export function PolicyBody({ body }: { body: IamPolicyBody }) {
  switch (body.kind) {
    case "role-link":
      return (
        <dl className="grid gap-x-6 gap-y-3 sm:grid-cols-2">
          <div className="flex flex-col">
            <dt className="text-xs text-muted-foreground">Role</dt>
            <dd className="capitalize">{body.roleLink.role}</dd>
          </div>
          <div className="flex flex-col">
            <dt className="text-xs text-muted-foreground">Scope</dt>
            <dd>{SCOPE_LABEL[body.roleLink.scope]}</dd>
          </div>
          <div className="flex min-w-0 flex-col">
            <dt className="text-xs text-muted-foreground">Principal</dt>
            <dd className="break-all font-mono text-table">
              {body.roleLink.principal}
            </dd>
          </div>
          <div className="flex min-w-0 flex-col">
            <dt className="text-xs text-muted-foreground">Resource</dt>
            <dd className="break-all font-mono text-table">
              {body.roleLink.resource}
            </dd>
          </div>
        </dl>
      );
    case "guardrail":
    case "permit":
      return (
        <pre
          tabIndex={0}
          aria-label="Cedar text"
          className="max-h-96 overflow-auto rounded-lg border bg-muted/30 p-3 font-mono text-table leading-[1.6] focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
        >
          {body.text}
        </pre>
      );
  }
}
