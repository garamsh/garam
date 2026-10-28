"use client";

import { ArrowRight, KeyRound } from "lucide-react";
import Link from "next/link";
import { useState, useTransition } from "react";

import {
  API_UNREACHABLE_TITLE,
  ApiUnreachableReason,
} from "@garam/design/components/api-unreachable";
import { Banner } from "@garam/design/components/banner";
import { CopyField } from "@/components/copy-field";
import { Refusal } from "@garam/design/components/refusal";
import { SecretReveal } from "@/components/secret-reveal";
import { StatusDot } from "@/components/status-dot";
import { Time } from "@/components/time";
import { Button } from "@garam/design/ui/button";
import {
  issueOperatorKey,
  type IssueOperatorKeyState,
} from "@/features/orgs/actions";
import type { OperatorKey } from "@/features/orgs";
import { useHydrated } from "@/hooks/use-hydrated";

import { RefusedRead } from "../../../../../../_components/refused-read";
import { WizardPage } from "../../../../../../_components/templates/wizard-page";
import { liveKeys } from "../../../_components/operator-status";
import {
  CodeBlock,
  ORDER,
  PLACEMENT_NOTES,
  TOKEN_URL_PLACEHOLDER,
  useRefreshWhileVisible,
  type ConnectStep,
} from "./connect-wizard";
import { secretSnippet, type Identity } from "./secret-snippet";

/** One of the Operator's two identities, as garam holds it. */
export type PairIdentity = {
  /** The identity's identifier, its route segment. */
  operator: string;
  name: string;
  /** Its client_id; absent where it was not read. */
  clientId: string | undefined;
  keys: OperatorKey[];
};

type Issued = Extract<IssueOperatorKeyState, { status: "issued" }>;

/** What a person calls each identity on this page. */
const NOUN: Record<Identity, string> = {
  manager: "cluster manager",
  control: "control service",
};

const ROLES: readonly Identity[] = ["manager", "control"];

/** The single setup's steps, each for two keys. */
const STEPS = [
  { id: "register", title: "Register", description: "Both identities." },
  { id: "issue", title: "Issue the keys", description: "Each shown once." },
  {
    id: "place",
    title: "Place the keys",
    description: "Each in its own Secret.",
  },
  { id: "verify", title: "Verify", description: "Both sign in." },
] as const;

/**
 * Connecting an Operator registered as a pair (ADR-0124): one setup for
 * its cluster manager and its control service, each issued its own key
 * in the same step, each key saved as its own file and placed in its
 * own Secret, and both seen to sign in. Only an identity holding no
 * live key is issued one, so a setup left between the two resumes by
 * issuing the missing key; rotating either is that identity's own
 * setup page. The private keys live in this component's state alone,
 * from the answers that issued them until the wizard unmounts.
 */
