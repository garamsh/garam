"use client";

import { CopyId } from "@/components/copy-id";
import { DescriptionList } from "@/components/description-list";
import { Time } from "@/components/time";
import type { HostedOperator } from "@/features/orgs";
import { ExecutionParser, segment } from "@/lib/operation-authority";

import {
  ControlNotice,
  NO_FIX,
  UNAVAILABLE,
  controlBlocker,
} from "./control-notice";
import { useControlRead } from "./use-control-read";

/**
 * An agent's execution as its hosted operator's control service holds
 * it (#1170): the revision asked for and whether it is still pending,
 * the revision its controller rendered, and what the running agent
 * reported. Effective configuration is only ever the runtime's report;
 * the panel never infers it from the other two. Reading it needs
 * `agent:execution-read`, which reading the agent does not grant.
 */
export function ExecutionPanel({
  org,
  agentGrn,
  hosted,
}: {
  org: string;
  agentGrn: string;
  hosted: HostedOperator | null;
}) {
  const blocker = controlBlocker(hosted, "agent:execution-read");
  const execution = useControlRead(
    org,
    "agent:execution-read",
    hosted !== null && blocker === null
      ? {
          endpoint: hosted.endpoint,
          audience: hosted.grn,
          path: `/v1/orgs/${segment(org)}/agents/${segment(agentGrn)}/execution`,
          target: agentGrn,
        }
      : null,
    ExecutionParser,
  );

  if (blocker !== null) return <ControlNotice org={org} {...blocker} />;
  if (execution === null || execution.status === "loading") {
    return (
      <p role="status" className="text-muted-foreground">
        Reading the agent&apos;s execution…
      </p>
    );
  }
  if (execution.status !== "ok") {
    return (
      <ControlNotice
        org={org}
        reason={
          execution.status === "unavailable" ? UNAVAILABLE : execution.message
        }
        fix={NO_FIX}
      />
    );
  }
  const { desired, rendered, effective } = execution.data;
  return (
    <DescriptionList
      items={[
        {
          term: "Desired",
          description: `Revision ${desired.revision}${desired.pending ? " (pending)" : ""}`,
        },
        {
          term: "Rendered",
          description:
            rendered === null
              ? "Not rendered yet"
              : `Revision ${rendered.revision}`,
        },
        {
          term: "Effective",
          description:
            effective === null ? (
              "No runtime report"
            ) : (
              <>
                Revision {effective.revision}, reported{" "}
                <Time value={effective.observedAt} />
              </>
            ),
        },
        // The generation is an identifier, so it is offered to copy
        // under a term of its own rather than read inside a sentence.
        ...(effective === null
          ? []
          : [
              {
                term: "Generation",
                description: (
                  <CopyId
                    value={effective.generation}
                    label="the running agent's generation"
                  />
                ),
              },
            ]),
      ]}
    />
  );
}
