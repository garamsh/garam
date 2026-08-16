import { Building2, FileStack, Fingerprint, FolderTree } from "lucide-react";
import Link from "next/link";

import { Button } from "@/components/ui/button";

import { PolicyPanel } from "./_components/policy-panel";
import { SectionHeading } from "./_components/section-heading";

/**
 * Every claim on this page has to exist in the product: organizations
 * and invitations, groups and projects, versioned policy documents,
 * GRNs, and Google sign-in.
 */
const CAPABILITIES = [
  {
    icon: Building2,
    title: "Organizations and invitations",
    body: "An organization grows by invitation, never by open signup. A single-use, expiring token admits a user with a fixed org role — owner, admin, or member.",
  },
  {
    icon: FolderTree,
    title: "Groups and projects",
    body: "Groups bundle people; projects hold a unit of work. Both live under one organization, carry their own members, and can hold roles scoped to that resource alone.",
  },
  {
    icon: FileStack,
    title: "Policies with versions",
    body: "Policy documents are stored per organization and versioned append-only. A stored version is never rewritten: activation, archival, and rollback move a pointer.",
  },
  {
    icon: Fingerprint,
    title: "One identifier system",
    body: "Organizations, groups, projects, policies, and users all carry a GRN, so one name shape answers who a subject is and what a policy is written about.",
  },
];

const DECISION_STEPS = [
  {
    title: "A matching Deny",
    body: "Any Deny statement whose action and resource patterns both match ends the decision.",
  },
  {
    title: "A matching Allow",
    body: "Otherwise an Allow from either origin — a role the api server collected, or an active policy the engine holds — admits the request.",
  },
  {
    title: "Default Deny",
    body: "With nothing matched, the request is denied.",
  },
];

const POLICY_STATES = [
  { state: "draft", body: "Stored, evaluated by nothing." },
  {
    state: "active",
    body: "Contributes its default version's statements to checks.",
  },
  { state: "archived", body: "Retained, no longer evaluated." },
];

const GRN_SEGMENTS = [
  { token: "grn", label: "prefix" },
  { token: "acme", label: "organization" },
  { token: "billing", label: "project" },
  { token: "policy", label: "service type" },
  { token: "org-read", label: "identifier" },
];

