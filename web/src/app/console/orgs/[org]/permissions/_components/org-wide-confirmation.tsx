import { Label } from "@/components/ui/label";

/**
 * The checkbox a write that would apply a policy to the whole
 * organization is not sent without. It posts `orgWide=confirmed`,
 * which the Server Action requires where it finds no attachment.
 */
export function OrgWideConfirmation({ id }: { id: string }) {
  return (
    <div className="flex items-start gap-2">
      <input
        id={id}
        type="checkbox"
        name="orgWide"
        value="confirmed"
        required
        className="mt-0.5 size-4 accent-primary"
      />
      <Label htmlFor={id} className="leading-snug font-normal">
        Apply it to everyone in this organization
      </Label>
    </div>
  );
}
