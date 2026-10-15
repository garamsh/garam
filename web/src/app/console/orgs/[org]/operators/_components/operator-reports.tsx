"use client";

import { createContext, useContext, useEffect, useState } from "react";

import {
  ControlNotice,
  NO_FIX,
  type ControlBlock,
} from "@/components/control-notice";
import { Time } from "@/components/time";
import type { DelegatedOperator } from "@/features/orgs";
import { mintOperationAuthority } from "@/features/orgs/actions";
import { controlFailureCopy } from "@/lib/control-refusal-copy";
import {
  OperatorReportsParser,
  callControl,
  segment,
  type ControlResult,
  type OperatorReport,
} from "@/lib/operation-authority";

import { CopyableCode } from "./copyable-code";

/** A hosted operator whose grant carries `operator:read`. */
export type ReportService = { operator: DelegatedOperator; orgGrn: string };

/**
 * Where the reports are read from: every service whose grant carries
 * the read, the GRNs of the organization's control-service identities,
 * and why none can be asked where that is worth saying; or why the
 * hosted operators could not be read at all.
 */
export type ReportsSource =
  | {
      status: "ready";
      org: string;
      services: ReportService[];
      identities: string[];
      blocked: ControlBlock | null;
    }
  | { status: "unavailable"; reason: string };

type Reports = { operators: OperatorReport[] };

/** What one service answered, beside the grant it answered under. */
type ServiceAnswer = {
  service: ReportService;
  read: ControlResult<Reports>;
};

/**
 * What the reports read answered: still reading, or each service's
 * answer, the identities, and why any of it went unchecked.
 */
export type ReportsRead =
  | { status: "loading" }
  | {
      status: "read";
      answers: ServiceAnswer[];
      identities: ReadonlySet<string>;
      notices: ControlBlock[];
    };

/** The services a source asks, empty where it asks none. */
function servicesOf(source: ReportsSource | null): ReportService[] {
  return source?.status === "ready" ? source.services : [];
}

/** What a person reads where a service did not answer the read. */
function unansweredReason(service: ReportService): string {
  return `${service.operator.name}'s control service did not answer, so what it reports of the operators it serves was not checked.`;
}

/** Why one service's read went unchecked, or null where it answered. */
function noticeOf({ service, read }: ServiceAnswer): ControlBlock | null {
  if (read.status === "ok") return null;
  return {
    reason:
      read.status === "unavailable"
        ? unansweredReason(service)
        : controlFailureCopy(read, "operator:read"),
    fix: NO_FIX,
  };
}

/**
 * Reads what each service holds of the organization's operators, under
 * `operator:read` (#1047), each call under its own authority. A null
 * source, which a page passes where the server denies the caller the
 * read, asks nothing and answers null.
 */
export function useOperatorReports(
  source: ReportsSource | null,
): ReportsRead | null {
  const [answered, setAnswered] = useState<{
    source: ReportsSource;
    answers: ServiceAnswer[];
  } | null>(null);

  useEffect(() => {
    const services = servicesOf(source);
    if (source?.status !== "ready" || services.length === 0) return;
    const { org } = source;
    let live = true;
    void Promise.all(
      services.map(async (service) => ({
        service,
        read: await callControl(
          {
            endpoint: service.operator.endpoint,
            path: `/v1/orgs/${segment(org)}/operators`,
            method: "GET",
            requestId: crypto.randomUUID(),
            org,
            audience: service.operator.grn,
            operation: "operator:read",
            target: service.orgGrn,
            parser: OperatorReportsParser,
          },
          mintOperationAuthority,
        ),
      })),
    ).then((answers) => {
      if (live) setAnswered({ source, answers });
    });
    return () => {
      live = false;
    };
  }, [source]);

  if (source === null) return null;
  if (source.status === "unavailable") {
    return {
      status: "read",
      answers: [],
      identities: new Set(),
      notices: [{ reason: source.reason, fix: NO_FIX }],
    };
  }
  const identities = new Set(source.identities);
  if (source.services.length === 0) {
    return {
      status: "read",
      answers: [],
      identities,
      notices: source.blocked === null ? [] : [source.blocked],
    };
  }
  if (answered?.source !== source) return { status: "loading" };
  return {
    status: "read",
    answers: answered.answers,
    identities,
    notices: answered.answers.flatMap((a) => noticeOf(a) ?? []),
  };
}