/** The public front door; everything on it is readable signed out. */
export default function LandingPage() {
  return (
    <>
      <section className="relative overflow-hidden border-b border-border/60">
        <div
          aria-hidden
          className="ruled-plane pointer-events-none absolute inset-0"
        />
        <div className="relative mx-auto grid w-full max-w-shell gap-14 px-6 pt-20 pb-section lg:grid-cols-2 lg:items-center lg:gap-16 lg:pt-28">
          <div className="flex flex-col items-start gap-6">
            <span className="inline-flex items-center gap-2 rounded-full border border-border bg-background/60 px-3 py-1 text-eyebrow font-medium text-muted-foreground uppercase">
              <span aria-hidden className="size-1.5 rounded-full bg-brand" />
              Identity, organization, authorization
            </span>
            <h1 className="font-heading text-display font-semibold text-balance">
              Who belongs to your organization, and what they may do.
            </h1>
            <p className="max-w-xl text-lead text-pretty text-muted-foreground">
              garam keeps the organizations, groups, and projects work is filed
              under, the people who belong to them, and the versioned policy
              documents that decide what each of them may do.
            </p>
            <div className="flex flex-wrap items-center gap-2 pt-2">
              <Button asChild size="lg">
                <Link href="/console">Open the console</Link>
              </Button>
              <Button asChild size="lg" variant="outline">
                <Link href="/login">Sign in</Link>
              </Button>
            </div>
            <p className="text-sm text-muted-foreground">
              Sign in with the Google account your organization invited.
            </p>
          </div>
          <PolicyPanel />
        </div>
      </section>

      <section id="platform" className="scroll-mt-20 border-b border-border/60">
        <div className="mx-auto w-full max-w-shell px-6 py-section">
          <SectionHeading
            eyebrow="Platform"
            title="What an organization is made of"
            body="The structure people work inside, and the rules that decide what they reach."
          />
          <ul className="mt-12 grid border-t border-l border-border sm:grid-cols-2">
            {CAPABILITIES.map(({ icon: Icon, title, body }) => (
              <li
                key={title}
                className="flex flex-col gap-3 border-r border-b border-border p-8"
              >
                <Icon aria-hidden className="size-5 text-brand" />
                <h3 className="font-heading text-base font-medium">{title}</h3>
                <p className="text-sm text-pretty text-muted-foreground">
                  {body}
                </p>
              </li>
            ))}
          </ul>
        </div>
      </section>

      <section
        id="authorization"
        className="scroll-mt-20 border-b border-border/60 bg-muted/30"
      >
        <div className="mx-auto w-full max-w-shell px-6 py-section">
          <SectionHeading
            eyebrow="Authorization"
            title="Every request ends in a decision"
            body="Statements reach the engine from two origins: the roles the api server collects for the request, and the active policies the engine holds for the organization. Deny overrides across both."
          />
          <div className="mt-12 grid gap-12 md:grid-cols-2 md:gap-16">
            <ol className="flex flex-col">
              {DECISION_STEPS.map((step, index) => (
                <li
                  key={step.title}
                  className="flex gap-4 border-t border-border py-5 first:border-t-0 first:pt-0"
                >
                  <span
                    aria-hidden
                    className="mt-1 font-mono text-xs text-brand"
                  >
                    {index + 1}
                  </span>
                  <div className="flex flex-col gap-1">
                    <h3 className="font-heading text-base font-medium">
                      {step.title}
                    </h3>
                    <p className="text-sm text-pretty text-muted-foreground">
                      {step.body}
                    </p>
                  </div>
                </li>
              ))}
            </ol>
            <div className="flex flex-col gap-4">
              <h3 className="font-heading text-base font-medium">
                A policy&rsquo;s life
              </h3>
              <dl className="flex flex-col rounded-xl border border-border bg-background">
                {POLICY_STATES.map(({ state, body }) => (
                  <div
                    key={state}
                    className="flex flex-col gap-1 border-t border-border px-5 py-4 first:border-t-0 sm:flex-row sm:items-baseline sm:gap-4"
                  >
                    <dt className="font-mono text-xs sm:w-20">{state}</dt>
                    <dd className="text-sm text-muted-foreground">{body}</dd>
                  </div>
                ))}
              </dl>
              <p className="text-sm text-pretty text-muted-foreground">
                Versions are append-only, enforced by the schema itself, and
                disappear only with the policy that owns them.
              </p>
            </div>
          </div>
        </div>
      </section>

      <section
        id="identifiers"
        className="scroll-mt-20 border-b border-border/60"
      >
        <div className="mx-auto w-full max-w-shell px-6 py-section">
          <SectionHeading
            eyebrow="Identifiers"
            title="One name shape, five segments"
            body="A GRN names every resource in the system. Policy resource patterns match those names with wildcards, so grn:acme:* scopes a statement to one organization."
          />
          <ul className="mt-12 grid gap-px overflow-hidden rounded-xl border border-border bg-border sm:grid-cols-5">
            {GRN_SEGMENTS.map(({ token, label }) => (
              <li key={label} className="flex flex-col gap-1 bg-background p-5">
                <span className="font-mono text-sm">{token}</span>
                <span className="text-eyebrow font-medium text-muted-foreground uppercase">
                  {label}
                </span>
              </li>
            ))}
          </ul>
          <p className="mt-6 text-sm text-muted-foreground">
            API paths address organizations, groups, projects, and policies by
            the last segment.
          </p>
        </div>
      </section>

      <section className="mx-auto w-full max-w-shell px-6 py-section">
        <div className="flex flex-col items-center gap-5 rounded-2xl border border-border px-6 py-16 text-center">
          <h2 className="font-heading text-title font-semibold">
            In the console today
          </h2>
          <p className="max-w-xl text-lead text-pretty text-muted-foreground">
            The organizations you belong to, and for each one its groups,
            projects, and members with their roles.
          </p>
          <Button asChild size="lg" className="mt-1">
            <Link href="/console">Open the console</Link>
          </Button>
        </div>
      </section>
    </>
  );
}
