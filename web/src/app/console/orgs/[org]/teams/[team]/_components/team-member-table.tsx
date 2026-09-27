import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

import { RemoveTeamMemberForm } from "./remove-team-member-form";
import {
  KIND_LABEL,
  memberLabel,
  type Lookup,
  type TeamMember,
} from "./team-member";

/**
 * What stands where a member without a name would have one: a person
 * found with no name on record, one the organization does not hold,
 * one whose list was refused, and an identity of neither type.
 */
const PLACEHOLDER: Record<Lookup | "unknown", string> = {
  found: "No name on record",
  missing: "Unknown person",
  refused: "Name not available",
  unknown: "Unknown identity",
};

/**
 * A team's members by name and type, each removable from this team.
 *
 * A member without a name says so rather than lifting its identifier
 * into the name's place, and one the organization no longer holds is
 * marked rather than dropped: either would otherwise read as someone
 * the organization knows. One whose list the caller may not read is
 * marked unchecked, never missing.
 */
export function TeamMemberTable({
  org,
  team,
  teamName,
  members,
}: {
  org: string;
  team: string;
  teamName: string;
  members: TeamMember[];
}) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Member</TableHead>
          <TableHead>Type</TableHead>
          <TableHead>
            <span className="sr-only">Actions</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {members.map((member) => (
          <TableRow key={member.grn}>
            <TableCell className="align-top font-medium whitespace-normal">
              {member.kind === "agent" ? (
                <span className="font-mono">{member.name}</span>
              ) : (
                (member.name ?? (
                  <span className="font-normal text-muted-foreground">
                    {
                      PLACEHOLDER[
                        member.kind === "unknown" ? "unknown" : member.lookup
                      ]
                    }
                  </span>
                ))
              )}
              {member.kind !== "agent" && (
                <span className="block font-mono text-xs font-normal text-muted-foreground">
                  {member.identifier}
                </span>
              )}
              {member.lookup === "missing" && (
                <span className="block text-xs font-normal text-destructive">
                  Not found in this organization
                </span>
              )}
              {member.lookup === "refused" && (
                <span className="block text-xs font-normal text-muted-foreground">
                  Not checked: this organization&apos;s{" "}
                  {member.kind === "agent" ? "agents are" : "people are"} not
                  listed to you
                </span>
              )}
            </TableCell>
            <TableCell className="align-top">
              <Badge
                variant={member.kind === "unknown" ? "outline" : "secondary"}
              >
                {KIND_LABEL[member.kind]}
              </Badge>
            </TableCell>
            <TableCell className="align-top whitespace-normal">
              <RemoveTeamMemberForm
                org={org}
                team={team}
                teamName={teamName}
                member={member.grn}
                label={memberLabel(member)}
              />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
