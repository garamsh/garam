"use client";

import { HardDrive } from "lucide-react";
import Link from "next/link";

import { DataTable, type ColumnDef } from "@/components/data-table";
import { StatusDot, type StatusTone } from "@/components/status-dot";
import { Time } from "@/components/time";
import { Button } from "@/components/ui/button";
import {
  revokeAgentSupervisor,
  withdrawAgentEnrollment,
  type RevokeAgentSupervisorState,
  type WithdrawAgentEnrollmentState,
} from "@/features/orgs/actions";

import { ActionDialog } from "../../../../_components/action-dialog";
import { EmptyState } from "../../../../_components/empty-state";
import { InactiveButton } from "../../../../_components/inactive-button";

/** The agent an approval or a supervisor binds, as the page names it. */
export type HostAgent = { name: string; href: string };

/** One approval as its row shows it, decided on the server. */
export type ApprovalRow = {
  id: string;
  hostName: string;
  state: { tone: StatusTone; label: string };
  placement: string;
  agent: HostAgent;
  /** Set for a replacement: the host it replaces and at which epoch. */
  replaces: string | null;
  /** When it stops being spendable, or when it was spent or withdrawn. */
  when: { label: string; at: string };
  withdrawable: boolean;
};

/** One supervisor registration as its row shows it. */
export type SupervisorRow = {
  identifier: string;
  hostName: string;
  state: { tone: StatusTone; label: string };
  placement: string;
  agent: HostAgent;
  epoch: number;
  lastActivatedAt: string | null;
  revocable: boolean;
};

function AgentLink({ agent }: { agent: HostAgent }) {
  return (
    <Link
      href={agent.href}
      className="underline-offset-4 hover:underline focus-visible:underline"
    >
      {agent.name}
    </Link>
  );
}

function approvalColumns(
  org: string,
  denied: string | null,
): ColumnDef<ApprovalRow, unknown>[] {
  return [
    {
      id: "host",
      header: "Host",
      accessorFn: (row) => row.hostName,
      cell: ({ row: { original: row } }) => (
        <div className="flex flex-col gap-0.5">
          <span className="font-medium">{row.hostName}</span>
          <span className="text-xs text-muted-foreground">{row.placement}</span>
        </div>
      ),
    },
    {
      id: "state",
      header: "State",
      accessorFn: (row) => row.state.label,
      cell: ({ row: { original: row } }) => <StatusDot {...row.state} />,
    },
    {
      id: "agent",
      header: "Agent",
      accessorFn: (row) => row.agent.name,
      cell: ({ row: { original: row } }) => (
        <div className="flex flex-col gap-0.5">
          <AgentLink agent={row.agent} />
          {row.replaces && (
            <span className="text-xs text-muted-foreground">
              {row.replaces}
            </span>
          )}
        </div>
      ),
    },
    {
      id: "when",
      header: "When",
      accessorFn: (row) => row.when.at,
      cell: ({ row: { original: row } }) => (
        <span>
          {row.when.label} <Time value={row.when.at} />
        </span>
      ),
    },
    {
      id: "actions",
      header: () => <span className="sr-only">Actions</span>,
      enableSorting: false,
      enableGlobalFilter: false,
      cell: ({ row: { original: row } }) =>
        !row.withdrawable ? null : denied !== null ? (
          <InactiveButton reason={denied}>Withdraw</InactiveButton>
        ) : (
          <ActionDialog<WithdrawAgentEnrollmentState>
            trigger={
              <Button
                type="button"
                variant="outline"
                size="sm"
                aria-label={`Withdraw the approval for ${row.hostName}`}
              >
                Withdraw
              </Button>
            }
            title={`Withdraw the approval for ${row.hostName}?`}
            description={
              <p>
                The host can no longer enroll with it. Approving it again needs
                a fresh preparation on the host.
              </p>
            }
            action={withdrawAgentEnrollment}
            initialState={{ status: "idle" }}
            fields={{ org, enrollment: row.id }}
            confirmLabel="Withdraw approval"
            pendingLabel="Withdrawing…"
            destructive
            done={(state) => state.status === "done"}
          />
        ),
    },
  ];
}

