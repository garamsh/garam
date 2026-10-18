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
  if (holders === null) {
    return <p>Who holds it was not checked: garam didn&apos;t answer.</p>;
  }
  if (holders.status === "forbidden") {
    return <p>Who holds it isn&apos;t shown to you.</p>;
  }
  if (holders.holders.length === 0) {
    return (
      <p>
        {holders.more
          ? "No one among the first fifty members and agents holds it."
          : "No member or agent holds it."}
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
              <span className="text-muted-foreground">
                {holder.type === "user" ? "person" : "agent"}
              </span>
            </li>
          );
        })}
        {holders.more && (
          <li className="text-muted-foreground">
            and others past the first fifty asked
          </li>
        )}
      </ul>
    </div>
  );
}

/** What decided one action, read from the organization down. */
function Why({ reading }: { reading: Reading }) {
  const { explanation } = reading;
  if (explanation === null) {
    return <p>Why was not answered: garam didn&apos;t explain it.</p>;
  }
  switch (explanation.basis) {
    case "operation":
      return (
        <p>
          The operation decides this when you take it, past the policy engine,
          so it is offered and checked then.
        </p>
      );
    case "undefined-action":
      return <p>garam doesn&apos;t define this action, so no one holds it.</p>;
    case "statements": {
      const lines = grantLines(explanation.statements);
      if (lines.length === 0) {
        return <p>No role or policy grants it to you.</p>;
      }
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
  }
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

/**
 * Your access (ADR-0113): what the caller may do on this object, and
 * the grant behind each, asked of garam's policy engine rather than
 * inferred from a role. A denied action stays listed with its reason
 * and who holds it. An explanation that failed leaves the check's
 * answer standing, and a policy the caller cannot read is said as one,
 * with nothing else about it.
 */
export function YourAccess({ readings }: { readings: readonly Reading[] }) {
  return (
    <Section
      title="Your access"
      description="What you may do here and the grant behind each, asked of garam's policy engine, not inferred from your role."
      region
    >
      <ul className="flex flex-col divide-y rounded-xl border">
        {readings.map((reading) => {
          const status = STATUS[reading.permission];
          return (
            <li
              key={reading.action.label}
              className="flex flex-col gap-2 px-4 py-3"
            >
              <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                <span className="font-medium">{reading.action.label}</span>
                <span className="font-mono text-xs text-muted-foreground">
                  {reading.action.check.action}
                </span>
                <StatusDot {...status} className="sm:ml-auto" />
              </div>
              <div className="flex flex-col gap-2 text-muted-foreground">
                <Why reading={reading} />
                {reading.holders !== undefined && (
                  <HoldersLine holders={reading.holders} />
                )}
              </div>
            </li>
          );
        })}
      </ul>
    </Section>
  );
}
