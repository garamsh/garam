import { Badge } from "@/components/ui/badge";
import type { ConfigurationStatus } from "@/lib/agent-configuration";

/** How a status is drawn, out of what the badge offers. */
type BadgeVariant = React.ComponentProps<typeof Badge>["variant"];

/** The label, marking and sentence each status takes. */
function describe(status: ConfigurationStatus): {
  label: string;
  variant: BadgeVariant;
  detail: string;
} {
  switch (status.kind) {
    case "unchecked":
      return {
        label: "Not checked",
        variant: "outline",
        detail: "Reported agents are not listed to you.",
      };
    case "unknown":
      return {
        label: "Unknown",
        variant: "outline",
        detail: {
          unreported: "No operator has reported applying these settings.",
          unassigned: "No operator holds this agent.",
          replaced: "The last report came from a replaced operator.",
          newer: "A newer revision was saved; reload to see it.",
        }[status.reason],
      };
    case "pending":
      return {
        label: "Pending",
        variant: "outline",
        detail: `The operator last reported revision ${status.acknowledged}.`,
      };
    case "applied":
      return {
        label: "Applied",
        variant: "secondary",
        detail: "The operator reports this revision applied.",
      };
    case "partial":
      return {
        label: "Partly applied",
        variant: "outline",
        detail: `The operator reports ignoring ${status.ignoredKeys.join(", ")}.`,
      };
    case "rejected":
      return {
        label: "Rejected",
        variant: "destructive",
        detail: `The operator reports: ${status.reason}`,
      };
  }
}

/**
 * Whether the saved revision is reported applied (ADR-0076). Only the
 * operator's report can say so, and the page says it is a report:
 * garam checks none of it.
 */
export function ConfigurationState({
  status,
}: {
  status: ConfigurationStatus;
}) {
  const { label, variant, detail } = describe(status);
  return (
    <div className="flex flex-col gap-1 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <Badge variant={variant}>{label}</Badge>
        <span className="wrap-anywhere text-muted-foreground">{detail}</span>
      </div>
      {"reportedAt" in status && (
        <p className="text-muted-foreground">
          Reported{" "}
          <time dateTime={status.reportedAt}>
            {status.reportedAt.slice(0, 10)}
          </time>
        </p>
      )}
    </div>
  );
}
