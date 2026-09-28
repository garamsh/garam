import Link from "next/link";

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
