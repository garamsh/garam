import { Badge } from "@/components/ui/badge";

/** An IAM-style policy document, as the authz engine stores one. */
const DOCUMENT = `{
  "Version": "2026-07-25",
  "Statement": [
    {
      "Sid": "AllowOrgRead",
      "Effect": "Allow",
      "Action": ["org:read", "project:list"],
      "Resource": ["grn:acme:*"]
    }
  ]
}`;

/** Shows a real policy document rather than describing one. */
export function PolicyPanel() {
  return (
    <figure className="overflow-hidden rounded-xl border border-border bg-card">
      <figcaption className="flex items-center gap-3 border-b border-border bg-muted/40 px-4 py-2.5">
        <span className="truncate font-mono text-xs text-muted-foreground">
          grn:acme:*:policy:org-read
        </span>
        <span className="ml-auto flex shrink-0 items-center gap-1.5">
          <Badge variant="outline" className="font-mono">
            v3
          </Badge>
          <Badge variant="secondary">active</Badge>
        </span>
      </figcaption>
      <pre className="overflow-x-auto px-4 py-4 font-mono text-xs leading-relaxed">
        {DOCUMENT}
      </pre>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-border px-4 py-3">
        <span className="text-eyebrow font-medium text-muted-foreground uppercase">
          Evaluation
        </span>
        <span className="ml-auto flex items-center gap-1.5 font-mono text-xs">
          <span className="text-destructive">Deny</span>
          <span className="text-muted-foreground">&gt;</span>
          <span className="text-brand">Allow</span>
          <span className="text-muted-foreground">&gt;</span>
          <span className="text-muted-foreground">default Deny</span>
        </span>
      </div>
    </figure>
  );
}
