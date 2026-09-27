import Link from "next/link";

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { slugFromGrn, type Policy } from "@/features/orgs";

import { orgHref } from "../../../../_components/navigation";
import { PolicyStatusBadge } from "./policy-status-badge";

/**
 * The organization's policies by name, each linking to its own page,
 * with its state and the version it evaluates while active.
 */
export function PolicyList({
  org,
  policies,
}: {
  org: string;
  policies: Policy[];
}) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Policy</TableHead>
          <TableHead>Status</TableHead>
          <TableHead>Default version</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {policies.map((policy) => {
          const identifier = slugFromGrn(policy.grn);
          return (
            <TableRow key={policy.grn}>
              <TableCell>
                <Link
                  href={`${orgHref(org, "permissions")}/${identifier}`}
                  className="font-medium underline-offset-4 hover:underline focus-visible:underline"
                >
                  {policy.name}
                </Link>
                <span className="block font-mono text-xs text-muted-foreground">
                  {identifier}
                </span>
              </TableCell>
              <TableCell>
                <PolicyStatusBadge status={policy.status} />
              </TableCell>
              <TableCell className="tabular-nums">
                Version {policy.defaultVersion}
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}