function supervisorColumns(
  org: string,
  denied: string | null,
): ColumnDef<SupervisorRow, unknown>[] {
  return [
    {
      id: "host",
      header: "Host",
      accessorFn: (row) => row.hostName,
      cell: ({ row: { original: row } }) => (
        <div className="flex flex-col gap-0.5">
          <span className="font-medium">{row.hostName}</span>
          <span className="font-mono text-xs text-muted-foreground">
            {row.identifier}
          </span>
        </div>
      ),
    },
    {
      id: "state",
      header: "State",
      accessorFn: (row) => row.state.label,
      cell: ({ row: { original: row } }) => <StatusDot {...row.state} />,
    },
    {
      id: "agent",
      header: "Agent",
      accessorFn: (row) => row.agent.name,
      cell: ({ row: { original: row } }) => (
        <span>
          <AgentLink agent={row.agent} />
          <span className="text-muted-foreground">, epoch {row.epoch}</span>
        </span>
      ),
    },
    {
      id: "activated",
      header: "Last activated",
      accessorFn: (row) => row.lastActivatedAt ?? "",
      cell: ({ row: { original: row } }) =>
        row.lastActivatedAt ? (
          <Time value={row.lastActivatedAt} />
        ) : (
          <span className="text-muted-foreground">Never</span>
        ),
    },
    {
      id: "actions",
      header: () => <span className="sr-only">Actions</span>,
      enableSorting: false,
      enableGlobalFilter: false,
      cell: ({ row: { original: row } }) =>
        !row.revocable ? null : denied !== null ? (
          <InactiveButton reason={denied}>Revoke</InactiveButton>
        ) : (
          <ActionDialog<RevokeAgentSupervisorState>
            trigger={
              <Button
                type="button"
                variant="outline"
                size="sm"
                aria-label={`Revoke ${row.hostName}'s supervisor`}
              >
                Revoke
              </Button>
            }
            title={`Revoke ${row.hostName}'s supervisor?`}
            description={
              <p>
                {row.agent.name} stops running on {row.hostName}: its activation
                and every unspent approval for it end now. The agent, its
                identity and its memory stay, and only a new approval stands a
                host up for it again.
              </p>
            }
            action={revokeAgentSupervisor}
            initialState={{ status: "idle" }}
            fields={{ org, supervisor: row.identifier }}
            confirmLabel="Revoke supervisor"
            pendingLabel="Revoking…"
            destructive
            done={(state) => state.status === "done"}
          />
        ),
    },
  ];
}

/**
 * The organization's approvals and supervisors, each withdrawable or
 * revocable where that applies; `denied` is the reason both are
 * inactive, where the server denies `agent:enroll`.
 */
export function HostLists({
  org,
  approvals,
  supervisors,
  denied,
}: {
  org: string;
  approvals: ApprovalRow[] | null;
  supervisors: SupervisorRow[] | null;
  denied: string | null;
}) {
  return (
    <>
      {supervisors !== null && (
        <section aria-labelledby="supervisors" className="flex flex-col gap-3">
          <h2 id="supervisors" className="text-base">
            Supervisors
          </h2>
          <DataTable
            label="Supervisors"
            columns={supervisorColumns(org, denied)}
            data={supervisors}
            getRowId={(row) => row.identifier}
            empty={<EmptyState icon={HardDrive} title="No supervisors" />}
          />
        </section>
      )}
      {approvals !== null && (
        <section aria-labelledby="approvals" className="flex flex-col gap-3">
          <h2 id="approvals" className="text-base">
            Approvals
          </h2>
          <DataTable
            label="Approvals"
            columns={approvalColumns(org, denied)}
            data={approvals}
            getRowId={(row) => row.id}
            empty={<EmptyState icon={HardDrive} title="No approvals" />}
          />
        </section>
      )}
    </>
  );
}
