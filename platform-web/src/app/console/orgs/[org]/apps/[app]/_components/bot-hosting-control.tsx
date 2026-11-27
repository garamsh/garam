"use client";

import { Button } from "@garam/design/ui/button";
import {
  grantInstallationBotHosting,
  withdrawInstallationBotHosting,
  type InstallationBotHostingState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

import { ActionDialog } from "../../../../../_components/action-dialog";

const initialState: InstallationBotHostingState = { status: "idle" };

/**
 * Grants or withdraws the installation's bot hosting (ADR-0192 decision
 * 10), a separate act from installing. Granting is offered only where
 * the app asks to host bots; withdrawing says first that the
 * installation's live bot tokens are revoked and its existing
 * declarations stay.
 */
export function BotHostingControl({
  org,
  installation,
  name,
  granted,
}: {
  org: string;
  installation: string;
  name: string;
  granted: boolean;
}) {
  const hydrated = useHydrated();
  const fields = { org, installation };

  return granted ? (
    <ActionDialog
      trigger={
        <Button variant="outline" size="sm" disabled={!hydrated}>
          Withdraw bot hosting
        </Button>
      }
      title={`Withdraw bot hosting from ${name}?`}
      description={
        <>
          <p>
            The installation&apos;s live bot tokens are revoked, so its
            instances stop acting as bots from now on.
          </p>
          <p>
            The declarations already placed on its instances stay until the
            organization tombstones them. Granting hosting again does not bring
            the tokens back.
          </p>
        </>
      }
      action={withdrawInstallationBotHosting}
      initialState={initialState}
      fields={fields}
      confirmLabel="Withdraw bot hosting"
      pendingLabel="Withdrawing…"
      destructive
      done={(state) => state.status === "withdrawn"}
    />
  ) : (
    <ActionDialog
      trigger={
        <Button variant="outline" size="sm" disabled={!hydrated}>
          Grant bot hosting
        </Button>
      }
      title={`Grant bot hosting to ${name}?`}
      description={
        <p>
          The installation&apos;s instances may then be the target of bot
          declarations and act as bots. Withdrawing it later revokes their live
          bot tokens.
        </p>
      }
      action={grantInstallationBotHosting}
      initialState={initialState}
      fields={fields}
      confirmLabel="Grant bot hosting"
      pendingLabel="Granting…"
      done={(state) => state.status === "granted"}
    />
  );
}
