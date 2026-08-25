import { Card, CardTitle } from "@/components/ui/card";
import { slugFromGrn, type Operator } from "@/features/orgs";

/**
 * The organization's registered operators: what each is identified by,
 * who registered it and when. There is no state to show — garam never
 * dials an operator, so it learns nothing about one until it calls.
 */
export function OperatorList({ operators }: { operators: Operator[] }) {
  return (
    <ul className="flex flex-col gap-3">
      {operators.map((operator) => (
        <li key={operator.grn}>
          <Card className="gap-4 p-4">
            <CardTitle className="font-mono text-base">
              {slugFromGrn(operator.grn)}
            </CardTitle>
            <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
              <dt className="text-muted-foreground">Registered by</dt>
              <dd>{slugFromGrn(operator.registeredBy)}</dd>
              <dt className="text-muted-foreground">Registered</dt>
              <dd>
                <time dateTime={operator.createdAt}>
                  {operator.createdAt.slice(0, 10)}
                </time>
              </dd>
            </dl>
          </Card>
        </li>
      ))}
    </ul>
  );
}
