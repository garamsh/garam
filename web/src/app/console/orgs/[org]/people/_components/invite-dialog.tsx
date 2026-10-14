"use client";

import { UserPlus } from "lucide-react";
import { useActionState, useState } from "react";

import { CopyField } from "@/components/copy-field";
import { FormField } from "@/components/form-field";
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

const initialState: CreateInvitationState = { status: "idle" };

/**
 * Invites a person: the role they join as, then the link that admits
 * them, to copy and send. garam sends no email, so the link is the
 * invitation. It works once, and closing the dialog leaves it behind.
 *
 * The select stands outside the `<form>` and the role is submitted
 * from a hidden input: the select returns to its first value on a
 * reset of the form holding it, and React resets a form after its
 * action, so the role chosen is the one shown and sent after a refusal.
 */
export function InviteDialog({ org }: { org: string }) {
  const [state, formAction, pending] = useActionState(
    createInvitation,
    initialState,
  );
  const [role, setRole] = useState("member");
  const link =
    state.status === "created"
      ? `${window.location.origin}/console/invitations/${state.token}`
      : null;

  return (
    <Dialog>
      <DialogTrigger asChild>
        <Button>
          <UserPlus />
          Invite people
        </Button>
      </DialogTrigger>
      {/* One track no wider than the dialog: the link's full length
          would otherwise set it and push the content off a phone. */}
      <DialogContent className="grid-cols-[minmax(0,1fr)] sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Invite a person</DialogTitle>
          <DialogDescription>
            Choose the role they join as, then send them the link.
          </DialogDescription>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          <FormField
            label="Role"
            error={state.status === "error" ? state.message : undefined}
          >
            {(control) => (
              <Select value={role} onValueChange={setRole}>
                <SelectTrigger {...control} className="w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="member">Member</SelectItem>
                  <SelectItem value="admin">Admin</SelectItem>
                </SelectContent>
              </Select>
            )}
          </FormField>
          <form action={formAction}>
            <input type="hidden" name="org" value={org} />
            <input type="hidden" name="role" value={role} />
            <Button type="submit" disabled={pending}>
              {pending ? "Creating…" : "Create invitation"}
            </Button>
          </form>
        </div>
        {link && (
          <div className="flex flex-col gap-2 border-t pt-4">
            <CopyField label="Invitation link" value={link} />
            <p className="text-muted-foreground">
              Send this link to the person you invited. It works once and then
              expires.
            </p>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
