"use client";

import { useId, useState } from "react";

import { Input } from "@garam/design/ui/input";
import { Label } from "@garam/design/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@garam/design/ui/select";
import { Textarea } from "@garam/design/ui/textarea";
import {
  approveAgentEnrollment,
  type ApproveAgentEnrollmentState,
} from "@/features/orgs/actions";

import { ActionDialog } from "./action-dialog";

const initialState: ApproveAgentEnrollmentState = { status: "idle" };

/** How long an approval stays spendable; garam allows a day at most. */
const LIFETIMES = [
  { hours: "1", label: "1 hour" },
  { hours: "4", label: "4 hours" },
  { hours: "12", label: "12 hours" },
  { hours: "24", label: "24 hours" },
];

/** The assignment a replacement replaces, exactly as the page read it. */
export type Replaces = {
  agentGrn: string;
  assigneeGrn: string;
  epoch: number;
  /** What the page calls the agent and the host holding it. */
  agentName: string;
  hostName: string;
};

/**
 * Approves a standalone or direct host from what `garam standalone
 * prepare` printed on it, pasted whole (ADR-0082, ADR-0111). With
 * `replaces`, the approval stands a new host up for an agent a
 * standalone host holds now, from exactly that assignment; garam
 * refuses it if the agent has moved since. The host already holds what
 * spending the approval needs, so nothing is shown once.
 */
export function ApproveHostDialog({
  org,
  replaces,
  trigger,
}: {
  org: string;
  replaces?: Replaces;
  trigger: React.ReactElement;
}) {
  const ids = {
    prepared: useId(),
    host: useId(),
    placement: useId(),
    life: useId(),
  };
  const [placement, setPlacement] = useState("standalone");
  const [hours, setHours] = useState("1");

  return (
    <ActionDialog<ApproveAgentEnrollmentState>
      trigger={trigger}
      title={
        replaces ? `Replace ${replaces.agentName}'s host?` : "Approve a host"
      }
      description={
        replaces ? (
          <p>
            {replaces.hostName} stops running {replaces.agentName} once the new
            host enrolls.
          </p>
        ) : undefined
      }
      action={approveAgentEnrollment}
      initialState={initialState}
      fields={{
        org,
        placement,
        hours,
        ...(replaces
          ? {
              replacesAgent: replaces.agentGrn,
              replacesAssignee: replaces.assigneeGrn,
              replacesEpoch: replaces.epoch,
            }
          : {}),
      }}
      confirmLabel={replaces ? "Approve replacement" : "Approve host"}
      pendingLabel="Approving…"
      done={(state) => state.status === "approved"}
      outcome={(state) =>
        state.status === "approved" ? `Approved ${state.hostName}` : null
      }
    >
      {() => (
        <div className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={ids.prepared}>
              What <code>garam standalone prepare</code> printed
            </Label>
            <Textarea
              id={ids.prepared}
              name="prepared"
              required
              rows={5}
              spellCheck={false}
              autoComplete="off"
              className="font-mono text-xs"
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={ids.host}>Host name</Label>
            <Input
              id={ids.host}
              name="hostName"
              required
              maxLength={100}
              autoComplete="off"
            />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor={ids.placement}>Runs its agent</Label>
              <Select value={placement} onValueChange={setPlacement}>
                <SelectTrigger id={ids.placement} className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="standalone">
                    In containers it starts
                  </SelectItem>
                  <SelectItem value="direct">
                    As a runtime already running
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor={ids.life}>Approval lasts</Label>
              <Select value={hours} onValueChange={setHours}>
                <SelectTrigger id={ids.life} className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {LIFETIMES.map((life) => (
                    <SelectItem key={life.hours} value={life.hours}>
                      {life.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
        </div>
      )}
    </ActionDialog>
  );
}
