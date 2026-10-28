import { Banner } from "@garam/design/components/banner";

/** What stands in for the form where the server denies creating one. */
export function CreatePolicyRefused() {
  return (
    <Banner tone="info" title="You can't create policies here">
      Creating a policy needs the admin role, or a policy that grants it. An
      owner or admin of the organization can create it, or give you access.
    </Banner>
  );
}
