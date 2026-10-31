import { UserRound } from "lucide-react";
import type { Metadata } from "next";

import {
  getOrg,
  lookupOrgMembers,
  lookupPendingInvitations,
  slugFromGrn,
  type Member,
  type PendingInvitation,
  type RefusableRead,
} from "@/features/orgs";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { EmptyState } from "../../../_components/empty-state";
import { InactiveButton } from "../../../_components/inactive-button";
import { readMemberChoices } from "../../../_components/member-choices";
import { MemberList, type MemberRow } from "../../../_components/member-list";
import { orgHref } from "../../../_components/navigation";
import { RefusedRead } from "../../../_components/refused-read";
import { ListPage } from "../../../_components/templates/list-page";
import { InviteDialog } from "./_components/invite-dialog";
import { PendingInvitations } from "./_components/pending-invitations";

/** Head metadata for the people route; Next.js reads it by name. */
export const metadata: Metadata = { title: "People — garam" };

type Params = {
  params: Promise<{ org: string }>;
  searchParams: Promise<{ team?: string }>;
};

const TITLE = "People";

const ROLE_LABEL = { owner: "Owner", admin: "Admin", member: "Member" };

/**
 * The invitations still pending, each inviter named from the people
 * read where it holds them. A read garam refused or never answered is
 * said, not left out.
 */
function PendingPart({
  org,
  pending,
  people,
}: {
  org: string;
  pending: RefusableRead<PendingInvitation[]>;
  people: RefusableRead<Member[]>;
}) {
  const subject = "this organization's pending invitations";
  if (pending.status === "unreachable") {
    return <ApiUnreachable subject={subject} />;
  }
  if (pending.status === "forbidden") return <RefusedRead subject={subject} />;
  const members = people.status === "ok" ? people.data : [];
  const nickname = (grn: string) =>
    members.find((member) => member.userGrn === grn)?.nickname;
  return (
    <PendingInvitations
      org={org}
      invitations={pending.data.map((invitation) => ({
        id: invitation.id,
        role: invitation.role,
        invitedBy:
          nickname(invitation.createdByGrn) ??
          slugFromGrn(invitation.createdByGrn),
        createdAt: invitation.createdAt,
        expiresAt: invitation.expiresAt,
      }))}
    />
  );
}

/**
 * The organization's people (ADR-0116), filtered by team, added to a
 * team or given a policy several at once, offered only for the teams
 * and policies the server does not deny. Inviting is the page's one
 * primary action, and whoever may invite sees the invitations still
 * pending and revokes one. A list garam refused is said, never shown as
 * empty.
 * The organization is read for itself, so one the caller cannot read
 * answers 404 (ADR-0011).
 */
export default async function PeoplePage({ params, searchParams }: Params) {
  const [{ org }, query] = await Promise.all([params, searchParams]);
  const [organization, people, choices] = await Promise.all([
    getOrg(org),
    lookupOrgMembers(org),
    readMemberChoices(org, query.team, { invite: { action: "org:invite" } }),
  ]);

  const invite =
    choices.may.get("invite") === "denied" ? (
      <InactiveButton
        variant="default"
        reason="No policy lets you invite people."
      >
        Invite people
      </InactiveButton>
    ) : (
      <InviteDialog org={org} />
    );
  const pending =
    choices.may.get("invite") === "denied"
      ? null
      : await lookupPendingInvitations(org);

  const pendingPart = pending && (
    <PendingPart org={org} pending={pending} people={people} />
  );

  if (
    organization.status === "unreachable" ||
    people.status === "unreachable"
  ) {
    return (
      <ListPage title={TITLE} primaryAction={invite}>
        <ApiUnreachable subject="this organization's people" />
      </ListPage>
    );
  }
  if (people.status === "forbidden") {
    return (
      <ListPage title={TITLE} primaryAction={invite}>
        <RefusedRead subject="this organization's people" />
        {pendingPart}
      </ListPage>
    );
  }

  const all: MemberRow[] = people.data.map((person) => {
    const identifier = slugFromGrn(person.userGrn);
    return {
      kind: "person",
      grn: person.userGrn,
      identifier,
      name: person.nickname ?? "No name on record",
      href: orgHref(org, `people/${identifier}`),
      status: { tone: "neutral", label: ROLE_LABEL[person.role] },
      detail: null,
      runsOn: null,
      lastSeen: null,
    };
  });
  const rows = all.filter(
    (row) => choices.inTeam === null || choices.inTeam.has(row.grn),
  );

  return (
    <ListPage title={TITLE} count={all.length} primaryAction={invite}>
      {choices.teamUnread && (
        <RefusedRead subject="this team's members">
          The team filter is not applied.
        </RefusedRead>
      )}
      <MemberList
        org={org}
        kind="person"
        rows={rows}
        filters={{
          team: query.team ?? "",
          runs: "",
          teams: choices.teams,
          runsOn: null,
        }}
        teamChoices={choices.teamChoices}
        policyChoices={choices.policyChoices}
        empty={<EmptyState icon={UserRound} title="No one matches." />}
      />
      {pendingPart}
    </ListPage>
  );
}
