"use client";

import { ArrowRight, Check, Copy, KeyRound, Plus } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useState, useTransition } from "react";

import {
  API_UNREACHABLE_TITLE,
  ApiUnreachableReason,
} from "@/components/api-unreachable";
import { Banner } from "@/components/banner";
import { CopyField } from "@/components/copy-field";
import { Refusal } from "@/components/refusal";
import { SecretReveal } from "@/components/secret-reveal";
import { StatusDot } from "@/components/status-dot";
import { Time } from "@/components/time";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  issueOperatorKey,
  type IssueOperatorKeyState,
} from "@/features/orgs/actions";
import type { OperatorKey } from "@/features/orgs";
import { useCopy } from "@/hooks/use-copy";
import { useHydrated } from "@/hooks/use-hydrated";

import { ActionDialog } from "../../../../../../_components/action-dialog";
import { RefusedRead } from "../../../../../../_components/refused-read";
import { WizardPage } from "../../../../../../_components/templates/wizard-page";
import { MAX_LIVE_KEYS, liveKeys } from "../../../_components/operator-status";
import { secretSnippet, type Identity } from "./secret-snippet";

/** The wizard's steps, in order. */
export type ConnectStep = "register" | "issue" | "place" | "verify";

export const STEPS = [
  { id: "register", title: "Register", description: "Its identity in garam." },
  { id: "issue", title: "Issue a key", description: "Shown once." },
  {
    id: "place",
    title: "Place the key",
    description: "In its Secret.",
  },
  { id: "verify", title: "Verify", description: "Its first sign-in." },
] as const;

export const ORDER: readonly ConnectStep[] = [
  "register",
  "issue",
  "place",
  "verify",
];

/** How often the verify step asks again while it is on screen. */
const POLL_MS = 5_000;

const initialState: IssueOperatorKeyState = { status: "idle" };

/**
 * Re-reads the page every `POLL_MS` while `active` and the tab is
 * visible, so a key's first sign-in reaches the verify step without a
 * reload. It stops when the step leaves or the tab is hidden.
 */
export function useRefreshWhileVisible(active: boolean) {
  const router = useRouter();
  useEffect(() => {
    if (!active) return;
    let timer: ReturnType<typeof setInterval> | undefined;
    const start = () => {
      if (timer === undefined)
        timer = setInterval(() => router.refresh(), POLL_MS);
    };
    const stop = () => {
      if (timer !== undefined) clearInterval(timer);
      timer = undefined;
    };
    const onVisibility = () =>
      document.visibilityState === "visible" ? start() : stop();
    onVisibility();
    document.addEventListener("visibilitychange", onVisibility);
    return () => {
      stop();
      document.removeEventListener("visibilitychange", onVisibility);
    };
  }, [active, router]);
}

/** A command to copy, in mono, with its copy control. */
export function CodeBlock({ label, code }: { label: string; code: string }) {
  const { outcome, copy } = useCopy(code);
  return (
    <div className="relative">
      <pre
        tabIndex={0}
        className="overflow-x-auto rounded-lg border bg-muted px-3 py-2.5 pr-11 font-mono text-xs leading-relaxed"
      >
        {code}
      </pre>
      <Button
        type="button"
        variant="ghost"
        size="icon-sm"
        aria-label={`Copy ${label}`}
        onClick={copy}
        className="absolute top-1.5 right-1.5"
      >
        {outcome === "copied" ? <Check className="text-ok" /> : <Copy />}
      </Button>
    </div>
  );
}

/**
 * Connecting one registered identity of an Operator (ADR-0102): register
 * it, issue its key, place the key in the Operator's Secret, and see its
 * first sign-in. The step it opens on comes from what garam holds, so a
 * person who leaves resumes where they were; the private key alone lives
 * in this component's state, from the answer that issued it until the
 * wizard unmounts, and nowhere else. A read that fails is shown here
 * rather than in the wizard's place for the same reason: the render
 * that follows issuing could otherwise unmount that one copy.
 */
