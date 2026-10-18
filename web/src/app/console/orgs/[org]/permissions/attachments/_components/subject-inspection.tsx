import Link from "next/link";
import { Building2, Link2, Users } from "lucide-react";
import type { LucideIcon } from "lucide-react";

import { Banner } from "@/components/banner";
import {
  KIND_LABEL,
  slugFromGrn,
  type Group,
  type Policy,
} from "@/features/orgs";

import { AttachPolicyForm } from "../../_components/attach-policy-form";
import { DetachPolicyDialog } from "../../_components/detach-policy-dialog";
import { Outcome, OutcomeProvider } from "../../../../../_components/outcome";
import {
  POLICY_STATUS_LABEL,
  PolicyStatus,
} from "../../_components/policy-status";
import { type Inspection, type Subject } from "../../_components/subjects";

/** A policy's name, linking to its page, and its state. */
function PolicyName({
  policy,
  policyHref,
}: {
  policy: Policy;
  policyHref: (policy: string) => string;
}) {
  return (
    <span className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
      <Link
        href={policyHref(slugFromGrn(policy.grn))}
        className="font-medium underline-offset-4 hover:underline focus-visible:underline"
      >
        {policy.name}
      </Link>
      <PolicyStatus status={policy.status} />
      {policy.status !== "active" && (
        <span className="text-xs text-muted-foreground">not evaluated</span>
      )}
    </span>
  );
}

/** A policy as an option of the attach choice. */
function policyOption(policy: Policy): {
  value: string;
  label: string;
  name: string;
} {
  return {
    value: slugFromGrn(policy.grn),
    label: `${policy.name} · ${POLICY_STATUS_LABEL[policy.status]}`,
    name: policy.name,
  };
}

/** A team's name, opening that team's attachments. */
function TeamLink({
  team,
  subjectHref,
}: {
  team: Group;
  subjectHref: (query: string) => string;
}) {
  return (
    <Link
      href={subjectHref(`team:${slugFromGrn(team.grn)}`)}
      className="font-medium text-foreground underline underline-offset-4"
    >
      {team.name}
    </Link>
  );
}

/**
 * One way policies reach the subject, as a card: what the way is, how
 * many reach it so, and the policies, each with where it comes from.
 */
function Source({
  id,
  icon: Icon,
  title,
  description,
  count,
  children,
}: {
  id: string;
  icon: LucideIcon;
  title: string;
  description: string;
  /** Omitted where the list could not be read whole. */
  count?: number;
  children: React.ReactNode;
}) {
  return (
    <section
      aria-labelledby={id}
      className="flex flex-col rounded-xl border bg-card"
    >
      <div className="flex items-start gap-3 border-b px-4 py-3">
        <Icon
          aria-hidden
          className="mt-0.5 size-4 shrink-0 text-muted-foreground"
        />
        <div className="flex min-w-0 flex-col gap-0.5">
          <h3 className="flex items-baseline gap-2 text-base">
            {/* The region is named by the title alone, not its count. */}
            <span id={id}>{title}</span>
            {count !== undefined && (
              <span className="text-sm font-normal text-muted-foreground tabular-nums">
                {count}
              </span>
            )}
          </h3>
          <p className="text-muted-foreground">{description}</p>
        </div>
      </div>
      <div className="flex flex-col gap-3 p-4">{children}</div>
    </section>
  );
}

/** Rows of a source's policies. */
function Rows({ children }: { children: React.ReactNode }) {
  return (
    <ul className="flex flex-col divide-y rounded-lg border">{children}</ul>
  );
}

/**
 * Where the policies naming one subject come from: attached to it
 * directly, inherited through a team it belongs to, or applying to
 * everyone. The three are kept apart, what could not be read is said to
 * be unchecked, and the whole is not offered as the subject's access.
 */
