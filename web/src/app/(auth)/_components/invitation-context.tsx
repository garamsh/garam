import {
  InvitationSummary,
  getInvitationPreview,
  unusableInvitation,
  type InvitationPreview,
} from "@/features/orgs";
import { invitationTokenOf } from "@/lib/return-path";

/**
 * The invitation a sign-in shell page was reached for: one that can
 * still be accepted, with its token, or the reason one cannot.
 */
export type InvitationContextRead =
  | ({ usable: true; token: string } & InvitationPreview)
  | { usable: false; title: string; body: string };

/**
 * Reads the invitation `returnPath` leads to, or null where it leads to
 * none or garam did not answer — the page then renders as it would
 * without one, and the invitation page says the rest once signed in.
 */
export async function readInvitationContext(
  returnPath: string | null,
): Promise<InvitationContextRead | null> {
  const token = invitationTokenOf(returnPath);
  if (token === null) return null;
  const read = await getInvitationPreview(token);
  const unusable = unusableInvitation(read);
  if (unusable !== null) return { usable: false, ...unusable };
  if (read.status !== "found") return null;
  return { usable: true, token, ...read.invitation };
}

/**
 * The invitation as the sign-in shell shows it above its form: the
 * organization and role it admits to, or why it cannot be used.
 */
export function InvitationContext({
  context,
}: {
  context: InvitationContextRead;
}) {
  if (context.usable) return <InvitationSummary invitation={context} />;
  return (
    <div
      role="status"
      className="flex flex-col gap-1 rounded-lg border border-border bg-muted/40 p-3"
    >
      <p className="text-sm font-medium">{context.title}</p>
      <p className="text-sm text-pretty text-muted-foreground">
        {context.body}
      </p>
    </div>
  );
}
