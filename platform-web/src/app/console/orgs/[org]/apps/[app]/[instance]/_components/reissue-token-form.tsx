"use client";

import { useState } from "react";

import { Button } from "@garam/design/ui/button";
import {
  reissueInstanceToken,
  type ReissueInstanceTokenState,
} from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

import { ActionDialog } from "../../../../../../_components/action-dialog";
import { RegistrationToken } from "../../../_components/registration-token";

const initialState: ReissueInstanceTokenState = { status: "idle" };

/**
 * Answers the instance a new registration token, shown once. The old
 * token is spent at once, and the keys the instance holds are withdrawn
 * when its connector registers with the new one (ADR-0184 decision 7),
 * so the dialog says that before it asks.
 */
export function ReissueTokenForm({
  org,
  installation,
  instance,
  name,
}: {
  org: string;
  installation: string;
  instance: string;
  name: string;
}) {
  const hydrated = useHydrated();
  const [issued, setIssued] = useState<{
    token: string;
    expiresAt: string;
  } | null>(null);

  return (
    <>
      <ActionDialog
        trigger={
          <Button variant="outline" disabled={!hydrated}>
            Reissue registration token
          </Button>
        }
        title="Reissue the registration token?"
        description={
          <>
            <p>
              The token it holds now stops working. When the connector registers
              with the new one, every key {name} holds is withdrawn.
            </p>
            <p>The new token is shown once and is good for about an hour.</p>
          </>
        }
        action={reissueInstanceToken}
        initialState={initialState}
        fields={{ org, installation, instance }}
        confirmLabel="Reissue token"
        pendingLabel="Reissuing…"
        done={(state) => state.status === "reissued"}
        onAnswer={(state) =>
          setIssued(
            state.status === "reissued"
              ? { token: state.registrationToken, expiresAt: state.expiresAt }
              : null,
          )
        }
      />
      <RegistrationToken
        name={name}
        token={issued?.token ?? null}
        expiresAt={issued?.expiresAt}
        onDone={() => setIssued(null)}
      />
    </>
  );
}
