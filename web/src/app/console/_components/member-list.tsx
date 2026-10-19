"use client";

import { MoreHorizontal } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useId, useState } from "react";

import { CopyId } from "@/components/copy-id";
import { DataTable, type ColumnDef } from "@/components/data-table";
import { MemberMark } from "@/components/member-mark";
import { StatusDot, type StatusTone } from "@/components/status-dot";
import { Time } from "@/components/time";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  addMembersToTeam,
  attachPolicyToMembers,
  type AddMembersToTeamState,
  type AttachPolicyToMembersState,
} from "@/features/orgs/actions";

import { ActionDialog } from "./action-dialog";
import { InactiveButton } from "./inactive-button";

/** One person or agent as the list shows it, decided on the server. */
export type MemberRow = {
  kind: "person" | "agent";
  grn: string;
  identifier: string;
  name: string;
  href: string;
  /** A person's role, or an agent's one derived state. */
  status: { tone: StatusTone; label: string };
  /** What makes an agent's state so; none for a person. */
  detail: string | null;
  /** What holds an agent, keyed for the filter; none for a person. */
  runsOn: { key: string; label: string } | null;
  /** When an agent last reported; none for a person or before any. */
  lastSeen: string | null;
};

/** A value a filter or a dialog offers, and how it is named. */
export type Choice = { value: string; label: string };

/**
 * The filters as the URL holds them, and what each can be set to.
 * `runsOn` is null on the people list, which has no such filter.
 */
export type MemberFilters = {
  team: string;
  runs: string;
  teams: Choice[];
  runsOn: Choice[] | null;
};

/** The filter value meaning "no filter", since an option cannot be "". */
const ANY = "any";

function columns(
  kind: MemberRow["kind"],
  act: (dialog: "team" | "policy", rows: MemberRow[]) => void,
): ColumnDef<MemberRow, unknown>[] {
  const name: ColumnDef<MemberRow, unknown> = {
    id: "member",
    header: "Name",
    // The identifier is searched too: names repeat and it does not.
    accessorFn: (row) => `${row.name} ${row.identifier}`,
    cell: ({ row: { original: row } }) => (
      <div className="flex min-w-0 items-start gap-2.5">
        <MemberMark kind={row.kind} name={row.name} className="mt-0.5" />
        <div className="flex min-w-0 flex-col items-start gap-1">
          <Link
            href={row.href}
            className="font-medium wrap-anywhere underline-offset-4 hover:underline focus-visible:underline"
          >
            {row.name}
          </Link>
          <CopyId value={row.identifier} label={row.name} />
        </div>
      </div>
    ),
  };
  const status: ColumnDef<MemberRow, unknown> = {
    id: "status",
    header: kind === "person" ? "Role" : "Status",
    accessorFn: (row) => row.status.label,
    cell: ({ row: { original: row } }) => (
      <div className="flex flex-col gap-0.5">
        <StatusDot {...row.status} />
        {row.detail && (
          <span className="text-xs text-muted-foreground">{row.detail}</span>
        )}
      </div>
    ),
  };
  const agentColumns: ColumnDef<MemberRow, unknown>[] = [
    {
      id: "runs",
      header: "Runs on",
      accessorFn: (row) => row.runsOn?.label ?? "",
      cell: ({ row: { original: row } }) =>
        row.runsOn ? (
          row.runsOn.label
        ) : (
          <span className="text-muted-foreground">—</span>
        ),
    },
    {
      id: "seen",
      header: "Last report",
      accessorFn: (row) => row.lastSeen ?? "",
      cell: ({ row: { original: row } }) =>
        row.lastSeen ? (
          <Time value={row.lastSeen} />
        ) : (
          <span className="text-muted-foreground">Never</span>
        ),
    },
  ];
  const actions: ColumnDef<MemberRow, unknown> = {
    id: "actions",
    header: () => <span className="sr-only">Actions</span>,
    enableSorting: false,
    cell: ({ row: { original: row } }) => (
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            aria-label={`Actions for ${row.name}`}
          >
            <MoreHorizontal aria-hidden />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem asChild>
            <Link href={row.href}>Open</Link>
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => act("team", [row])}>
            Add to a team…
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => act("policy", [row])}>
            Attach a policy…
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    ),
  };
  return [name, status, ...(kind === "agent" ? agentColumns : []), actions];
}

/** "Scout", "Scout and Ada", "3 members". */
function whom(rows: readonly MemberRow[]): string {
  if (rows.length === 1) return rows[0].name;
  if (rows.length === 2) return `${rows[0].name} and ${rows[1].name}`;
  return `${rows.length} ${rows[0].kind === "person" ? "people" : "agents"}`;
}

