import Link from "next/link";

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { slugFromGrn, type Operator } from "@/features/orgs";

import { orgHref } from "../../../../_components/navigation";
import { EnrollmentBadge } from "./enrollment-badge";

/**
 * The organization's operators by name, each linking to its own page,
 * with where each stands in obtaining a certificate.
 *
 * A name is what the organization calls an operator and two may share
 * one, so the identifier — the operator's certificate identity and the
 * segment its page's URL carries — is under every name.
 */
export function OperatorList({
  org,
  operators,
}: {
  org: string;
  operators: Operator[];
}) {
  return (
    <div className="flex flex-col gap-3">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Operator</TableHead>
            <TableHead>Enrollment</TableHead>
            <TableHead className="hidden sm:table-cell">Registered</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {operators.map((operator) => {
            const identifier = slugFromGrn(operator.grn);
            return (
              <TableRow key={operator.grn}>
                <TableCell className="whitespace-normal">
                  <Link
                    href={`${orgHref(org, "operators")}/${identifier}`}
                    className="font-medium wrap-anywhere underline-offset-4 hover:underline focus-visible:underline"
                  >
                    {operator.name}
                  </Link>
                  <span className="block font-mono text-xs text-muted-foreground">
                    {identifier}
                  </span>
                </TableCell>
                <TableCell>
                  <EnrollmentBadge state={operator.enrollmentState} />
                </TableCell>
                <TableCell className="hidden sm:table-cell">
                  <time dateTime={operator.createdAt}>
                    {operator.createdAt.slice(0, 10)}
                  </time>
                </TableCell>
              </TableRow>
            );
          })}
        </TableBody>
      </Table>
      <p className="max-w-2xl text-sm text-muted-foreground">
        Enrollment is not a sign an operator is running.
      </p>
    </div>
  );
}
