import { ShieldOff } from "lucide-react";

import { EmptyState } from "../../../../_components/empty-state";

/**
 * What a Permissions page shows where garam refused the caller the
 * organization's policies: a refusal inside an organization the caller
 * reads, not a missing one and not an empty list.
 */
export function PoliciesRefused() {
  return (
    <div role="alert">
      <EmptyState
        icon={ShieldOff}
        title="You can't view this organization's policies"
        description="Reading policies needs the admin role, or a policy that grants it. An owner or admin of the organization can give you access."
      />
    </div>
  );
}
