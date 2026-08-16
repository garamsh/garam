import { Badge } from "@/components/ui/badge";

/** The identities one stored policy is attached to. */
const SUBJECTS = [
  { kind: "user", name: "alice" },
  { kind: "group", name: "eng" },
  { kind: "agent", name: "bot-1" },
];

/** Shows one policy reaching a person and an agent on the same terms. */
export function PolicyAttachments() {
  return (
    <figure className="overflow-hidden rounded-xl border border-border bg-card">
      <figcaption className="flex items-center gap-3 border-b border-border bg-muted/40 px-4 py-2.5">
        <span className="truncate font-mono text-xs text-muted-foreground">
          grn:acme:default:policy:a1b2c3d4
        </span>
        <span className="ml-auto flex shrink-0 items-center gap-1.5">
          <Badge variant="outline" className="font-mono">
            v3
          </Badge>
          <Badge variant="secondary">active</Badge>
        </span>
      </figcaption>
      <div className="flex flex-col gap-2 border-b border-border px-4 py-4">
        <span className="text-eyebrow font-medium text-muted-foreground uppercase">
          Statement
        </span>
        <span className="flex flex-wrap items-baseline gap-x-3 gap-y-1 font-mono text-xs">
          <span className="text-brand">Allow</span>
          <span>project:read</span>
          <span className="text-muted-foreground">grn:acme:*</span>
        </span>
      </div>
      <div className="flex flex-col gap-2 px-4 py-4">
        <span className="text-eyebrow font-medium text-muted-foreground uppercase">
          Attached to
        </span>
        <dl className="flex flex-col gap-1.5 font-mono text-xs">
          {SUBJECTS.map(({ kind, name }) => (
            <div key={kind} className="flex items-baseline gap-3">
              <dt className="w-14 text-muted-foreground">{kind}</dt>
              <dd>{name}</dd>
            </div>
          ))}
        </dl>
      </div>
      <p className="border-t border-border px-4 py-3 text-sm text-pretty text-muted-foreground">
        A policy attaches to an identity: a user, a group, or an agent.
      </p>
    </figure>
  );
}
