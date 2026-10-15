"use client";

import { createContext, useContext } from "react";

import {
  ControlNotice,
  NO_FIX,
  controlReach,
  type ControlBlock,
} from "@/components/control-notice";
import { Time } from "@/components/time";
import type { HostedOperator } from "@/features/orgs";
import { mintOperationAuthority } from "@/features/orgs/actions";
import { useControlRead } from "@/hooks/use-control-read";
import { controlFailureCopy } from "@/lib/control-refusal-copy";
import {
  OperatorReportsParser,
  segment,
  type OperatorReport,
} from "@/lib/operation-authority";

/**
 * Where the reports are read from: the organization, and the hosted
 * operator whose control service holds them with the GRN of the
 * organization the read targets, or null where none is hosted; or why
 * no hosted operator could be read.
 */
export type ReportsSource =
  | {
      status: "ready";
      org: string;
      hosted: { operator: HostedOperator; orgGrn: string } | null;
    }
  | { status: "unavailable"; reason: string };

/**
 * What the reports read answered: still reading, not checked and why,
 * or each reporting operator's entry by GRN.
 */
export type ReportsRead =
  | { status: "loading" }
  | { status: "unchecked"; block: ControlBlock }
  | { status: "ok"; byGrn: ReadonlyMap<string, OperatorReport> };

/**
 * Reads what the hosted operator's control service holds of the
 * organization's operators, under `operator:read` (#1047). A null
 * source, which a page passes where the server denies the caller the
 * read, asks nothing and answers null.
 */
export function useOperatorReports(
  source: ReportsSource | null,
): ReportsRead | null {
  const ready = source?.status === "ready" ? source : null;
  const reach = controlReach(ready?.hosted?.operator ?? null, "operator:read");
  const read = useControlRead(
    ready?.org ?? "",
    "operator:read",
    ready?.hosted && reach.status === "ready"
      ? {
          endpoint: reach.hosted.endpoint,
          audience: reach.hosted.grn,
          path: `/v1/orgs/${segment(ready.org)}/operators`,
          target: ready.hosted.orgGrn,
        }
      : null,
    OperatorReportsParser,
    mintOperationAuthority,
  );

  if (source === null) return null;
  if (source.status === "unavailable") {
    return {
      status: "unchecked",
      block: { reason: source.reason, fix: NO_FIX },
    };
  }
  if (reach.status === "blocked") {
    return { status: "unchecked", block: reach.block };
  }
  if (read === null || read.status === "loading") return { status: "loading" };
  if (read.status !== "ok") {
    return {
      status: "unchecked",
      block: { reason: controlFailureCopy(read, "operator:read"), fix: NO_FIX },
    };
  }
  return {
    status: "ok",
    byGrn: new Map(read.data.operators.map((report) => [report.grn, report])),
  };
}

/**
 * What one operator's report says: being read, not checked, never
 * reported, or the report. An operator the answer leaves out never
 * reached the control service, so it has reported nothing.
 */
export type OperatorReportState =
  | { status: "loading" }
  | { status: "unchecked" }
  | { status: "reported"; report: OperatorReport | null };

/** The state of `grn`'s report within what the read answered. */
export function reportOf(read: ReportsRead, grn: string): OperatorReportState {
  if (read.status !== "ok") return { status: read.status };
  return { status: "reported", report: read.byGrn.get(grn) ?? null };
}

/** Why the reports were not checked, where they were not. */
export function ReportsNotice({
  read,
  operatorsHref,
}: {
  read: ReportsRead;
  operatorsHref: string;
}) {
  if (read.status !== "unchecked") return null;
  return <ControlNotice operatorsHref={operatorsHref} {...read.block} />;
}

function Muted({ children }: { children: React.ReactNode }) {
  return <span className="text-muted-foreground">{children}</span>;
}

/**
 * The words for a report that is not there to show, or null where it
 * is: reading, not checked, or not reported.
 */
function absent(state: OperatorReportState, reported: boolean) {
  if (state.status === "loading") return <Muted>Checking…</Muted>;
  if (state.status === "unchecked") return <Muted>Not checked</Muted>;
  if (!reported) return <Muted>Not reported</Muted>;
  return null;
}

/**
 * The images an operator reported: each by name and reference, with
 * its digest where it gave one and `withDigest` asks for it, and when
 * it reported them. Never filled in where nothing was reported.
 */
export function ReportedImages({
  state,
  withDigest,
}: {
  state: OperatorReportState;
  withDigest: boolean;
}) {
  const report = state.status === "reported" ? state.report : null;
  const images = report?.images ?? [];
  const missing = absent(state, images.length > 0);
  if (missing !== null) return missing;
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <ul className="flex flex-col gap-1">
        {images.map((image) => (
          <li key={image.name} className="flex min-w-0 flex-col">
            <span className="font-mono text-xs wrap-anywhere">
              <span className="text-muted-foreground">{image.name}</span>{" "}
              <span>{image.ref}</span>
            </span>
            {withDigest && image.digest !== undefined && (
              <span className="font-mono text-xs wrap-anywhere text-muted-foreground">
                {image.digest}
              </span>
            )}
          </li>
        ))}
      </ul>
      {report?.reportedAt !== undefined && (
        <span className="text-xs text-muted-foreground">
          Reported <Time value={report.reportedAt} />
        </span>
      )}
    </div>
  );
}

/**
 * When an operator last made contact with the control service, or that
 * no contact is recorded.
 */
export function LastContact({ state }: { state: OperatorReportState }) {
  if (state.status !== "reported") return absent(state, true);
  const at = state.report?.lastContactAt;
  if (at === undefined) return <Muted>No contact recorded</Muted>;
  return <Time value={at} />;
}

const ReportsContext = createContext<ReportsRead | null>(null);

/**
 * Reads the reports once for everything inside it that shows one
 * operator's, so the facts of one page share a single read.
 */
export function OperatorReportsProvider({
  source,
  children,
}: {
  source: ReportsSource;
  children: React.ReactNode;
}) {
  const read = useOperatorReports(source);
  return (
    <ReportsContext.Provider value={read}>{children}</ReportsContext.Provider>
  );
}

function useProvidedReports(): ReportsRead {
  const read = useContext(ReportsContext);
  if (read === null) {
    throw new Error("an operator's report is read inside its provider");
  }
  return read;
}

/** `grn`'s reported images, from the provider above it. */
export function ProvidedImages({ grn }: { grn: string }) {
  return (
    <ReportedImages state={reportOf(useProvidedReports(), grn)} withDigest />
  );
}

/** `grn`'s last contact, from the provider above it. */
export function ProvidedLastContact({ grn }: { grn: string }) {
  return <LastContact state={reportOf(useProvidedReports(), grn)} />;
}

/** Why the provider's reports were not checked, where they were not. */
export function ProvidedReportsNotice({
  operatorsHref,
}: {
  operatorsHref: string;
}) {
  return (
    <ReportsNotice read={useProvidedReports()} operatorsHref={operatorsHref} />
  );
}
