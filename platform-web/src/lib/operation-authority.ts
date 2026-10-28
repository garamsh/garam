/**
 * The console's calls to a hosted operator's control service (#1170).
 * garam mints one operation authority per call, bound to the exact
 * request; the browser sends it to the operator's registered control
 * endpoint as `Authorization: Garam-Operation <authority>` and to no
 * other origin.
 *
 * The authority is held only for the call it was minted for: it is
 * never written to storage, a URL or a log line. The request URL is
 * built once and read twice — its `pathname + search` is the target the
 * authority binds, and the same object is what `fetch` sends — so no
 * normalization can differ between mint and call. A redirect is an
 * error, so no intermediary can resend the authority elsewhere.
 *
 * The control routes and their answers are the merged wire of
 * `garamsh/garam-agent-operator` at `dev` `8e1116933408`: create
 * (#244, `5e46800`), the reads and publication (#277, `5ef745a`); and
 * at `ba691a6`, the operators' reports (#364).
 */

import { z } from "zod";

/** The contract garam's operation-authority routes require. */
export const CONTRACT_VERSION = "operation-authority.v1";
export const CONTRACT_VERSION_HEADER = "garam-contract-version";

/** The SHA-256 of the empty body every read sends. */
export const EMPTY_BODY_SHA256 =
  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855";

/** The operations the console hands a control service. */
export type ControlOperation =
  | "agent:create"
  | "agent-template:read"
  | "agent-template:publish"
  | "execution-profile:read"
  | "agent:execution-read"
  | "operator:read";

/** What a mint needs, as the Server Action takes it. */
export type MintInput = {
  org: string;
  audience: string;
  operation: ControlOperation;
  target: string;
  requestId: string;
  bodySha256: string;
  requestTarget?: string;
};

/** What a mint answered: the authority, or the sentence garam gave. */
export type MintResult =
  | { status: "ok"; authority: string }
  | { status: "refused"; message: string }
  | { status: "unavailable" };

/**
 * Why a control call was refused: garam refused the mint, in the
 * sentence `refusal-copy.ts` wrote for it; the control service refused
 * the call, with its HTTP status and the `kind` it named, or null where
 * it named none; or it answered outside the agreed wire. The control
 * service's own `message` is not kept, so no consumer can show it.
 */
export type ControlRefusal =
  | { source: "mint"; sentence: string }
  | { source: "control"; status: number; kind: string | null }
  | { source: "unreadable" };

/** What a control call answered. */
export type ControlResult<T> =
  | { status: "ok"; data: T }
  | { status: "refused"; refusal: ControlRefusal }
  | { status: "unavailable" };

/** Lowercase hexadecimal SHA-256 of text's UTF-8 bytes. */
export async function sha256Hex(text: string): Promise<string> {
  const digest = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(text),
  );
  return Array.from(new Uint8Array(digest), (b) =>
    b.toString(16).padStart(2, "0"),
  ).join("");
}

/**
 * The `kind` a control refusal names, where it names one. The wire
 * omits an empty kind, so an empty one is read as none.
 */
const RefusalKindParser = z.object({
  kind: z
    .string()
    .optional()
    .transform((kind) => (kind === "" ? undefined : kind)),
});

/** Whether an operation's authority binds its request target. */
function bindsRequestTarget(operation: ControlOperation): boolean {
  return operation !== "agent:create";
}

/** One call to a control service, described before anything is minted. */
export type ControlCall<T> = {
  /** The hosted operator's registered control endpoint. */
  endpoint: string;
  /** The route under it, its segments already escaped. */
  path: string;
  method: "GET" | "POST";
  /** The JSON body a POST sends; a GET sends none. */
  body?: unknown;
  /** The request identifier the authority binds. */
  requestId: string;
  org: string;
  audience: string;
  operation: ControlOperation;
  target: string;
  parser: z.ZodType<T>;
};

/**
 * Mints an authority for call and sends it. A connection that failed,
 * a CORS refusal, a redirect and a 503 are one answer: the control
 * service is unavailable. So is a mint that threw rather than
 * answered, so no caller is left waiting on a call that never settles.
 */
