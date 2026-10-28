import { MemberMark } from "@/components/member-mark";
import { StatusDot } from "@/components/status-dot";
import { slugFromGrn } from "@/lib/grn";
import {
  checkPermissions,
  type Permission,
  type PermissionCheck,
} from "@/lib/permission-check";
import {
  explainPermission,
  listPermissionHolders,
  type PermissionExplanation,
} from "@/lib/permission-explanation";

import { grantLines } from "./access-reading";
import { Section } from "./section";

/**
 * One action a page offers, as the panel names and asks it. `key` is
 * the name the page reads its decision by, so the page and its panel
 * share one check.
 */
export type AccessAction = {
  key: string;
  label: string;
  check: PermissionCheck;
};

type Holders = Awaited<ReturnType<typeof listPermissionHolders>>;

/** What the panel read for one action. */
export type Reading = {
  action: AccessAction;
  /** The batched check's answer, which stands where nothing explains it. */
  permission: Permission;
  explanation: PermissionExplanation | null;
  holders: Holders | undefined;
};

/**
 * The panel's decisions by the keys the page gave its actions, read the
 * way a page reads `checkPermissions`: an action not asked is unknown.
 */
export function accessDecisions(readings: readonly Reading[]): {
  get(key: string): Permission;
} {
  return {
    get: (key) =>
      readings.find((reading) => reading.action.key === key)?.permission ??
      "unknown",
  };
}

/** Who else holds a denied action, in a sentence's worth of names. */
function HoldersLine({ holders }: { holders: Holders }) {
  if (holders === null || holders.status === "forbidden") {
    return <p>Holders not shown.</p>;
  }
  if (holders.holders.length === 0) {
    return (
      <p>
        {holders.more
          ? "No one in the first fifty holds it."
          : "No one holds it."}
      </p>
    );
  }
  return (
    <div className="flex flex-col gap-1.5">
      <p>Who can:</p>
      <ul className="flex flex-wrap gap-x-4 gap-y-1.5">
        {holders.holders.map((holder) => {
          const name =
            holder.type === "user"
              ? (holder.nickname ?? "A person with no name on record")
              : slugFromGrn(holder.grn);
          return (
            <li key={holder.grn} className="flex items-center gap-1.5">
              <MemberMark
                kind={holder.type === "user" ? "person" : "agent"}
                name={name}
                className="size-5"
              />
              <span className={holder.type === "agent" ? "font-mono" : ""}>
                {name}
              </span>
            </li>
          );
        })}
        {holders.more && <li className="text-muted-foreground">and more</li>}
      </ul>
    </div>
  );
}

/**
 * The grant behind an allowed action, read from the organization down.
 * A denied one names the permission it lacks; its reason is said once,
 * beside the inactive control, and not repeated here (ADR-0116).
 */
function Grant({ reading }: { reading: Reading }) {
  if (reading.permission === "denied") {
    return (
      <p>
        Missing{" "}
        <code className="font-mono text-xs">{reading.action.check.action}</code>
      </p>
    );
  }
  const { explanation } = reading;
  if (explanation?.basis !== "statements") return null;
  const lines = grantLines(explanation.statements);
  return (
    <ol className="flex flex-col gap-1">
      {lines.map((line, index) => (
        <li key={index}>
          {line.effect === "Deny" ? "Denied by " : ""}
          {line.text}
        </li>
      ))}
    </ol>
  );
}

const STATUS = {
  allowed: { tone: "ok", label: "Allowed" },
  denied: { tone: "danger", label: "Not allowed" },
  unknown: { tone: "neutral", label: "Decided when you act" },
} as const;

/**
 * Reads what Your access shows for the page's actions: one check for
 * all of them, then each explained, in parallel, and who holds an
 * action only where the engine denied it. A page awaits it beside its
 * own reads, so the panel adds no round trip after them.
 */
export async function readAccess(
  org: string,
  actions: readonly AccessAction[],
): Promise<Reading[]> {
  const [may, explanations] = await Promise.all([
    checkPermissions(
      org,
      Object.fromEntries(
        actions.map((action, index) => [String(index), action.check]),
      ),
    ),
    Promise.all(actions.map((action) => explainPermission(org, action.check))),
  ]);
  return Promise.all(
    actions.map(async (action, index) => {
      const explanation = explanations[index];
      const holders =
        explanation?.decision === "deny" && explanation.basis === "statements"
          ? await listPermissionHolders(org, action.check)
          : undefined;
      return {
        action,
        permission: may.get(String(index)),
        explanation,
        holders,
      };
    }),
  );
}

/** "A", "A and B", "A, B and C". */
function listed(labels: readonly string[]): string {
  return labels.length < 2
    ? labels.join("")
    : `${labels.slice(0, -1).join(", ")} and ${labels.at(-1)}`;
}

/**
 * Your access (ADR-0113): one line saying what the caller may not do
 * on this object, and behind an expander (ADR-0116) the grant behind
 * each action, asked of garam's policy engine rather than inferred from
 * a role, with who holds a denied one. An explanation that failed
 * leaves the check's answer standing.
 */
export function YourAccess({ readings }: { readings: readonly Reading[] }) {
  const denied = readings
    .filter((reading) => reading.permission === "denied")
    .map((reading) => reading.action.label.toLowerCase());
  return (
    <Section title="Your access" region>
      <p className="text-muted-foreground">
        {denied.length === 0
          ? "You can do everything."
          : `You can't ${listed(denied)}.`}
      </p>
      <details className="group rounded-xl border">
        <summary className="cursor-pointer rounded-xl px-4 py-2.5 font-medium outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
          Details
        </summary>
        <ul className="flex flex-col divide-y border-t">
          {readings.map((reading) => (
            <li
              key={reading.action.label}
              className="flex flex-col gap-2 px-4 py-3"
            >
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                <span className="font-medium">{reading.action.label}</span>
                <StatusDot
                  {...STATUS[reading.permission]}
                  className="sm:ml-auto"
                />
              </div>
              <div className="flex flex-col gap-2 text-muted-foreground">
                <Grant reading={reading} />
                {reading.holders !== undefined && (
                  <HoldersLine holders={reading.holders} />
                )}
              </div>
            </li>
          ))}
        </ul>
      </details>
    </Section>
  );
}
