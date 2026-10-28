import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { DescriptionList } from "@/components/description-list";
import { MemberMark } from "@/components/member-mark";
import { StatusDot } from "@/components/status-dot";
import { Time } from "@/components/time";
import { getOrg, lookupOrgMembers, slugFromGrn } from "@/features/orgs";

import { ApiUnreachable } from "../../../../_components/api-unreachable";
import { ItemCrumb } from "../../../../_components/item-crumb";
import { PageHeader } from "../../../../_components/page-header";
import { RefusedRead } from "../../../../_components/refused-read";
import { DetailPage } from "../../../../_components/templates/detail-page";
import { YourAccess, readAccess } from "../../../../_components/your-access";

/** Head metadata for a person's route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Person — garam" };

type Params = { params: Promise<{ org: string; person: string }> };

const ROLE_LABEL = { owner: "Owner", admin: "Admin", member: "Member" };

/**
 * One person of the organization, on the member layout an agent's page
 * shares (ADR-0113): their mark, name and identifier, their role and
 * since when, and what the caller may do here. A person is read from
 * the organization's members, since garam reads no member alone; one
 * the list does not hold answers 404. The organization is read for
 * itself, so one the caller cannot read answers 404 (ADR-0011).
 */
export default async function PersonPage({ params }: Params) {
  const { org, person } = await params;
  // Started before the page's own reads, so it runs beside them.
  const access = readAccess(org, [
    {
      key: "members",
      label: "See who is in this organization",
      check: { action: "org:member-list" },
    },
    { key: "invite", label: "Invite people", check: { action: "org:invite" } },
  ]);
  const [organization, members] = await Promise.all([
    getOrg(org),
    lookupOrgMembers(org),
  ]);

  const fallback = (
    <PageHeader title="Person" description="A person of this organization." />
  );
  if (
    organization.status === "unreachable" ||
    members.status === "unreachable"
  ) {
    return (
      <>
        {fallback}
        <ApiUnreachable subject="this person" />
      </>
    );
  }
  if (members.status === "forbidden") {
    return (
      <>
        {fallback}
        <RefusedRead subject="this organization's people" />
      </>
    );
  }
  const member = members.data.find(
    (candidate) => slugFromGrn(candidate.userGrn) === person,
  );
  if (member === undefined) notFound();

  const name = member.nickname ?? "No name on record";
  const role = { tone: "neutral" as const, label: ROLE_LABEL[member.role] };

  return (
    <>
      <ItemCrumb label={name} />
      <DetailPage
        mark={<MemberMark kind="person" name={name} className="size-7" />}
        name={name}
        idLabel="Person identifier"
        id={person}
        status={<StatusDot {...role} />}
        facts={[
          { term: "Role", description: role.label },
          { term: "Member since", description: <Time value={member.since} /> },
        ]}
        tabs={[
          {
            value: "overview",
            label: "Overview",
            content: (
              <DescriptionList
                items={[
                  {
                    term: "Role in this organization",
                    description: role.label,
                  },
                  {
                    term: "Member since",
                    description: <Time value={member.since} />,
                  },
                ]}
              />
            ),
          },
        ]}
      />
      <YourAccess readings={await access} />
    </>
  );
}