/** A choice made inside a dialog, carried as a form field. */
function ChoiceField({
  name,
  label,
  choices,
}: {
  name: string;
  label: string;
  choices: Choice[];
}) {
  const id = useId();
  const [value, setValue] = useState("");
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <Select value={value} onValueChange={setValue}>
        <SelectTrigger id={id} className="w-full">
          <SelectValue placeholder={`Choose a ${name}`} />
        </SelectTrigger>
        <SelectContent>
          {choices.map((choice) => (
            <SelectItem key={choice.value} value={choice.value}>
              {choice.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <input
        type="hidden"
        name={name === "team" ? "group" : name}
        value={value}
      />
    </div>
  );
}

function MemberFields({ rows }: { rows: readonly MemberRow[] }) {
  return rows.map((row) => (
    <input key={row.grn} type="hidden" name="member" value={row.grn} />
  ));
}

/** Each member a bulk write could not take, by name, with the reason. */
function Refused({
  rows,
  refused,
}: {
  rows: readonly MemberRow[];
  refused: { member: string; message: string }[];
}) {
  if (refused.length === 0) return null;
  return (
    <ul role="alert" className="flex flex-col gap-1 text-danger">
      {refused.map((entry) => (
        <li key={entry.member}>
          {rows.find((row) => row.grn === entry.member)?.name ?? "A member"}:{" "}
          {entry.message}
        </li>
      ))}
    </ul>
  );
}

/**
 * The organization's people, or its agents, as a table (ADR-0116), with
 * filters kept in the URL so a filtered list can be shared, and the two
 * writes that act on members: adding them to a team and attaching a
 * policy, for the rows chosen or from one row's menu.
 */
export function MemberList({
  org,
  kind,
  rows,
  filters,
  teamChoices,
  policyChoices,
  empty,
}: {
  org: string;
  kind: MemberRow["kind"];
  rows: MemberRow[];
  filters: MemberFilters;
  /** The teams the server does not deny the caller adding to. */
  teamChoices: Choice[];
  /** The policies the server does not deny the caller attaching. */
  policyChoices: Choice[];
  empty: React.ReactNode;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const [dialog, setDialog] = useState<{
    kind: "team" | "policy";
    rows: MemberRow[];
  } | null>(null);

  function setFilter(name: "team" | "runs", value: string) {
    const next = new URLSearchParams();
    const current = { team: filters.team, runs: filters.runs };
    for (const [key, held] of Object.entries({ ...current, [name]: value })) {
      if (held && held !== ANY) next.set(key, held);
    }
    const query = next.toString();
    router.push(query ? `${pathname}?${query}` : pathname);
  }

  const bulk = (selected: MemberRow[], clear: () => void) => {
    const open = (kind: "team" | "policy") => {
      setDialog({ kind, rows: selected });
      clear();
    };
    return (
      <>
        {teamChoices.length === 0 ? (
          <InactiveButton reason="No team you may add to.">
            Add to team
          </InactiveButton>
        ) : (
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={selected.length === 0}
            onClick={() => open("team")}
          >
            Add to team
          </Button>
        )}
        {policyChoices.length === 0 ? (
          <InactiveButton reason="No policy you may attach.">
            Attach policy
          </InactiveButton>
        ) : (
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={selected.length === 0}
            onClick={() => open("policy")}
          >
            Attach policy
          </Button>
        )}
      </>
    );
  };

  const target = dialog?.rows ?? [];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-3">
        <FilterSelect
          label="Team"
          value={filters.team}
          choices={filters.teams}
          onChange={(value) => setFilter("team", value)}
        />
        {filters.runsOn && (
          <FilterSelect
            label="Runs on"
            value={filters.runs}
            choices={filters.runsOn}
            onChange={(value) => setFilter("runs", value)}
          />
        )}
      </div>
      <DataTable
        label={kind === "person" ? "People" : "Agents"}
        columns={columns(kind, (dialog, chosen) =>
          setDialog({ kind: dialog, rows: chosen }),
        )}
        data={rows}
        getRowId={(row) => row.grn}
        empty={empty}
        search={{ placeholder: "Filter by name or identifier" }}
        selection={{ name: (row) => `Select ${row.name}`, actions: bulk }}
      />
      <ActionDialog<AddMembersToTeamState>
        open={dialog?.kind === "team"}
        onOpenChange={(open) => !open && setDialog(null)}
        title={`Add ${whom(target)} to a team?`}
        description={<p>Each gains what the team&apos;s policies grant.</p>}
        action={addMembersToTeam}
        initialState={{ status: "idle" }}
        fields={{ org }}
        confirmLabel="Add to team"
        pendingLabel="Adding…"
        done={(state) => state.status === "done" && state.refused.length === 0}
        outcome={(state) =>
          state.status === "done"
            ? `Added ${state.added === 1 ? "1 member" : `${state.added} members`}`
            : null
        }
      >
        {(state) => (
          <>
            <ChoiceField name="team" label="Team" choices={teamChoices} />
            <MemberFields rows={target} />
            {state.status === "done" && (
              <Refused rows={target} refused={state.refused} />
            )}
          </>
        )}
      </ActionDialog>
      <ActionDialog<AttachPolicyToMembersState>
        open={dialog?.kind === "policy"}
        onOpenChange={(open) => !open && setDialog(null)}
        title={`Attach a policy to ${whom(target)}?`}
        description={
          <p>
            A policy attached to no one applies to everyone, so a first
            attachment narrows it.
          </p>
        }
        action={attachPolicyToMembers}
        initialState={{ status: "idle" }}
        fields={{ org }}
        confirmLabel="Attach policy"
        pendingLabel="Attaching…"
        done={(state) => state.status === "done" && state.refused.length === 0}
        outcome={(state) =>
          state.status === "done"
            ? `Attached to ${state.attached === 1 ? "1 member" : `${state.attached} members`}`
            : null
        }
      >
        {(state) => (
          <>
            <ChoiceField name="policy" label="Policy" choices={policyChoices} />
            <MemberFields rows={target} />
            {state.status === "done" && (
              <Refused rows={target} refused={state.refused} />
            )}
          </>
        )}
      </ActionDialog>
    </div>
  );
}

function FilterSelect({
  label,
  value,
  choices,
  onChange,
}: {
  label: string;
  value: string;
  choices: Choice[];
  onChange: (value: string) => void;
}) {
  const id = useId();
  return (
    <div className="flex items-center gap-2">
      <Label htmlFor={id} className="text-muted-foreground">
        {label}
      </Label>
      <Select value={value || ANY} onValueChange={onChange}>
        <SelectTrigger id={id} size="sm" className="min-w-36">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ANY}>Any</SelectItem>
          {choices.map((choice) => (
            <SelectItem key={choice.value} value={choice.value}>
              {choice.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
