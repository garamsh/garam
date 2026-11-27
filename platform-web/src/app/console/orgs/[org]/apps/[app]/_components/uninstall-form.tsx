"use client";

import { Button } from "@garam/design/ui/button";
import { uninstallApp, type UninstallAppState } from "@/features/orgs/actions";
import { useHydrated } from "@/hooks/use-hydrated";

import { ActionDialog } from "../../../../../_components/action-dialog";

const initialState: UninstallAppState = { status: "idle" };

/**
 * Uninstalls one app, which nothing undoes (ADR-0192 decision 4): its
 * instances, their keys and tokens, and every policy that names the
 * installation or an instance go with it, and installing it again
 * inherits none of them. The person types the app's slug, and the
 * dialog says what goes and what does not: nothing in the instances
 * themselves is touched.
 */
export function UninstallForm({
  org,
  installation,
  name,
}: {
  org: string;
  installation: string;
  name: string;
}) {
  const hydrated = useHydrated();

  return (
    <ActionDialog
      trigger={
        <Button variant="destructive" disabled={!hydrated}>
          Uninstall app
        </Button>
      }
      title={`Uninstall ${name}?`}
      description={
        <>
          <p>
            This deletes the installation, its instances with their keys and
            registration tokens, and every policy that names the installation or
            one of its instances.
          </p>
          <p>
            Installing {name} again starts from none of them. Nothing running in
            an instance is touched: it can no longer reach garam.
          </p>
        </>
      }
      action={uninstallApp}
      initialState={initialState}
      fields={{ org, installation }}
      command={{
        label: "Uninstall app",
        consequence:
          "Deletes the installation, its instances and every policy naming them.",
      }}
      confirmLabel="Uninstall app"
      pendingLabel="Uninstalling…"
      destructive
      typed={{ phrase: installation, mono: true }}
      done={(state) => state.status !== "error"}
    />
  );
}
