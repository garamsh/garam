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

export function MemberTable({ members }: { members: Member[] }) {
  return (
    <section className="flex flex-col gap-3">
      <h2 className="font-heading text-lg font-semibold">Members</h2>
      {members.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          This organization has no members.
        </p>
      ) : (
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
                <TableCell className="font-mono text-xs">
                  {member.userGrn}
                </TableCell>
                <TableCell>
                  <Badge variant="secondary">{member.role}</Badge>
                </TableCell>
                <TableCell>
                  <time dateTime={member.since}>
                    {member.since.slice(0, 10)}
                  </time>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </section>
  );
}
