import Link from "next/link";

import { slugFromGrn, type Group, type Policy } from "@/features/orgs";

import { AttachPolicyForm } from "../../_components/attach-policy-form";
import { DetachPolicyForm } from "../../_components/detach-policy-form";
import {
  POLICY_STATUS_LABEL,
  PolicyStatusBadge,
} from "../../_components/policy-status-badge";
import {
  KIND_LABEL,
  type Inspection,
  type Subject,
} from "../../_components/subjects";

/** A policy's name, linking to its page, and its state. */
function PolicyName({
  policy,
  policyHref,
}: {
  policy: Policy;
  policyHref: (policy: string) => string;
}) {
  return (
    <span className="flex flex-wrap items-center gap-2">
      <Link
        href={policyHref(slugFromGrn(policy.grn))}
        className="font-medium underline-offset-4 hover:underline focus-visible:underline"
      >
        {policy.name}
      </Link>
      <PolicyStatusBadge status={policy.status} />
      {policy.status !== "active" && (
        <span className="text-xs text-muted-foreground">not evaluated</span>
      )}
    </span>
  );
}

/** A policy as an option of the attach choice. */
function policyOption(policy: Policy): { value: string; label: string } {
  return {
    value: slugFromGrn(policy.grn),
    label: `${policy.name} · ${POLICY_STATUS_LABEL[policy.status]}`,
  };
}

/** A list of team names, each opening that team's attachments. */
function TeamNames({
  teams,
  subjectHref,
}: {
  teams: Group[];
  subjectHref: (query: string) => string;
}) {
  return teams.map((team, index) => (
    <span key={team.grn}>
      {index > 0 && ", "}
      <Link
        href={subjectHref(`team:${slugFromGrn(team.grn)}`)}
        className="underline underline-offset-4"
      >
        {team.name}
      </Link>
    </span>
  ));
}

/**
 * Where the policies naming one subject come from: attached to it,
 * attached to a team it belongs to, or applying to everyone. The three
 * are kept apart, what could not be read is said to be unchecked, and
 * the whole is not offered as the subject's access.
 */
export function SubjectInspection({
  org,
  subject,
  inspection,
  attachable,
  policyHref,
  subjectHref,
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
}) {
  const { direct, inherited, orgWide, unread } = inspection;
  // With a policy's attachments unread, an empty list says only that
  // no policy the caller can read is in it.
  const readable = unread.length > 0 ? " you can read" : "";
  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h2 className="font-heading text-lg font-semibold">
          {subject.label} · {KIND_LABEL[subject.kind]}
        </h2>
        <p className="max-w-2xl text-sm text-muted-foreground">
          The policies that name {subject.label}. Roles grant access too, an
          explicit Deny wins, and Conditions are not evaluated, so this is not a
          final access decision.
        </p>
      </div>

      <section className="flex flex-col gap-3" aria-labelledby="direct">
        <h3 id="direct" className="font-medium">
          Attached directly
        </h3>
        {direct.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No policy{readable} is attached to {subject.label}.
          </p>
        ) : (
          <ul className="flex flex-col divide-y rounded-lg border">
            {direct.map((policy) => (
              <li
                key={policy.grn}
                className="flex flex-wrap items-start justify-between gap-3 p-3"
              >
                <PolicyName policy={policy} policyHref={policyHref} />
                <DetachPolicyForm
                  org={org}
                  policy={slugFromGrn(policy.grn)}
                  subject={subject.grn}
                  policyName={policy.name}
                  subjectLabel={subject.label}
                  policyHref={policyHref(slugFromGrn(policy.grn))}
                />
              </li>
            ))}
          </ul>
        )}
        <AttachPolicyForm
          org={org}
          fixed={{ name: "subject", value: subject.grn }}
          choose={{
            label: "Policy",
            placeholder: "Choose a policy to attach",
            groups: [
              {
                label: "Policies",
                options: attachable
                  .filter(
                    ({ policy, unattached }) =>
                      !(unattached && policy.status === "active"),
                  )
                  .map(({ policy }) => policyOption(policy)),
              },
              {
                // The first attachment of an active policy with none
                // takes it from everyone to this one subject.
                label: "Applies to everyone now; attaching narrows it",
                options: attachable
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
      </section>

      {inherited !== undefined && (
        <section className="flex flex-col gap-3" aria-labelledby="inherited">
          <h3 id="inherited" className="font-medium">
            Through teams
          </h3>
          {inherited.uncheckedTeams === null ? (
            <p className="text-sm text-muted-foreground">
              Not checked: this organization&apos;s teams are not listed to you.
            </p>
          ) : inherited.via.length === 0 ? (
            // An unread team may be the one, so absence is claimed only
            // where every attached team was read.
            inherited.uncheckedTeams.length === 0 && (
              <p className="text-sm text-muted-foreground">
                No policy{readable} reaches {subject.label} through a team.
              </p>
            )
          ) : (
            <ul className="flex flex-col divide-y rounded-lg border">
              {inherited.via.map(({ policy, team }) => (
                <li
                  key={`${policy.grn} ${team.grn}`}
                  className="flex flex-wrap items-center justify-between gap-3 p-3"
                >
                  <PolicyName policy={policy} policyHref={policyHref} />
                  <span className="text-sm text-muted-foreground">
                    through{" "}
                    <TeamNames teams={[team]} subjectHref={subjectHref} />
                  </span>
                </li>
              ))}
            </ul>
          )}
          {inherited.uncheckedTeams !== null &&
            inherited.uncheckedTeams.length > 0 && (
              <p className="text-sm text-muted-foreground">
                Not checked: the members of{" "}
                <TeamNames
                  teams={inherited.uncheckedTeams}
                  subjectHref={subjectHref}
                />{" "}
                are not listed to you, and policies are attached to them.
              </p>
            )}
        </section>
      )}

      <section className="flex flex-col gap-3" aria-labelledby="org-wide">
        <h3 id="org-wide" className="font-medium">
          Organization-wide
        </h3>
        {orgWide.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No active policy{readable} applies to everyone in this organization.
          </p>
        ) : (
          <ul className="flex flex-col divide-y rounded-lg border">
            {orgWide.map((policy) => (
              <li key={policy.grn} className="p-3">
                <PolicyName policy={policy} policyHref={policyHref} />
              </li>
            ))}
          </ul>
        )}
      </section>

      {unread.length > 0 && (
        <p role="note" className="text-sm text-muted-foreground">
          Not checked: the attachments of{" "}
          {unread.map((policy) => policy.name).join(", ")} are not listed to
          you, so whether they name {subject.label} is unknown.
        </p>
      )}
    </div>
  );
}
