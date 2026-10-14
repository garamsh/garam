"use client";

import { Plus } from "lucide-react";
import { useActionState, useState } from "react";

import { FormField } from "@/components/form-field";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  grantOrgConsent,
  type GrantOrgConsentState,
} from "@/features/orgs/actions";

const initialState: GrantOrgConsentState = { status: "idle" };

/**
 * The list's primary action: a dialog that grants this organization's
 * half of a pair, named by the identifier the other organization is
 * addressed by. It opens a pair nobody has answered yet; a pair the
 * other side already holds is granted from its own card.
 *
 * A refusal stays in the dialog, attached to the field, with the
 * identifier still typed. A grant closes it, and what it changed is
 * said beside the button that opened it: the pair's state, not this
 * half's, since a granted half waits for the other (ADR-0035).
 */
export function GrantConsentDialog({ org }: { org: string }) {
  const [open, setOpen] = useState(false);
  // Controlled, so the reset React runs after the action keeps it.
  const [other, setOther] = useState("");
  const [state, formAction, pending] = useActionState(
    async (previous: GrantOrgConsentState, formData: FormData) => {
      const next = await grantOrgConsent(previous, formData);
      if (next.status === "granted") {
        setOpen(false);
        setOther("");
      }
      return next;
    },
    initialState,
  );

  return (
    <div className="flex flex-wrap items-center justify-end gap-x-3 gap-y-1">
      {state.status === "granted" && !open && (
        <p role="status" className="text-muted-foreground">
          {state.stands
            ? "Granted. Messages now cross with the other organization."
            : "Granted. Messages stay refused until the other organization grants its own half."}
        </p>
      )}
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogTrigger asChild>
          <Button>
            <Plus aria-hidden />
            New consent
          </Button>
        </DialogTrigger>
        <DialogContent>
          <form action={formAction} className="flex flex-col gap-4">
            <DialogHeader>
              <DialogTitle>Grant consent</DialogTitle>
              <DialogDescription>
                Granting stands this organization&apos;s half of the pair and
                nothing more. Messages cross once the other organization has
                granted its own.
              </DialogDescription>
            </DialogHeader>
            <input type="hidden" name="org" value={org} />
            <FormField
              label="Other organization"
              hint="The identifier the other organization is addressed by."
              error={state.status === "error" ? state.message : undefined}
            >
              {(control) => (
                <Input
                  {...control}
                  name="other"
                  required
                  autoComplete="off"
                  spellCheck={false}
                  placeholder="globex"
                  className="font-mono"
                  value={other}
                  onChange={(event) => setOther(event.target.value)}
                />
              )}
            </FormField>
            <DialogFooter>
              <Button type="submit" disabled={pending}>
                {pending ? "Granting…" : "Grant consent"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
