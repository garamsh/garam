import { Building2, FileStack, Fingerprint, FolderTree } from "lucide-react";
import type { Metadata } from "next";
import Link from "next/link";

import { Button } from "@/components/ui/button";

import { PolicyAttachments } from "./_components/policy-attachments";
import { SectionHeading } from "./_components/section-heading";

/**
 * Every claim on this page has to exist in the product: organizations
 * and invitations, groups and projects, versioned policy documents
 * attached to identities, GRNs, and Google sign-in.
 */
const CAPABILITIES = [
  {
    icon: Building2,
    title: "Invitation, not signup",
    body: "An organization grows by invitation, never by open signup. A single-use, expiring token admits a user with a fixed org role — owner, admin, or member.",
  },
  {
    icon: FolderTree,
    title: "Groups and projects",
    body: "Groups bundle people; a project holds a unit of work. Both live under one organization, carry their own members, and can hold roles scoped to that resource alone.",
  },
  {
    icon: FileStack,
    title: "Policies attached to identities",
    body: "IAM-style documents, stored per organization and versioned append-only. Attaching one to a user, a group, or an agent decides who it reaches, and a matching Deny anywhere in the set ends the decision.",
  },
  {
    icon: Fingerprint,
    title: "One identifier system",
    body: "Organizations, groups, projects, policies, users, and agents all carry a GRN, so one shape answers who a subject is and what a policy is written about.",
  },
];

/**
 * Decisions behind the agent surface, each named by the ADR that
 * settles it so a reader can check the claim. Only the persistence
 * behind the first exists.
 */
const AGENT_DECISIONS = [
  {
    adr: "ADR-0012",
    title: "Identity and assignment",
    body: "garam mints an agent's GRN inside an organization and a project and never changes it, so which operator runs the agent is a relation a reassignment moves rather than part of the identity. That operator provisions it from inside a cluster garam never dials into.",
  },
  {
    adr: "ADR-0013",
    title: "Messages",
    body: "A message reaches an agent when an adapter beside it long-polls a queue held in Postgres, so any garam instance can answer any poll. Channels such as Slack are consumers of that API rather than part of it.",
  },
  {
    adr: "ADR-0014",
    title: "Authentication",
    body: "An agent presents a client certificate whose SAN URI is its GRN, on a listener that takes no session cookie. It holds no user row and no org membership; a policy attachment is what widens its reach.",
  },
];

const GRN_SEGMENTS = [
  { token: "grn", label: "prefix" },
  { token: "acme", label: "organization" },
  { token: "default", label: "project" },
  { token: "agent", label: "service type" },
  { token: "bot-1", label: "identifier" },
];

export const metadata: Metadata = {
  description:
    "One organization for the people and the agents that work in it.",
};

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
              One organization for the people and the agents that work in it.
            </h1>
            <p className="max-w-xl text-lead text-pretty text-muted-foreground">
              garam holds the identities, the structure they belong to, and the
              policy documents that decide what each may do. The people are here
              today; an agent already has an identity and is a subject of the
              same policies, while the surface it connects through is decided
              rather than built.
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
          <PolicyAttachments />
        </div>
      </section>

      <section
        id="organization"
        className="scroll-mt-20 border-b border-border/60"
      >
        <div className="mx-auto w-full max-w-shell px-6 py-section">
          <SectionHeading
            eyebrow="Organization"
            title="What an organization holds"
            body="The structure people are invited into, and the policy documents that decide what anything in it may reach."
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
        id="agents"
        className="scroll-mt-20 border-b border-border/60 bg-muted/30"
      >
        <div className="mx-auto w-full max-w-shell px-6 py-section">
          <SectionHeading
            eyebrow="Agents"
            title="Stored, not yet reachable"
            body="The agent surface is decided in ADRs and largely unbuilt: identity, operator assignment and state are persisted, and nothing else of it exists — no endpoint, no adapter, no message queue, and no certificate authority."
          />
          <ul className="mt-12 grid gap-px overflow-hidden rounded-xl border border-border bg-border md:grid-cols-3">
            {AGENT_DECISIONS.map(({ adr, title, body }) => (
              <li key={adr} className="flex flex-col gap-3 bg-background p-6">
                <span className="font-mono text-xs text-brand">{adr}</span>
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
        id="identifiers"
        className="scroll-mt-20 border-b border-border/60"
      >
        <div className="mx-auto w-full max-w-shell px-6 py-section">
          <SectionHeading
            eyebrow="Identifiers"
            title="One name shape, five segments"
            body="A GRN names every resource in the system. Its organization and project segments name the resources that own it, so an agent is addressed inside an organization the way a group or a project is."
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
            <span className="font-mono">default</span> is the project segment of
            an org-level resource. API paths address organizations, groups,
            projects, and policies by the last segment.
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
