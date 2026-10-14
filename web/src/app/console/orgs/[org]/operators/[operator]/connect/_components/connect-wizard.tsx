"use client";

import { ArrowRight, Check, Copy, KeyRound, Plus } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useActionState, useEffect, useState } from "react";

import { Banner } from "@/components/banner";
import { CopyField } from "@/components/copy-field";
import { SecretReveal } from "@/components/secret-reveal";
import { StatusDot } from "@/components/status-dot";
import { Time } from "@/components/time";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  issueOperatorKey,
  type IssueOperatorKeyState,
} from "@/features/orgs/actions";
import type { OperatorKey } from "@/features/orgs";
import { useCopy } from "@/hooks/use-copy";
import { useHydrated } from "@/hooks/use-hydrated";

import { WizardPage } from "../../../../../../_components/templates/wizard-page";
import { liveKeys } from "../../../_components/operator-status";
import { SECRET, secretSnippet, type Identity } from "./secret-snippet";

/** The wizard's steps, in order. */
export type ConnectStep = "register" | "issue" | "place" | "verify";

const STEPS = [
  { id: "register", title: "Register", description: "Its identity in garam." },
  { id: "issue", title: "Issue a key", description: "Shown once." },
  {
    id: "place",
    title: "Place the key",
    description: "In the Operator's Secret.",
  },
  { id: "verify", title: "Verify", description: "Its first sign-in." },
] as const;

const ORDER: readonly ConnectStep[] = ["register", "issue", "place", "verify"];

/** How often the verify step asks again while it is on screen. */
const POLL_MS = 5_000;

const initialState: IssueOperatorKeyState = { status: "idle" };

/**
 * Re-reads the page every `POLL_MS` while `active` and the tab is
 * visible, so a key's first sign-in reaches the verify step without a
 * reload. It stops when the step leaves or the tab is hidden.
 */
