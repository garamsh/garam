"use client";

import { UserPlus } from "lucide-react";
import { useActionState, useState } from "react";

import { FormField } from "@/components/form-field";
import { SecretReveal } from "@/components/secret-reveal";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  createInvitation,
  type CreateInvitationState,
} from "@/features/orgs/actions";
import { refuseUnsent } from "@/lib/unsent-action";

import { ROLE_LABEL } from "../../../../_components/org-role";

const initialState: CreateInvitationState = { status: "idle" };

// A request that never reached garam stays in the dialog (#1631).
const invite = refuseUnsent(createInvitation);

/** The roles an invitation can carry; owners are not invited. */
type InvitedRole = "member" | "admin";

/**
 * Invites a person: the role they join as, then the link that admits
 * them. garam sends no email, so the link is the invitation, and it is
 * a credential shown once (ADR-0103 §Patterns): it is revealed in
 * `SecretReveal` and never kept past the dialog that issued it.
 */
export function InviteDialog({ org }: { org: string }) {
  const [open, setOpen] = useState(false);

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button>
          <UserPlus />
          Invite people
        </Button>
      </DialogTrigger>
      <DialogContent className="grid-cols-[minmax(0,1fr)] sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Invite a person</DialogTitle>
          <DialogDescription>The link is shown once.</DialogDescription>
        </DialogHeader>
        <InviteForm org={org} onDone={() => setOpen(false)} />
      </DialogContent>
    </Dialog>
  );
}

/**
 * The dialog's form, mounted only while the dialog is open, so an
 * issued link and a refusal both go when it closes. The link is shown
 * in `SecretReveal`, which holds the form shut until it is kept and
 * then closes the dialog: the role cannot change under a shown link.
 *
 * The role starts unchosen and is sent from a hidden input in the form.
 * React resets the form after its action, and the select answers a
 * reset with its empty default, which is no choice and is ignored: a
 * refused role stays chosen.
 */
function InviteForm({ org, onDone }: { org: string; onDone: () => void }) {
  const [state, formAction, pending] = useActionState(invite, initialState);
  const [role, setRole] = useState<InvitedRole | "">("");
  const issued =
    state.status === "created" && role !== ""
      ? { token: state.token, role }
      : null;

  return (
    <>
      <form action={formAction} className="flex flex-col gap-4">
        <input type="hidden" name="org" value={org} />
        <input type="hidden" name="role" value={role} />
        <FormField
          label="Role"
          error={state.status === "error" ? state.message : undefined}
        >
          {(control) => (
            <Select
              value={role}
              onValueChange={(value) => {
                // The form's reset answers with the empty default; no
                // option is empty, so that is not a choice.
                if (value !== "member" && value !== "admin") return;
                setRole(value);
              }}
            >
              <SelectTrigger {...control} className="w-40">
                <SelectValue placeholder="Choose a role" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="member">{ROLE_LABEL.member}</SelectItem>
                <SelectItem value="admin">{ROLE_LABEL.admin}</SelectItem>
              </SelectContent>
            </Select>
          )}
        </FormField>
        <div>
          <Button type="submit" disabled={pending}>
            {pending ? "Creating…" : "Create invitation"}
          </Button>
        </div>
      </form>
      <SecretReveal
        open={issued !== null}
        title="Invitation link"
        description={
          issued && `Admits one person as ${ROLE_LABEL[issued.role]}, once.`
        }
        secret={
          issued
            ? `${window.location.origin}/console/invitations/${issued.token}`
            : ""
        }
        noun="invitation link"
        onDone={onDone}
      />
    </>
  );
}
