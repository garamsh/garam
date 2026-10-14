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
import { slugFromGrn, type Member } from "@/features/orgs";

/**
 * The organization's members by name, role and joining date. A member
 * is shown by the name the user row carries, and the identifier segment
 * the API addresses them by (ADR-0011) — `local-` and all, for an
 * account garam minted — is offered beneath it to copy rather than as
 * text to read.
 *
 * `nickname` is absent only where that row is gone
 * (`api/openapi.yaml` §Member), and the cell then says so rather than
 * lifting the identifier into the name's place: an identity subject
 * put where a name goes reads as a name and names nobody.
 */
export function MemberTable({ members }: { members: Member[] }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>User</TableHead>
          <TableHead>Role</TableHead>
          <TableHead>Member since</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {members.map((member) => (
          <TableRow key={member.userGrn}>
            <TableCell className="font-medium">
              {member.nickname ?? (
                <span className="font-normal text-muted-foreground">
                  No name on record
                </span>
              )}
              <CopyId
                value={slugFromGrn(member.userGrn)}
                label={member.nickname ?? "this member"}
                className="flex"
              />
            </TableCell>
            <TableCell>
              <Badge variant="secondary">{member.role}</Badge>
            </TableCell>
            <TableCell>
              <time dateTime={member.since}>{member.since.slice(0, 10)}</time>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