export async function callControl<T>(
  call: ControlCall<T>,
  mint: (input: MintInput) => Promise<MintResult>,
): Promise<ControlResult<T>> {
  const url = new URL(call.endpoint + call.path);
  const body = call.body === undefined ? undefined : JSON.stringify(call.body);
  const minted: MintResult = await mint({
    org: call.org,
    audience: call.audience,
    operation: call.operation,
    target: call.target,
    requestId: call.requestId,
    bodySha256: body === undefined ? EMPTY_BODY_SHA256 : await sha256Hex(body),
    ...(bindsRequestTarget(call.operation)
      ? { requestTarget: url.pathname + url.search }
      : {}),
  }).catch(() => ({ status: "unavailable" as const }));
  if (minted.status === "unavailable") return minted;
  if (minted.status === "refused") {
    return {
      status: "refused",
      refusal: { source: "mint", sentence: minted.message },
    };
  }

  const response = await fetch(url, {
    method: call.method,
    headers: {
      authorization: `Garam-Operation ${minted.authority}`,
      ...(body === undefined ? {} : { "content-type": "application/json" }),
    },
    body,
    redirect: "error",
    credentials: "omit",
    cache: "no-store",
    referrerPolicy: "no-referrer",
  }).catch(() => null);
  if (response === null || response.status === 503) {
    return { status: "unavailable" };
  }
  const answer: unknown = await response.json().catch(() => null);
  if (!response.ok) {
    const named = RefusalKindParser.safeParse(answer);
    return {
      status: "refused",
      refusal: {
        source: "control",
        status: response.status,
        kind: named.success ? (named.data.kind ?? null) : null,
      },
    };
  }
  const parsed = call.parser.safeParse(answer);
  if (!parsed.success) {
    return { status: "refused", refusal: { source: "unreadable" } };
  }
  return { status: "ok", data: parsed.data };
}

/** A named, numbered version of a template or profile. */
export const VersionRefParser = z.object({
  name: z.string().min(1),
  version: z.number().int().positive(),
});
export type VersionRef = z.infer<typeof VersionRefParser>;

/**
 * What an agent runs with. A secret appears only as a reference to
 * where it is held, never as its value. The embedding is absent where
 * the model names none; which models need one the control service
 * decides.
 */
export const ConfigurationParser = z.object({
  model: z.object({
    provider: z.string(),
    baseUrl: z.string(),
    name: z.string(),
    apiKeyRef: z.string(),
    embedding: z
      .object({ baseUrl: z.string(), name: z.string(), apiKeyRef: z.string() })
      .optional(),
  }),
  ego: z.string(),
  tools: z.record(z.string(), z.string()),
});
export type Configuration = z.infer<typeof ConfigurationParser>;

/** The latest version of each published template. */
export const TemplatesParser = z.object({
  templates: z.array(VersionRefParser.extend({ profile: VersionRefParser })),
});
export type PublishedTemplate = z.infer<
  typeof TemplatesParser
>["templates"][number];

/** One immutable template version, in full. */
export const TemplateVersionParser = VersionRefParser.extend({
  profile: VersionRefParser,
  configuration: ConfigurationParser,
});
export type TemplateVersion = z.infer<typeof TemplateVersionParser>;

/** What publishing answered: the version it published. */
export const PublishedParser = VersionRefParser;

/** The published execution profiles. */
export const ProfilesParser = z.object({ profiles: z.array(VersionRefParser) });

/** What a create answered: the agent garam registered, revision 1. */
export const CreatedParser = z.object({
  agent: z.string(),
  revision: z.string(),
  epoch: z.string(),
});

/**
 * An agent's execution: the revision the organization asked for and
 * whether it is still pending, the revision the controller rendered,
 * and what the running agent itself reported. Effective is only ever a
 * runtime report, never inferred from the other two.
 */
export const ExecutionParser = z.object({
  desired: z.object({ revision: z.string(), pending: z.boolean() }),
  rendered: z.object({ revision: z.string() }).nullable(),
  effective: z
    .object({
      revision: z.string(),
      generation: z.string(),
      observedAt: z.string(),
    })
    .nullable(),
});
export type Execution = z.infer<typeof ExecutionParser>;

/** One image an operator reported running, by reference and digest. */
export const ReportedImageParser = z.object({
  name: z.string(),
  ref: z.string(),
  digest: z.string().optional(),
});
export type ReportedImage = z.infer<typeof ReportedImageParser>;

/**
 * What the control service holds of each operator of the organization
 * that reached it: the images it last reported, when it reported them,
 * and when it last made contact (garam-agent-operator ADR 0074). An
 * operator that never reached it is absent. An absent time never
 * happened; it is never now.
 */
export const OperatorReportsParser = z.object({
  operators: z.array(
    z.object({
      grn: z.string(),
      images: z.array(ReportedImageParser),
      reportedAt: z.string().optional(),
      lastContactAt: z.string().optional(),
    }),
  ),
});
export type OperatorReport = z.infer<
  typeof OperatorReportsParser
>["operators"][number];

/** An escaped path segment. */
export function segment(value: string | number): string {
  return encodeURIComponent(String(value));
}
