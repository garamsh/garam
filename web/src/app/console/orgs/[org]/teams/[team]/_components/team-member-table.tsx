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
import { KIND_LABEL, memberLabel, type TeamMember } from "./team-member";

/**
 * A team's members by name and type, each removable from this team.
 *
 * A member without a name says so rather than lifting its identifier
 * into the name's place, and one the organization no longer holds is
 * marked rather than dropped: either would otherwise read as someone
 * the organization knows.
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
                    {member.kind === "unknown"
                      ? "Unknown identity"
                      : member.resolved
                        ? "No name on record"
                        : "Unknown person"}
                  </span>
                ))
              )}
              {member.kind !== "agent" && (
                <span className="block font-mono text-xs font-normal text-muted-foreground">
                  {member.identifier}
                </span>
              )}
              {!member.resolved && (
                <span className="block text-xs font-normal text-destructive">
                  Not found in this organization
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
