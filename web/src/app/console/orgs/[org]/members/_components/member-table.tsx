import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { Member } from "@/features/orgs/api/orgs";
import { slugFromGrn } from "@/features/orgs/grn";

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
              {slugFromGrn(member.userGrn)}
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
