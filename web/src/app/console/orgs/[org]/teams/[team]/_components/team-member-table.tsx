import { CopyId } from "@/components/copy-id";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { KIND_LABEL } from "@/features/orgs";

import { RemoveTeamMemberForm } from "./remove-team-member-form";
import { memberLabel, memberName, type TeamMember } from "./team-member";

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
              {member.label ?? (
                <span className="font-normal text-muted-foreground">
                  {memberName(member)}
                </span>
              )}
              <CopyId
                value={member.identifier}
                label={memberName(member)}
                className="flex"
              />
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
