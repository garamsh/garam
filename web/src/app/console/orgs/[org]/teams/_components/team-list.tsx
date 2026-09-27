import Link from "next/link";

import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { slugFromGrn, type Group } from "@/features/orgs";

import { orgHref } from "../../../../_components/navigation";

/**
 * The organization's teams by name, each linking to its own page, and
 * the roles each grants its members. The identifier beneath a name is
 * the segment the team's URL carries (ADR-0011).
 */
export function TeamList({ org, teams }: { org: string; teams: Group[] }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Team</TableHead>
          <TableHead>Roles it grants</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {teams.map((team) => {
          const identifier = slugFromGrn(team.grn);
          return (
            <TableRow key={team.grn}>
              <TableCell>
                <Link
                  href={`${orgHref(org, "teams")}/${identifier}`}
                  className="font-medium underline-offset-4 hover:underline focus-visible:underline"
                >
                  {team.name}
                </Link>
                <span className="block font-mono text-xs text-muted-foreground">
                  {identifier}
                </span>
              </TableCell>
              <TableCell>
                {team.roles.length === 0 ? (
                  <span className="text-muted-foreground">None</span>
                ) : (
                  <div className="flex flex-wrap gap-1.5">
                    {team.roles.map((role) => (
                      <Badge key={role} variant="secondary">
                        {role}
                      </Badge>
                    ))}
                  </div>
                )}
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}