export function ConnectWizard({
  org,
  operator,
  name,
  clientId,
  keys,
  problem,
  initialStep,
  canReplace,
  isControlService = false,
  operatorHref,
  operatorsHref,
}: {
  org: string;
  operator: string;
  name: string;
  /**
   * The operator's client_id, which its assertions name and its Secret
   * holds; absent where the operator was not read.
   */
  clientId: string | undefined;
  keys: OperatorKey[];
  /** Why the operator or its keys were not read, if they were not. */
  problem: "unreachable" | "forbidden" | null;
  initialStep: Exclude<ConnectStep, "register">;
  /** garam lets the caller issue a replacement (`operator:reissue`). */
  canReplace: boolean;
  /**
   * The identity is a control service, whose record names its manager
   * (ADR-0124), so there is no second identity left to connect.
   */
  isControlService?: boolean;
  operatorHref: string;
  operatorsHref: string;
}) {
  const [state, setState] = useState<IssueOperatorKeyState>(initialState);
  const [pending, startTransition] = useTransition();
  const formAction = (formData: FormData) =>
    startTransition(async () => {
      const next = await issueOperatorKey(state, formData);
      // State set after an await is its own transition (React 19).
      startTransition(() => setState(next));
    });
  const hydrated = useHydrated();
  const [step, setStep] =
    useState<Exclude<ConnectStep, "register">>(initialStep);
  const [kept, setKept] = useState<string | null>(null);
  const [chosenRotation, setRotation] = useState<"planned" | "immediate">(
    "planned",
  );

  const issued = state.status === "issued" ? state : null;
  const live = liveKeys(keys);
  // At the limit only replacing is a transition garam accepts, so it is
  // the one offered.
  const atLimit = live.length >= MAX_LIVE_KEYS;
  const rotation = atLimit ? "immediate" : chosenRotation;
  const latest = live.at(-1);
  const target = issued?.kid ?? latest?.kid;
  const targetKey = keys.find((key) => key.kid === target);
  const verifiedAt = step === "verify" ? targetKey?.lastExchangedAt : undefined;
  const verified = verifiedAt !== undefined;
  // A key revoked before its first sign-in never signs in, so the step
  // stops asking rather than waiting on it.
  const withdrawn = targetKey?.state === "revoked";

  useRefreshWhileVisible(step === "verify" && !verified && !withdrawn);

  const index = ORDER.indexOf(step);
  const completed = ORDER.filter(
    (id, at) => at < index || (id === "verify" && verified),
  );
  const keyFile = `./${operator}.pem`;
  const placeClientId = issued?.clientId ?? clientId;
  // A failed read leaves nothing to act on, unless a key was issued in
  // this sitting: it is still placed from what issuing answered.
  const showSteps = problem === null || issued !== null;

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
        <RefusedRead subject="this identity or its keys" />
      )}

      {showSteps && step === "issue" && (
        <div className="flex flex-col gap-5">
          <h2 className="text-base">Issue a key</h2>
          {state.status === "unconfigured" && (
            <Banner tone="info" title="This garam doesn't issue keys">
              <code>GARAM_MACHINE_TOKEN_URL</code> is not set. Ask whoever runs
              garam.
            </Banner>
          )}
          {state.status === "unavailable" && (
            <Banner tone="warn" title="garam couldn't issue a key">
              Try again shortly.
            </Banner>
          )}
          <form action={formAction} className="flex flex-col gap-5">
            <input type="hidden" name="org" value={org} />
            <input type="hidden" name="operator" value={operator} />
            {live.length > 0 ? (
              <fieldset className="flex flex-col gap-2">
                <legend className="mb-1 font-medium">
                  This identity already holds a key
                </legend>
                <label className="flex items-start gap-2.5 rounded-lg border px-3 py-2.5 has-checked:border-primary has-disabled:opacity-60">
                  <input
                    type="radio"
                    name="rotation"
                    value="planned"
                    checked={rotation === "planned"}
                    disabled={atLimit}
                    aria-describedby={atLimit ? "rotation-limit" : undefined}
                    onChange={() => setRotation("planned")}
                    className="mt-0.5 size-4 accent-primary"
                  />
                  <span className="flex flex-col gap-0.5">
                    <span className="font-medium">Rotate</span>
                    <span className="text-muted-foreground">
                      The current key keeps working for 24 hours.
                    </span>
                    {atLimit && (
                      <span id="rotation-limit" className="text-foreground">
                        Two keys are already live. Revoke one first, or replace
                        them now.
                      </span>
                    )}
                  </span>
                </label>
                <label className="flex items-start gap-2.5 rounded-lg border px-3 py-2.5 has-checked:border-primary">
                  <input
                    type="radio"
                    name="rotation"
                    value="immediate"
                    checked={rotation === "immediate"}
                    onChange={() => setRotation("immediate")}
                    className="mt-0.5 size-4 accent-primary"
                  />
                  <span className="flex flex-col gap-0.5">
                    <span className="font-medium">Replace now</span>
                    <span className="text-muted-foreground">
                      Every current key stops working at once.
                    </span>
                  </span>
                </label>
              </fieldset>
            ) : (
              <input type="hidden" name="rotation" value="planned" />
            )}
            {state.status === "error" && <Refusal>{state.message}</Refusal>}
            <div>
              {live.length > 0 && rotation === "immediate" ? (
                <ActionDialog
                  trigger={
                    <Button
                      type="button"
                      variant="destructive"
                      disabled={!hydrated || pending}
                    >
                      Replace key now
                    </Button>
                  }
                  title={`Replace ${name}'s key now?`}
                  description={
                    <p>Every current key and its tokens stop working.</p>
                  }
                  action={issueOperatorKey}
                  initialState={initialState}
                  fields={{ org, operator, rotation: "immediate" }}
                  confirmLabel="Replace key now"
                  pendingLabel="Replacing…"
                  destructive
                  done={(answer) => answer.status !== "error"}
                  // A refusal is said in the dialog; any other answer
                  // is the wizard's.
                  onAnswer={(answer) =>
                    answer.status !== "error" && setState(answer)
                  }
                />
              ) : (
                <Button type="submit" disabled={!hydrated || pending}>
                  <KeyRound />
                  {pending
                    ? "Issuing…"
                    : live.length > 0
                      ? "Issue the new key"
                      : "Issue key"}
                </Button>
              )}
            </div>
          </form>
        </div>
      )}

      {showSteps && step === "place" && placeClientId !== undefined && (
        <PlaceStep
          keyFile={keyFile}
          clientId={placeClientId}
          tokenUrl={issued?.tokenUrl}
          resumed={issued === null}
          replaceHref={canReplace ? "?rotate" : undefined}
          onPlaced={() => setStep("verify")}
        />
      )}

      {showSteps && step === "verify" && (
        <div className="flex flex-col gap-5">
          <h2 className="text-base">Verify</h2>
          {verifiedAt !== undefined ? (
            <Banner tone="ok" title={`${name} is connected`}>
              Signed in <Time value={verifiedAt} />.
            </Banner>
          ) : withdrawn ? (
            <Banner tone="warn" title="This key was revoked">
              Issue a new key to connect it.
            </Banner>
          ) : (
            <div role="status" className="flex items-center gap-2">
              <StatusDot tone="info" label="Waiting for its first sign-in" />
            </div>
          )}
          <div className="flex flex-wrap gap-2">
            {verified && !isControlService && (
              <Button asChild>
                <Link href={`${operatorsHref}?connect=${operator}`}>
                  <Plus />
                  Connect the control service too
                </Link>
              </Button>
            )}
            <Button asChild variant="outline">
              <Link href={`${operatorHref}?tab=keys`}>
                Go to {name}
                <ArrowRight />
              </Link>
            </Button>
            {withdrawn && canReplace && (
              <Button asChild variant="ghost">
                <Link href="?rotate">Issue a new key</Link>
              </Button>
            )}
            {!verified && !withdrawn && (
              <Button variant="ghost" onClick={() => setStep("place")}>
                Back to placing the key
              </Button>
            )}
          </div>
        </div>
      )}

      {issued && (
        <SecretReveal
          open={kept !== issued.kid}
          title="Save the private key"
          description="It is shown only now."
          secret={issued.privateKeyPem}
          noun="private key"
          filename={`${operator}.pem`}
          onDone={() => {
            setKept(issued.kid);
            setStep("place");
          }}
        />
      )}
    </WizardPage>
  );
}

