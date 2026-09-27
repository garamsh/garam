import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

import { DetachPolicyForm } from "../../_components/detach-policy-form";
import {
  KIND_LABEL,
  KIND_PLURAL,
  subjectLabel,
  type AttachedSubject,
} from "../../_components/subjects";

/**
 * The subjects a policy is attached to, by name and type, each
 * detachable. One the organization no longer holds stays listed and
 * marked; one whose list the caller may not read is marked unchecked.
 */
export function AttachmentTable({
  org,
  policy,
  policyName,
  subjects,
}: {
  org: string;
  policy: string;
  policyName: string;
  subjects: AttachedSubject[];
}) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Attached to</TableHead>
          <TableHead>Type</TableHead>
          <TableHead>
            <span className="sr-only">Actions</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {subjects.map((subject) => (
          <TableRow key={subject.grn}>
            <TableCell className="align-top font-medium whitespace-normal">
              {subject.label ?? (
                <span className="font-normal text-muted-foreground">
                  No name available
                </span>
              )}
              {subject.label !== subject.identifier && (
                <span className="block font-mono text-xs font-normal text-muted-foreground">
                  {subject.identifier}
                </span>
              )}
              {subject.lookup === "missing" && (
                <span className="block text-xs font-normal text-destructive">
                  Not found in this organization
                </span>
              )}
              {subject.lookup === "refused" && subject.kind !== "unknown" && (
                <span className="block text-xs font-normal text-muted-foreground">
                  Not checked: this organization&apos;s{" "}
                  {KIND_PLURAL[subject.kind]} are not listed to you
                </span>
              )}
            </TableCell>
            <TableCell className="align-top">
              <Badge
                variant={subject.kind === "unknown" ? "outline" : "secondary"}
              >
                {KIND_LABEL[subject.kind]}
              </Badge>
            </TableCell>
            <TableCell className="align-top whitespace-normal">
              <DetachPolicyForm
                org={org}
                policy={policy}
                subject={subject.grn}
                policyName={policyName}
                subjectLabel={subjectLabel(subject)}
              />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
