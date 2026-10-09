import Link from "next/link";

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { KIND_LABEL, slugFromGrn } from "@/features/orgs";

import { PolicyStatusBadge } from "../../_components/policy-status-badge";
import {
  reachOf,
  subjectLabel,
  type Catalogs,
  type PolicyScope,
} from "../../_components/subjects";

/**
 * Every policy of the organization with whom it is attached to. An
 * active policy with no attachment reaches everyone; one whose
 * attachments were refused says its scope is unknown rather than
 * claiming either.
 */
export function ScopeTable({
  policyHref,
  subjectHref,
  scopes,
  catalogs,
}: {
  policyHref: (policy: string) => string;
  subjectHref: (query: string) => string;
  scopes: PolicyScope[];
  catalogs: Catalogs;
}) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Policy</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Attached to</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {scopes.map(({ policy, attachments }) => {
          const reach = reachOf(attachments, catalogs);
          return (
            <TableRow key={policy.grn}>
              <TableCell className="align-top">
                <Link
                  href={policyHref(slugFromGrn(policy.grn))}
                  className="font-medium underline-offset-4 hover:underline focus-visible:underline"
                >
                  {policy.name}
                </Link>
              </TableCell>
              <TableCell className="align-top">
                <PolicyStatusBadge status={policy.status} />
              </TableCell>
              <TableCell className="align-top whitespace-normal">
                {reach.kind === "unknown" ? (
                  <span className="text-muted-foreground">
                    Not checked: not listed to you
                  </span>
                ) : reach.kind === "org-wide" ? (
                  policy.status === "active" ? (
                    "Everyone in this organization"
                  ) : (
                    <span className="text-muted-foreground">
                      No attachments
                    </span>
                  )
                ) : (
                  reach.subjects.map((subject, index) => {
                    const text = `${subjectLabel(subject)} (${KIND_LABEL[subject.kind]})`;
                    return (
                      <span key={subject.grn}>
                        {index > 0 && ", "}
                        {subject.lookup === "found" &&
                        subject.kind !== "unknown" ? (
                          <Link
                            href={subjectHref(
                              `${subject.kind}:${subject.identifier}`,
                            )}
                            className="underline-offset-4 hover:underline focus-visible:underline"
                          >
                            {text}
                          </Link>
                        ) : (
                          text
                        )}
                      </span>
                    );
                  })
                )}
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}
