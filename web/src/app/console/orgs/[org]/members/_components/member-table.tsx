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
 * is shown by the name the user row carries and addressed by the
 * identifier segment beneath it (ADR-0011). `nickname` is absent only
 * where that row is gone, and the identifier then stands alone: it is
 * the address rather than a name put in a name's place
 * (`api/openapi.yaml` §Member).
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
        {members.map((member) => {
          const identifier = slugFromGrn(member.userGrn);
          return (
            <TableRow key={member.userGrn}>
              <TableCell className="font-medium">
                {member.nickname ?? identifier}
                {member.nickname !== undefined && (
                  <span className="block text-xs font-normal text-muted-foreground">
                    {identifier}
                  </span>
                )}
              </TableCell>
              <TableCell>
                <Badge variant="secondary">{member.role}</Badge>
              </TableCell>
              <TableCell>
                <time dateTime={member.since}>{member.since.slice(0, 10)}</time>
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}
