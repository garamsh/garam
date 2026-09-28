"use client";

import { useActionState, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  reviseAgentValues,
  type ReviseAgentValuesState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";
import { parseAgentValues } from "@/lib/agent-definition";

import { rowsFrom, ValuesEditor } from "../../_components/values-editor";

const initialState: ReviseAgentValuesState = { status: "idle" };

/**
 * Edits the agent's settings and saves them as the next revision of
 * the one this form read (ADR-0076). What was typed survives a refusal,
 * and a save another person landed first is offered to load or to save
 * over — never taken over silently in either direction.
 */
export function AgentSettingsForm({
  org,
  agent,
  revision,
  values,
}: {
  org: string;
  agent: string;
  revision: number;
  values: Record<string, string>;
}) {
  const [state, formAction, pending] = useActionState(
    reviseAgentValues,
    initialState,
  );
  const hydrated = useHydrated();
  const [rows, setRows] = useState(() => rowsFrom(values));
  // The revision the next save is made against: the one read, then the
  // one each save produced, then the one a conflict chose to build on.
  const [expected, setExpected] = useState(revision);
  const [seen, setSeen] = useState(state);
  const [settled, setSettled] = useState<ReviseAgentValuesState | null>(null);
  if (state !== seen) {
    setSeen(state);
    if (state.status === "saved") setExpected(state.revision);
  }

  const invalid = parseAgentValues(rows);
  const problem = invalid.status === "error" ? invalid.message : null;
  const conflict =
    state.status === "conflict" && settled !== state ? state : null;
  const latest = conflict?.latest ?? null;
  const message = problem ?? (state.status === "error" ? state.message : null);

  return (
    <form action={formAction} className="flex max-w-2xl flex-col gap-4">
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="agent" value={agent} />
      <input type="hidden" name="expectedRevision" value={expected} />

      <ValuesEditor rows={rows} onChange={setRows} disabled={!hydrated} />

      {conflict && (
        <div
          role="alert"
          className="flex flex-col gap-3 rounded-md border border-destructive/50 p-3 text-sm"
        >
          {latest ? (
            <>
              <p>
                Someone saved revision {latest.revision} first. Your edits are
                still here.
              </p>
              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setRows(rowsFrom(latest.values));
                    setExpected(latest.revision);
                    setSettled(conflict);
                  }}
                >
                  Load revision {latest.revision}
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    setExpected(latest.revision);
                    setSettled(conflict);
                  }}
                >
                  Keep my edits
                </Button>
              </div>
            </>
          ) : (
            <p>
              Someone saved first. Your edits are still here; reload to see the
              saved settings.
            </p>
          )}
        </div>
      )}
      {message && (
        <p role="alert" className="text-sm text-destructive">
          {message}
        </p>
      )}
      {state.status === "saved" && (
        <p role="status" className="text-sm text-muted-foreground">
          Saved as revision {state.revision}.
        </p>
      )}

      <div>
        <Button
          type="submit"
          disabled={
            !hydrated || pending || problem !== null || conflict !== null
          }
        >
          {pending ? "Saving…" : "Save settings"}
        </Button>
      </div>
    </form>
  );
}