export function PairConnectWizard({
  org,
  name,
  identities,
  problem,
  initialStep,
  canIssue,
  operatorsHref,
}: {
  org: string;
  /** What the Operator is called: its cluster manager's name. */
  name: string;
  identities: Record<Identity, PairIdentity>;
  /** Why an identity or its keys were not read, if they were not. */
  problem: "unreachable" | "forbidden" | null;
  initialStep: Exclude<ConnectStep, "register">;
  /**
   * Whether a policy lets the caller issue a key to an identity holding
   * no live one, which is every key this setup issues.
   */
  canIssue: boolean;
  operatorsHref: string;
}) {
  const [issued, setIssued] = useState<Partial<Record<Identity, Issued>>>({});
  const [refusal, setRefusal] = useState<IssueOperatorKeyState | null>(null);
  const [kept, setKept] = useState<Identity[]>([]);
  const [step, setStep] =
    useState<Exclude<ConnectStep, "register">>(initialStep);
  const [pending, startTransition] = useTransition();
  const hydrated = useHydrated();

  const unkeyed = ROLES.filter(
    (role) =>
      issued[role] === undefined &&
      liveKeys(identities[role].keys).length === 0,
  );
  // Each identity's key the page waits on: the one issued in this
  // sitting, or the newest live one garam holds.
  const target = (role: Identity) => {
    const kid =
      issued[role]?.kid ?? liveKeys(identities[role].keys).at(-1)?.kid;
    return identities[role].keys.find((key) => key.kid === kid);
  };
  const signedIn = (role: Identity) => target(role)?.lastExchangedAt;
  const verified = ROLES.every((role) => signedIn(role) !== undefined);
  const revealing = ROLES.find(
    (role) => issued[role] !== undefined && !kept.includes(role),
  );

  useRefreshWhileVisible(step === "verify" && !verified);

  const issueKeys = () =>
    startTransition(async () => {
      setRefusal(null);
      // One write per identity; a refusal stops before the next, and a
      // retry issues only what is still missing.
      for (const role of unkeyed) {
        const form = new FormData();
        form.set("org", org);
        form.set("operator", identities[role].operator);
        form.set("rotation", "planned");
        const answer = await issueOperatorKey({ status: "idle" }, form);
        if (answer.status !== "issued") {
          startTransition(() => setRefusal(answer));
          return;
        }
        startTransition(() =>
          setIssued((current) => ({ ...current, [role]: answer })),
        );
      }
    });

  const index = ORDER.indexOf(step);
  const completed = ORDER.filter(
    (id, at) => at < index || (id === "verify" && verified),
  );
  const anyIssued = ROLES.some((role) => issued[role] !== undefined);
  const showSteps = problem === null || anyIssued;

  return (
    <WizardPage
      title={`Connect ${name}`}
      steps={STEPS}
      current={verified ? "verify" : step}
      completed={completed}
    >
      {problem === "unreachable" && (
        <Banner tone="warn" title={API_UNREACHABLE_TITLE}>
          <ApiUnreachableReason subject="this setup" />
        </Banner>
      )}
      {problem === "forbidden" && (
        <RefusedRead subject="this Operator's identities or their keys" />
      )}

      {showSteps && step === "issue" && (
        <div className="flex flex-col gap-5">
          <div className="flex flex-col gap-1.5">
            <h2 className="text-base">Issue the keys</h2>
            <p className="text-muted-foreground">
              The cluster manager and the control service each get a key of
              their own, saved as its own file. Each is shown once.
            </p>
          </div>
          <ul className="flex flex-col gap-2">
            {ROLES.map((role) => (
              <li
                key={role}
                className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2.5"
              >
                <span className="flex min-w-0 flex-col">
                  <span className="font-medium capitalize">{NOUN[role]}</span>
                  <span className="font-mono text-xs text-muted-foreground">
                    {identities[role].operator}
                  </span>
                </span>
                <span className="text-muted-foreground">
                  {unkeyed.includes(role) ? "No key yet" : "Has a key"}
                </span>
              </li>
            ))}
          </ul>
          {refusal?.status === "unconfigured" && (
            <Banner tone="info" title="This garam doesn't issue keys">
              <code>GARAM_MACHINE_TOKEN_URL</code> is not set. Ask whoever runs
              garam.
            </Banner>
          )}
          {refusal?.status === "unavailable" && (
            <Banner tone="warn" title="garam couldn't issue a key">
              Try again shortly.
            </Banner>
          )}
          {refusal?.status === "error" && <Refusal>{refusal.message}</Refusal>}
          <div>
            {unkeyed.length > 0 && !canIssue ? (
              <Banner
                tone="info"
                title="No policy lets you issue these identities a key"
              />
            ) : unkeyed.length > 0 ? (
              <Button
                type="button"
                disabled={!hydrated || pending}
                onClick={issueKeys}
              >
                <KeyRound />
                {pending
                  ? "Issuing…"
                  : unkeyed.length === 2
                    ? "Issue both keys"
                    : `Issue the ${NOUN[unkeyed[0]]}'s key`}
              </Button>
            ) : (
              <Button type="button" onClick={() => setStep("place")}>
                Place the keys
                <ArrowRight />
              </Button>
            )}
          </div>
        </div>
      )}

      {showSteps && step === "place" && (
        <div className="flex flex-col gap-6">
          <div className="flex flex-col gap-1.5">
            <h2 className="text-base">Place the keys</h2>
            <p className="text-muted-foreground">
              Only as the saved files: never on a command line, in an
              environment variable or in source control.
            </p>
          </div>
          {ROLES.map((role) => {
            const identity = identities[role];
            const answer = issued[role];
            const clientId = answer?.clientId ?? identity.clientId;
            return (
              <section
                key={role}
                aria-label={`The ${NOUN[role]}'s key`}
                className="flex flex-col gap-3"
              >
                <h3 className="font-medium capitalize">{NOUN[role]}</h3>
                {answer === undefined && (
                  <Banner tone="info" title="Its private key was shown once">
                    <Link
                      href={`${operatorsHref}/${identity.operator}/connect?rotate`}
                      className="font-medium underline underline-offset-4"
                    >
                      Issue a replacement
                    </Link>
                  </Banner>
                )}
                {clientId !== undefined && (
                  <CopyField label="Client ID" value={clientId} />
                )}
                <p className="text-muted-foreground">{PLACEMENT_NOTES[role]}</p>
                <CodeBlock
                  label={`the ${NOUN[role]} command`}
                  code={secretSnippet(role, {
                    keyFile: `./${identity.operator}.pem`,
                    clientId: clientId ?? "<client ID>",
                    tokenUrl: answer?.tokenUrl ?? TOKEN_URL_PLACEHOLDER,
                  })}
                />
              </section>
            );
          })}
          <div>
            <Button onClick={() => setStep("verify")}>
              I&apos;ve placed both keys
              <ArrowRight />
            </Button>
          </div>
        </div>
      )}

      {showSteps && step === "verify" && (
        <div className="flex flex-col gap-5">
          <h2 className="text-base">Verify</h2>
          {verified ? (
            <Banner tone="ok" title={`${name} is connected`}>
              Both identities have signed in.
            </Banner>
          ) : null}
          <ul className="flex flex-col gap-2">
            {ROLES.map((role) => {
              const at = signedIn(role);
              return (
                <li
                  key={role}
                  className="flex items-center justify-between gap-3 rounded-lg border px-3 py-2.5"
                >
                  <span className="font-medium capitalize">{NOUN[role]}</span>
                  {at !== undefined ? (
                    <span>
                      Signed in <Time value={at} />
                    </span>
                  ) : (
                    <span role="status">
                      <StatusDot
                        tone="info"
                        label="Waiting for its first sign-in"
                      />
                    </span>
                  )}
                </li>
              );
            })}
          </ul>
          <div className="flex flex-wrap gap-2">
            <Button asChild variant="outline">
              <Link
                href={`${operatorsHref}/${identities.manager.operator}?tab=keys`}
              >
                Go to {name}
                <ArrowRight />
              </Link>
            </Button>
            {!verified && (
              <Button variant="ghost" onClick={() => setStep("place")}>
                Back to placing the keys
              </Button>
            )}
          </div>
        </div>
      )}

      {revealing !== undefined && (
        <SecretReveal
          open
          title={`Save the ${NOUN[revealing]}'s private key`}
          description="It is shown only now."
          secret={issued[revealing]?.privateKeyPem ?? ""}
          noun="private key"
          filename={`${identities[revealing].operator}.pem`}
          onDone={() => {
            const next = [...kept, revealing];
            setKept(next);
            if (
              ROLES.every(
                (role) => issued[role] === undefined || next.includes(role),
              ) &&
              unkeyed.length === 0
            )
              setStep("place");
          }}
        />
      )}
    </WizardPage>
  );
}
