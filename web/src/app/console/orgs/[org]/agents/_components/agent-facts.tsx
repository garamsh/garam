import Link from "next/link";
import { Fragment } from "react";

import { orgHref } from "../../../../_components/navigation";

/** An operator by name and identifier, or by identifier alone. */
export function OperatorName({
  org,
  identifier,
  name,
}: {
  org: string;
  identifier: string;
  name: string | undefined;
}) {
  if (name === undefined) {
    return <span className="font-mono">{identifier}</span>;
  }
  return (
    <>
      <Link
        href={`${orgHref(org, "operators")}/${identifier}`}
        className="wrap-anywhere underline-offset-4 hover:underline focus-visible:underline"
      >
        {name}
      </Link>
      <span className="block font-mono text-xs text-muted-foreground">
        {identifier}
      </span>
    </>
  );
}

/** Values the organization asked for, or the fact that it asked none. */
export function DesiredValues({ values }: { values: Record<string, string> }) {
  const entries = Object.entries(values);
  if (entries.length === 0) {
    return <p className="text-sm text-muted-foreground">No values.</p>;
  }
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 font-mono text-xs">
      {entries.map(([key, value]) => (
        <Fragment key={key}>
          <dt className="text-muted-foreground">{key}</dt>
          <dd className="wrap-anywhere whitespace-pre-wrap">{value}</dd>
        </Fragment>
      ))}
    </dl>
  );
}
