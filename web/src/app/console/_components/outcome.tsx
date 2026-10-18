"use client";

import { Check } from "lucide-react";
import { createContext, use, useState } from "react";

/** Where on a page a write's success is said. */
export type OutcomeArea = "lifecycle" | "document" | "versions" | "attachments";

type Reports = Partial<Record<OutcomeArea, string>>;

const OutcomeContext = createContext<{
  reports: Reports;
  report: (area: OutcomeArea, text: string) => void;
} | null>(null);

/**
 * Holds what the page's writes last reported, each by the area it is
 * said in. A confirmed write often removes the control that opened it
 * — a detached row, an Activate button on a policy now active — and a
 * tab that is not shown is unmounted, so the sentence lives here and
 * an `Outcome` at a place that stays says it.
 */
export function OutcomeProvider({ children }: { children: React.ReactNode }) {
  const [reports, setReports] = useState<Reports>({});
  const report = (area: OutcomeArea, text: string) =>
    setReports((current) => ({ ...current, [area]: text }));
  return (
    <OutcomeContext value={{ reports, report }}>{children}</OutcomeContext>
  );
}

/**
 * Reports a write's success to the area that says it, or null outside
 * an `OutcomeProvider`, where the write says it beside its control.
 */
export function useReportOutcome():
  ((area: OutcomeArea, text: string) => void) | null {
  return use(OutcomeContext)?.report ?? null;
}

/** What the last write reported in `area`, said politely. */
export function Outcome({ area }: { area: OutcomeArea }) {
  const text = use(OutcomeContext)?.reports[area];
  return (
    <p
      role="status"
      className="flex items-center gap-1.5 text-sm text-muted-foreground empty:hidden"
    >
      {text !== undefined && (
        <>
          <Check aria-hidden className="size-4 shrink-0 text-ok" />
          {text}
        </>
      )}
    </p>
  );
}