/**
 * What one operator's report says: being read; not checked, where no
 * service that answered serves it; a control service, which is no
 * controller to report on; or what the services that serve it hold,
 * null where it reported nothing.
 */
export type OperatorReportState =
  | { status: "loading" }
  | { status: "unchecked" }
  | { status: "control-service" }
  | { status: "reported"; report: OperatorReport | null };

/** When a report was last heard from, for choosing between two. */
function heardAt(report: OperatorReport): number {
  const at = report.lastContactAt ?? report.reportedAt;
  return at === undefined ? 0 : Date.parse(at) || 0;
}

/**
 * The state of `grn`'s report within what the read answered. A
 * service answers only for the controllers that reached it, so one
 * absent from every answer has reported nothing only where an
 * answering service's grant names it; where two answer for it, the
 * one heard from later stands.
 */
export function reportOf(read: ReportsRead, grn: string): OperatorReportState {
  if (read.status === "loading") return read;
  const answered = read.answers.flatMap(({ service, read: r }) =>
    r.status === "ok" ? [{ service, operators: r.data.operators }] : [],
  );
  const reports = answered.flatMap(({ operators }) =>
    operators.filter((report) => report.grn === grn),
  );
  if (reports.length > 0) {
    const latest = reports.reduce((a, b) => (heardAt(b) > heardAt(a) ? b : a));
    return { status: "reported", report: latest };
  }
  if (
    answered.some(({ service }) =>
      service.operator.delegation.controllers.includes(grn),
    )
  ) {
    return { status: "reported", report: null };
  }
  if (read.identities.has(grn)) return { status: "control-service" };
  return { status: "unchecked" };
}

/** Why the reports, or some of them, were not checked. */
export function ReportsNotice({
  read,
  operatorsHref,
}: {
  read: ReportsRead;
  operatorsHref: string;
}) {
  if (read.status !== "read" || read.notices.length === 0) return null;
  return (
    <>
      {read.notices.map((notice) => (
        <ControlNotice
          key={notice.reason}
          operatorsHref={operatorsHref}
          {...notice}
        />
      ))}
    </>
  );
}

function Muted({ children }: { children: React.ReactNode }) {
  return <span className="text-muted-foreground">{children}</span>;
}

/**
 * The words for a report that is not there to show, or null where it
 * is: reading, not checked, a control service, or not reported.
 */
function absent(state: OperatorReportState, reported: boolean) {
  if (state.status === "loading") return <Muted>Checking…</Muted>;
  if (state.status === "unchecked") return <Muted>Not checked</Muted>;
  if (state.status === "control-service") {
    return <Muted>A control service, not a controller</Muted>;
  }
  if (!reported) return <Muted>Not reported</Muted>;
  return null;
}

/** A digest as a table shows it: its algorithm and first 12 digits. */
function shortDigest(digest: string): string {
  const [algorithm, hex] = digest.split(":", 2);
  return hex === undefined ? digest : `${algorithm}:${hex.slice(0, 12)}…`;
}

/**
 * The images an operator reported: each by name and reference, with
 * its digest where it gave one and `withDigest` asks for it, and when
 * it reported them. Each reference and digest copies whole. Never
 * filled in where nothing was reported.
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
            <span className="flex min-w-0 items-center gap-1.5">
              <span className="font-mono text-xs text-muted-foreground">
                {image.name}
              </span>
              <CopyableCode
                value={image.ref}
                shown={image.ref}
                label={`the ${image.name} image's reference`}
              />
            </span>
            {withDigest && image.digest !== undefined && (
              <CopyableCode
                value={image.digest}
                shown={shortDigest(image.digest)}
                label={`the ${image.name} image's digest`}
              />
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
 * When an operator last made contact with a control service, or that
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
