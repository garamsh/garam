import { Badge } from "@/components/ui/badge";
import type { Agent, Reported } from "@/features/orgs";

/** How a state is drawn, out of what the badge offers. */
type BadgeVariant = React.ComponentProps<typeof Badge>["variant"];

/**
 * What the operator reports about the pod, and the marking each state
 * takes. Only a state that needs attention is drawn loudly, so a column
 * of agents is read by its markings rather than word by word.
 */
const PROVISIONING_BADGE: Record<Agent["provisioningState"], BadgeVariant> = {
  pending: "outline",
  provisioned: "secondary",
  ready: "secondary",
  failed: "destructive",
};

/**
 * What the agent reports about itself, marked on the same terms.
 * `unknown` is an agent that has not reported since it was created.
 */
const RUNTIME_BADGE: Record<Agent["runtimeState"], BadgeVariant> = {
  unknown: "outline",
  connected: "secondary",
  disconnected: "destructive",
};

/**
 * What operators and the agent itself have reported, kept apart from
 * what the organization asked for. An agent no operator has claimed has
 * no reported state at all, which is said rather than drawn as
 * `pending`; a refused list is said as not checked. The two reported
 * states are both named and neither is summarized into the other
 * (ADR-0053). `Updated` is when a report last landed — nothing expires
 * one.
 */
export function ReportedState({
  reported,
  agent,
}: {
  reported: Reported;
  agent: Agent | undefined;
}) {
  if (reported === "unchecked") {
    return (
      <p className="text-sm text-muted-foreground">
        Not checked: reported agents are not listed to you.
      </p>
    );
  }
  if (reported === "awaiting" || agent === undefined) {
    return (
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <Badge variant="outline">Awaiting operator</Badge>
        <span className="text-muted-foreground">No report yet.</span>
      </div>
    );
  }
  return (
    <dl className="grid grid-cols-[auto_1fr] items-center gap-x-4 gap-y-1.5 text-sm">
      <dt className="text-muted-foreground">Provisioning</dt>
      <dd>
        <Badge variant={PROVISIONING_BADGE[agent.provisioningState]}>
          {agent.provisioningState}
        </Badge>
      </dd>
      <dt className="text-muted-foreground">Runtime</dt>
      <dd>
        <Badge variant={RUNTIME_BADGE[agent.runtimeState]}>
          {agent.runtimeState}
        </Badge>
      </dd>
      <dt className="text-muted-foreground">Updated</dt>
      <dd>
        <time dateTime={agent.updatedAt}>{agent.updatedAt.slice(0, 10)}</time>
      </dd>
    </dl>
  );
}
