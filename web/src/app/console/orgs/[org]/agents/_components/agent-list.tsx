import { Badge } from "@/components/ui/badge";
import { Card, CardTitle } from "@/components/ui/card";
import { slugFromGrn, type Agent } from "@/features/orgs";

/** How a state is drawn, out of what the badge offers. */
type BadgeVariant = React.ComponentProps<typeof Badge>["variant"];

/**
 * What the operator reports about the pod, and the marking each state
 * takes. Only a state that needs attention is drawn loudly, so a column
 * of agents is read by its markings rather than word by word; `pending`
 * is the quietest, being an agent no operator has reported on yet.
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
 * The organization's agents: what each is identified by and the two
 * states its two parties report. Neither state substitutes for the
 * other (ADR-0053), so both are named and neither is summarized into a
 * single word. `Updated` is when a report last landed — nothing expires
 * one, so a state says nothing about how recently it was true.
 */
export function AgentList({ agents }: { agents: Agent[] }) {
  return (
    <ul className="flex flex-col gap-3">
      {agents.map((agent) => (
        <li key={agent.grn}>
          <Card className="gap-4 p-4">
            <CardTitle className="font-mono text-base">
              {slugFromGrn(agent.grn)}
            </CardTitle>
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
                <time dateTime={agent.updatedAt}>
                  {agent.updatedAt.slice(0, 10)}
                </time>
              </dd>
            </dl>
          </Card>
        </li>
      ))}
    </ul>
  );
}
