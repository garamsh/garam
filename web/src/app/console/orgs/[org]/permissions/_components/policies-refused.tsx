/**
 * What a Permissions page shows where garam refused the caller the
 * organization's policies: a refusal inside an organization the caller
 * reads, not a missing one and not an empty list.
 */
export function PoliciesRefused() {
  return (
    <section role="alert" className="flex flex-col gap-1">
      <h2 className="font-heading text-lg font-semibold">
        You cannot view this organization&apos;s policies
      </h2>
      <p className="text-sm text-muted-foreground">
        garam refused to list them to you. Reading policies needs the admin role
        or a policy that grants it.
      </p>
    </section>
  );
}