function useRefreshWhileVisible(active: boolean) {
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
function CodeBlock({ label, code }: { label: string; code: string }) {
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
 * in this component's state, from the answer that issued it until it is
 * placed, and nowhere else.
 */
export function ConnectWizard({
  org,
  operator,
  name,
  clientId,
  keys,
  initialStep,
  rotate,
  operatorHref,
  operatorsHref,
}: {
  org: string;
  operator: string;
  name: string;
  /** The operator's client_id, which its assertions name and its Secret holds. */
  clientId: string;
  keys: OperatorKey[];
  initialStep: Exclude<ConnectStep, "register">;
  /** A live key is being replaced rather than a first one issued. */
  rotate: boolean;
  operatorHref: string;
  operatorsHref: string;
}) {
  const [state, formAction, pending] = useActionState(
    issueOperatorKey,
    initialState,
  );
  const hydrated = useHydrated();
  const [step, setStep] =
    useState<Exclude<ConnectStep, "register">>(initialStep);
  const [kept, setKept] = useState<string | null>(null);
  const [rotation, setRotation] = useState<"planned" | "immediate">("planned");

  const issued = state.status === "issued" ? state : null;
  const live = liveKeys(keys);
  const latest = live.at(-1);
  const target = issued?.kid ?? latest?.kid;
  const exchanged = keys.find((key) => key.kid === target)?.lastExchangedAt;
  const verified = step === "verify" && exchanged !== undefined;

  useRefreshWhileVisible(step === "verify" && !verified);

  const index = ORDER.indexOf(step);
  const completed = ORDER.filter(
    (id, at) => at < index || (id === "verify" && verified),
  );
  const keyFile = `./${operator}.pem`;

  return (
    <WizardPage
      title={`Connect ${name}`}
      description="Each identity of an Operator — its manager and its control service — is connected on its own."
      steps={STEPS}
      current={verified ? "verify" : step}
      completed={completed}
    >
      {step === "issue" && (
        <div className="flex flex-col gap-5">
          <div className="flex flex-col gap-1.5">
            <h2 className="text-base">Issue a key</h2>
            <p className="text-muted-foreground">
              garam generates the key and shows its private half once. It keeps
              only the public half, so save the key when it appears.
            </p>
          </div>
          {state.status === "unavailable" && (
            <Banner tone="warn" title="garam couldn't issue a key">
              Either garam can&apos;t be reached right now, or it isn&apos;t
              configured to issue keys because its token endpoint isn&apos;t
              set. Try again in a moment; if it keeps happening, ask whoever
              runs garam.
            </Banner>
          )}
          <form action={formAction} className="flex flex-col gap-5">
            <input type="hidden" name="org" value={org} />
            <input type="hidden" name="operator" value={operator} />
            {live.length > 0 ? (
              <fieldset className="flex flex-col gap-2">
                <legend className="mb-1 font-medium">
                  This operator already holds a key
                </legend>
                <label className="flex items-start gap-2.5 rounded-lg border px-3 py-2.5 has-checked:border-primary">
                  <input
                    type="radio"
                    name="rotation"
                    value="planned"
                    checked={rotation === "planned"}
                    onChange={() => setRotation("planned")}
                    className="mt-0.5 size-4 accent-primary"
                  />
                  <span className="flex flex-col gap-0.5">
                    <span className="font-medium">Rotate</span>
                    <span className="text-muted-foreground">
                      Add the new key beside the current one, which keeps
                      working for 24 hours and then retires by itself.
                    </span>
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
                      For a key that was lost or exposed. Every current key, and
                      every token signed in with one, stops working at once.
                    </span>
                  </span>
                </label>
              </fieldset>
            ) : (
              <input type="hidden" name="rotation" value="planned" />
            )}
            {state.status === "error" && (
              <p role="alert" className="text-destructive">
                {state.message}
              </p>
            )}
            <div>
              {live.length > 0 && rotation === "immediate" ? (
                <AlertDialog>
                  <AlertDialogTrigger asChild>
                    <Button
                      type="button"
                      variant="destructive"
                      disabled={!hydrated || pending}
                    >
                      Replace key now
                    </Button>
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <form action={formAction} className="contents">
                      <input type="hidden" name="org" value={org} />
                      <input type="hidden" name="operator" value={operator} />
                      <input type="hidden" name="rotation" value="immediate" />
                      <AlertDialogHeader>
                        <AlertDialogTitle>
                          Replace {name}&apos;s key now?
                        </AlertDialogTitle>
                        <AlertDialogDescription>
                          Every current key, and every token signed in with one,
                          stops working the moment you confirm. The operator is
                          refused until the new key is placed.
                        </AlertDialogDescription>
                      </AlertDialogHeader>
                      <AlertDialogFooter>
                        <AlertDialogCancel>Cancel</AlertDialogCancel>
                        <AlertDialogAction type="submit" variant="destructive">
                          Replace key now
                        </AlertDialogAction>
                      </AlertDialogFooter>
                    </form>
                  </AlertDialogContent>
                </AlertDialog>
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
          {rotate && live.length === 0 && (
            <p className="text-muted-foreground">
              This operator holds no live key, so the new one is its only key.
            </p>
          )}
        </div>
      )}

      {step === "place" && (
        <PlaceStep
          keyFile={keyFile}
          clientId={issued?.clientId ?? clientId}
          tokenUrl={issued?.tokenUrl}
          resumed={issued === null}
          replaceHref={`?rotate`}
          onPlaced={() => setStep("verify")}
        />
      )}

      {step === "verify" && (
        <div className="flex flex-col gap-5">
          <div className="flex flex-col gap-1.5">
            <h2 className="text-base">Verify</h2>
            <p className="text-muted-foreground">
              The operator signs in to garam with the new key on its next
              exchange. This page checks every few seconds while it is open.
            </p>
          </div>
          {verified ? (
            <Banner tone="ok" title={`${name} is connected`}>
              It signed in with its key <Time value={exchanged!} />.
            </Banner>
          ) : (
            <div role="status" className="flex items-center gap-2">
              <StatusDot tone="info" label="Waiting for its first sign-in" />
            </div>
          )}
          <div className="flex flex-wrap gap-2">
            {verified && (
              <Button asChild>
                <Link href={`${operatorsHref}?connect`}>
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
            {!verified && (
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
          description="garam keeps only the public half. Download it or copy it now; it cannot be shown again."
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
  replaceHref: string;
  onPlaced: () => void;
}) {
  const shownTokenUrl = tokenUrl ?? "<token URL shown when the key was issued>";
  const notes: Record<Identity, React.ReactNode> = {
    manager: (
      <>
        This is the Secret the manager mounts read-only at{" "}
        <code className="font-mono text-xs">/etc/garam/credential</code>. It
        also holds <code className="font-mono text-xs">server-root.pem</code>,
        garam&apos;s server root, which the manager verifies garam against. A
        replaced Secret reaches the running Pod without a restart.
      </>
    ),
    control: (
      <>
        The control service ships no manifest of its own yet, so the
        Secret&apos;s name and namespace are yours. Mount it read-only and point{" "}
        <code className="font-mono text-xs">--operator-private-key-file</code>,{" "}
        <code className="font-mono text-xs">--operator-client-id-file</code> and{" "}
        <code className="font-mono text-xs">--operator-token-url-file</code> at
        its three files.
      </>
    ),
  };

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-1.5">
        <h2 className="text-base">Place the key</h2>
        <p className="text-muted-foreground">
          Put the key in the Secret of the identity you are connecting. The
          private key goes in only as the file you saved — never on a command
          line, in an environment variable or in source control.
        </p>
      </div>
      {resumed && (
        <Banner
          tone="info"
          title="The private key was shown when it was issued"
        >
          It cannot be shown again. If it was not saved, issue a replacement.{" "}
          <Link
            href={replaceHref}
            className="font-medium underline underline-offset-4"
          >
            Issue a replacement
          </Link>
        </Banner>
      )}
      <div className="grid gap-4 sm:grid-cols-2">
        <CopyField
          label="Client ID (for the Operator's Secret)"
          value={clientId}
        />
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
            Manager
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
            <p className="text-muted-foreground">{notes[identity]}</p>
            <CodeBlock
              label={`the ${identity === "manager" ? "manager" : "control service"} command`}
              code={secretSnippet(identity, {
                keyFile,
                clientId,
                tokenUrl: shownTokenUrl,
              })}
            />
            {!SECRET[identity].fixed && (
              <p className="text-muted-foreground">
                Replace the values in angle brackets with your own.
              </p>
            )}
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
