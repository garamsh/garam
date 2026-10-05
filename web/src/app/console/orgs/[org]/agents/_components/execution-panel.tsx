"use client";

import type { HostedOperator } from "@/features/orgs";
import { ExecutionParser, segment } from "@/lib/operation-authority";

import { ControlNotice, UNAVAILABLE, controlBlocker } from "./control-notice";
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

  if (blocker !== null || execution === null) {
    return <ControlNotice>{blocker}</ControlNotice>;
  }
  if (execution.status === "loading") {
    return <ControlNotice>Reading the agent&apos;s execution…</ControlNotice>;
  }
  if (execution.status !== "ok") {
    return (
      <ControlNotice>
        {execution.status === "unavailable" ? UNAVAILABLE : execution.message}
      </ControlNotice>
    );
  }
  const { desired, rendered, effective } = execution.data;
  return (
    <dl className="grid max-w-2xl grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
      <dt className="text-muted-foreground">Desired</dt>
      <dd>
        Revision {desired.revision}
        {desired.pending ? " (pending)" : ""}
      </dd>
      <dt className="text-muted-foreground">Rendered</dt>
      <dd>
        {rendered === null
          ? "Not rendered yet"
          : `Revision ${rendered.revision}`}
      </dd>
      <dt className="text-muted-foreground">Effective</dt>
      <dd>
        {effective === null ? (
          "No runtime report"
        ) : (
          <>
            Revision {effective.revision}, generation{" "}
            <span className="font-mono">{effective.generation}</span>, reported{" "}
            <time dateTime={effective.observedAt}>
              {effective.observedAt.slice(0, 16).replace("T", " ")} UTC
            </time>
          </>
        )}
      </dd>
    </dl>
  );
}
