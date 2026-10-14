import { Banner } from "@/components/banner";
import { Label } from "@/components/ui/label";

/**
 * The checkbox a write that would or may apply a policy to the whole
 * organization is not sent without. It posts `orgWide=confirmed`,
 * which the Server Action requires where it finds no attachment or is
 * refused the list of them.
 */
export function OrgWideConfirmation({ id }: { id: string }) {
  return (
    <div className="flex items-start gap-2 rounded-lg border border-warn/40 bg-warn/10 px-3 py-2.5">
      <input
        id={id}
        type="checkbox"
        name="orgWide"
        value="confirmed"
        required
        className="mt-0.5 size-4 shrink-0 accent-primary"
      />
      <Label htmlFor={id} className="leading-snug font-normal">
        Apply it to everyone in this organization
      </Label>
    </div>
  );
}

/**
 * What garam's own read of the scope found when the page had not asked
 * for the confirmation: no attachment, or a list it would not show.
 * The two are said apart, since an unread list is not an empty one.
 */
export function ScopeConfirmationNotice({
  state,
}: {
  state: { status: string };
}) {
  switch (state.status) {
    case "confirm-org-wide":
      return (
        <Banner tone="danger" title="This reaches everyone">
          It has no attachments now, so it applies to everyone in this
          organization.
        </Banner>
      );
    case "confirm-unknown-scope":
      return (
        <Banner tone="danger" title="This may reach everyone">
          Its attachments are not listed to you, so it may apply to everyone in
          this organization.
        </Banner>
      );
    default:
      return null;
  }
}
