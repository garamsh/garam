import type { Metadata } from "next";
import Link from "next/link";

import { Button } from "@garam/design/ui/button";
import { getOrg, listAuditRecords } from "@/features/orgs";

import { ApiUnreachable } from "../../../_components/api-unreachable";
import { orgHref } from "../../../_components/navigation";
import { RefusedRead } from "../../../_components/refused-read";
import { ListPage } from "../../../_components/templates/list-page";
import { AuditTable } from "./_components/audit-table";

/** Head metadata for the audit route; Next.js reads it by name. */
export const metadata: Metadata = { title: "Audit — garam" };

type Params = {
  params: Promise<{ org: string }>;
  searchParams: Promise<{ page?: string }>;
};

const SUBJECT = "this organization's audit records";

/**
 * The organization's audit records, newest first, a page at a time
 * (ADR-0176). The policy service decides the read on `iam:audit.read`,
 * so a refusal is said and never shown as an empty list. The
 * organization is read for itself, so one the caller cannot read
 * answers 404 (ADR-0011). The page token of the next page is the
 * `page` search parameter.
 */
export default async function AuditPage({ params, searchParams }: Params) {
  const [{ org }, { page }] = await Promise.all([params, searchParams]);
  const [organization, records] = await Promise.all([
    getOrg(org),
    listAuditRecords(org, page),
  ]);

  return (
    <ListPage
      title="Audit"
      description="Policy writes and denials, newest first."
    >
      {organization.status === "unreachable" ||
      records.status === "unreachable" ? (
        <ApiUnreachable subject={SUBJECT} />
      ) : records.status === "forbidden" ? (
        <RefusedRead subject={SUBJECT} />
      ) : (
        <>
          <AuditTable records={records.data.records} />
          {records.data.nextPageToken && (
            <div>
              <Button asChild variant="outline">
                <Link
                  href={`${orgHref(org, "audit")}?page=${encodeURIComponent(records.data.nextPageToken)}`}
                >
                  Older records
                </Link>
              </Button>
            </div>
          )}
        </>
      )}
    </ListPage>
  );
}