/** What a snippet shows where the token URL is not at hand. */
export const TOKEN_URL_PLACEHOLDER =
  "<token URL shown when the key was issued>";

/** Where each identity mounts its Secret, under the snippet. */
export const PLACEMENT_NOTES: Record<Identity, React.ReactNode> = {
  manager: (
    <>
      Mounted read-only at{" "}
      <code className="font-mono text-xs">/etc/garam/credential</code>.
    </>
  ),
  control: (
    <>
      Mount it read-only and point{" "}
      <code className="font-mono text-xs">--operator-private-key-file</code>,{" "}
      <code className="font-mono text-xs">--operator-client-id-file</code> and{" "}
      <code className="font-mono text-xs">--operator-token-url-file</code> at
      its files.
    </>
  ),
};

/**
 * Where the key goes: the Secret each identity mounts read-only, from
 * garam-agent-operator `c730884` (ADR 0072). The private key goes in as
 * the file it was saved to; the client ID and token URL beside it.
 */
function PlaceStep({
  keyFile,
  clientId,
  tokenUrl,
  resumed,
  replaceHref,
  onPlaced,
}: {
  keyFile: string;
  clientId: string;
  tokenUrl: string | undefined;
  /** The key was issued earlier, so its private half is not at hand. */
  resumed: boolean;
  /** Where a replacement is issued; absent where garam denies it. */
  replaceHref: string | undefined;
  onPlaced: () => void;
}) {
  const shownTokenUrl = tokenUrl ?? TOKEN_URL_PLACEHOLDER;

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-1.5">
        <h2 className="text-base">Place the key</h2>
        <p className="text-muted-foreground">
          Only as the saved file: never on a command line, in an environment
          variable or in source control.
        </p>
      </div>
      {resumed && (
        <Banner tone="info" title="The private key was shown once">
          {replaceHref !== undefined && (
            <Link
              href={replaceHref}
              className="font-medium underline underline-offset-4"
            >
              Issue a replacement
            </Link>
          )}
        </Banner>
      )}
      <div className="grid gap-4 sm:grid-cols-2">
        <CopyField label="Client ID" value={clientId} />
        {tokenUrl ? (
          <CopyField label="Token URL" value={tokenUrl} />
        ) : (
          <div className="flex flex-col gap-1">
            <span className="text-muted-foreground">Token URL</span>
            <span>Shown when the key was issued</span>
          </div>
        )}
      </div>
      <Tabs defaultValue="manager" className="gap-4">
        <TabsList variant="line" className="w-full justify-start border-b">
          <TabsTrigger value="manager" className="flex-none px-2">
            Cluster manager
          </TabsTrigger>
          <TabsTrigger value="control" className="flex-none px-2">
            Control service
          </TabsTrigger>
        </TabsList>
        {(["manager", "control"] as const).map((identity) => (
          <TabsContent
            key={identity}
            value={identity}
            className="flex flex-col gap-3"
          >
            <p className="text-muted-foreground">{PLACEMENT_NOTES[identity]}</p>
            <CodeBlock
              label={`the ${identity === "manager" ? "cluster manager" : "control service"} command`}
              code={secretSnippet(identity, {
                keyFile,
                clientId,
                tokenUrl: shownTokenUrl,
              })}
            />
          </TabsContent>
        ))}
      </Tabs>
      <div>
        <Button onClick={onPlaced}>
          I&apos;ve placed the key
          <ArrowRight />
        </Button>
      </div>
    </div>
  );
}