export function SubjectInspection({
  org,
  subject,
  inspection,
  attachable,
  policyHref,
  subjectHref,
  may,
}: {
  org: string;
  subject: Subject;
  inspection: Inspection;
  /**
   * Policies the subject can be attached to — readable and not attached
   * to it — and whether each has no attachment yet.
   */
  attachable: { policy: Policy; unattached: boolean }[];
  policyHref: (policy: string) => string;
  subjectHref: (query: string) => string;
  /** Whether the server leaves the caller a write on a policy. */
  may: (action: "attach" | "detach", policy: Policy) => boolean;
}) {
  const { direct, inherited, orgWide, unread } = inspection;
  // With a policy's attachments unread, an empty list says only that
  // no policy the caller can read is in it.
  const readable = unread.length > 0 ? " you can read" : "";
  const offered = attachable.filter(({ policy }) => may("attach", policy));
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h2 className="text-lg">
          {subject.label} · {KIND_LABEL[subject.kind]}
        </h2>
        <p className="max-w-2xl text-muted-foreground">
          The policies that name {subject.label}. Roles grant access too, an
          explicit Deny wins, and Conditions are not evaluated, so this is not a
          final access decision.
        </p>
      </div>

      <Source
        id="direct"
        icon={Link2}
        title="Attached directly"
        description={`Attached to ${subject.label} itself.`}
        count={unread.length > 0 ? undefined : direct.length}
      >
        {/* A detached row goes, so its success is said above the rows. */}
        <OutcomeProvider>
          <Outcome area="attachments" />
          {direct.length === 0 ? (
            <p className="text-muted-foreground">
              No policy{readable} is attached to {subject.label}.
            </p>
          ) : (
            <Rows>
              {direct.map((policy) => (
                <li
                  key={policy.grn}
                  className="flex flex-wrap items-center justify-between gap-3 px-3 py-2"
                >
                  <PolicyName policy={policy} policyHref={policyHref} />
                  {may("detach", policy) && (
                    <DetachPolicyDialog
                      org={org}
                      policy={slugFromGrn(policy.grn)}
                      subject={subject.grn}
                      policyName={policy.name}
                      subjectLabel={subject.label}
                      policyHref={policyHref(slugFromGrn(policy.grn))}
                    />
                  )}
                </li>
              ))}
            </Rows>
          )}
          {offered.length > 0 && (
            <AttachPolicyForm
              org={org}
              fixed={{
                name: "subject",
                value: subject.grn,
                label: subject.label,
              }}
              choose={{
                label: "Attach a policy",
                placeholder: "Choose a policy to attach",
                groups: [
                  {
                    label: "Policies",
                    options: offered
                      .filter(
                        ({ policy, unattached }) =>
                          !(unattached && policy.status === "active"),
                      )
                      .map(({ policy }) => policyOption(policy)),
                  },
                  {
                    // The first attachment of an active policy with
                    // none
                    // takes it from everyone to this one subject.
                    label: "Applies to everyone now; attaching narrows it",
                    options: offered
                      .filter(
                        ({ policy, unattached }) =>
                          unattached && policy.status === "active",
                      )
                      .map(({ policy }) => policyOption(policy)),
                  },
                ],
              }}
              empty="Every policy you can list is already attached here."
            />
          )}
        </OutcomeProvider>
      </Source>

      {inherited !== undefined && (
        <Source
          id="inherited"
          icon={Users}
          title="Through teams"
          description={`Inherited: attached to a team ${subject.label} belongs to. Detach it on that team.`}
          count={
            inherited.uncheckedTeams === null ||
            inherited.uncheckedTeams.length > 0 ||
            unread.length > 0
              ? undefined
              : inherited.via.length
          }
        >
          {inherited.uncheckedTeams === null ? (
            <p className="text-muted-foreground">
              Not checked: this organization&apos;s teams are not listed to you.
            </p>
          ) : inherited.via.length === 0 ? (
            // An unread team may be the one, so absence is claimed only
            // where every attached team was read.
            inherited.uncheckedTeams.length === 0 && (
              <p className="text-muted-foreground">
                No policy{readable} reaches {subject.label} through a team.
              </p>
            )
          ) : (
            <Rows>
              {inherited.via.map(({ policy, team }) => (
                <li
                  key={`${policy.grn} ${team.grn}`}
                  className="flex flex-wrap items-center justify-between gap-3 px-3 py-2"
                >
                  <PolicyName policy={policy} policyHref={policyHref} />
                  <span className="text-sm text-muted-foreground">
                    through <TeamLink team={team} subjectHref={subjectHref} />
                  </span>
                </li>
              ))}
            </Rows>
          )}
          {inherited.uncheckedTeams !== null &&
            inherited.uncheckedTeams.length > 0 && (
              <p className="text-muted-foreground">
                Not checked: the members of{" "}
                {inherited.uncheckedTeams.map((team, index) => (
                  <span key={team.grn}>
                    {index > 0 && ", "}
                    <TeamLink team={team} subjectHref={subjectHref} />
                  </span>
                ))}{" "}
                are not listed to you, and policies are attached to them.
              </p>
            )}
        </Source>
      )}

      <Source
        id="org-wide"
        icon={Building2}
        title="Organization-wide"
        description="Active policies with no attachment apply to everyone, this subject included."
        count={unread.length > 0 ? undefined : orgWide.length}
      >
        {orgWide.length === 0 ? (
          <p className="text-muted-foreground">
            No active policy{readable} applies to everyone in this organization.
          </p>
        ) : (
          <Rows>
            {orgWide.map((policy) => (
              <li key={policy.grn} className="px-3 py-2">
                <PolicyName policy={policy} policyHref={policyHref} />
              </li>
            ))}
          </Rows>
        )}
      </Source>

      {unread.length > 0 && (
        <Banner tone="info" title="Some policies were not checked">
          The attachments of {unread.map((policy) => policy.name).join(", ")}{" "}
          are not listed to you, so whether they name {subject.label} is
          unknown.
        </Banner>
      )}
    </div>
  );
}
