"use client";

import { useActionState, useState } from "react";

import { Refusal } from "@garam/design/components/refusal";
import { Button } from "@garam/design/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@garam/design/ui/dialog";
import { installApp, type InstallAppState } from "@/features/orgs/actions";
import type { App } from "@/features/orgs";
import { useHydrated } from "@/hooks/use-hydrated";
import { refuseUnsent } from "@/lib/unsent-action";

import { INSTANCES_LABEL, VISIBILITY_LABEL } from "./app-labels";

/** What the dialog shows of an app before it is installed. */
export type InstallableApp = Pick<
  App,
  | "slug"
  | "name"
  | "description"
  | "visibility"
  | "requestedActions"
  | "requestsBotHosting"
  | "instances"
>;

const initialState: InstallAppState = { status: "idle" };

// A call garam's answer never came back from stays in the dialog as a
// refusal rather than closing it as if cancelled (#1631).
const install = refuseUnsent(
  installApp,
  "garam didn't answer, so the app may or may not have been installed. Check the app list before trying again.",
);

/**
 * Installs one app, after the owner has read what installing does
 * (ADR-0192 decision 4): the actions it requests, each of which becomes
 * a permit the owner writes by installing, and the scope those permits
 * cover. Nothing is written until the button that names the act is
 * pressed, and installing sends the page of the installation.
 */
export function InstallDialog({
  org,
  app,
  projects,
}: {
  org: string;
  app: InstallableApp;
  projects: { grn: string; name: string }[];
}) {
  const [open, setOpen] = useState(false);
  const hydrated = useHydrated();

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button
          variant="outline"
          size="sm"
          disabled={!hydrated}
          aria-label={`Install ${app.name}`}
        >
          Install
        </Button>
      </DialogTrigger>
      <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg">
        <InstallForm
          org={org}
          app={app}
          projects={projects}
          onCancel={() => setOpen(false)}
        />
      </DialogContent>
    </Dialog>
  );
}

function InstallForm({
  org,
  app,
  projects,
  onCancel,
}: {
  org: string;
  app: InstallableApp;
  projects: { grn: string; name: string }[];
  onCancel: () => void;
}) {
  const [state, formAction, pending] = useActionState(install, initialState);
  const hydrated = useHydrated();
  const [scope, setScope] = useState<"organization" | "projects">(
    "organization",
  );
  const [chosen, setChosen] = useState<string[]>([]);
  const incomplete = scope === "projects" && chosen.length === 0;

  return (
    <form action={formAction} className="flex flex-col gap-5">
      <DialogHeader>
        <DialogTitle>Install {app.name}</DialogTitle>
        <DialogDescription>
          {app.description || "This app has no description."}
        </DialogDescription>
      </DialogHeader>
      <input type="hidden" name="org" value={org} />
      <input type="hidden" name="app" value={app.slug} />
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
        <dt className="text-muted-foreground">App</dt>
        <dd className="font-mono">{app.slug}</dd>
        <dt className="text-muted-foreground">Visibility</dt>
        <dd>{VISIBILITY_LABEL[app.visibility]}</dd>
        <dt className="text-muted-foreground">Instances</dt>
        <dd>{INSTANCES_LABEL[app.instances]}</dd>
      </dl>
      <section
        className="flex flex-col gap-2"
        aria-labelledby="install-actions"
      >
        <h3 id="install-actions" className="font-medium">
          Actions it requests
        </h3>
        {app.requestedActions.length === 0 ? (
          <p className="text-muted-foreground">
            It requests no actions, so installing writes no permission.
          </p>
        ) : (
          <>
            <p className="text-muted-foreground">
              Installing writes one permission for each, as you, over the scope
              below.
            </p>
            <ul className="flex flex-col gap-1">
              {app.requestedActions.map((action) => (
                <li key={action} className="font-mono text-xs">
                  {action}
                </li>
              ))}
            </ul>
          </>
        )}
        {app.requestsBotHosting && (
          <p className="text-muted-foreground">
            It asks to host bots. Installing does not grant that: it is granted
            separately.
          </p>
        )}
      </section>
      <fieldset className="flex flex-col gap-2">
        <legend className="mb-1 font-medium">Scope</legend>
        <label className="flex items-center gap-2.5 rounded-lg border px-3 py-2 has-checked:border-primary">
          <input
            type="radio"
            name="scope"
            value="organization"
            checked={scope === "organization"}
            onChange={() => setScope("organization")}
            className="size-4 accent-primary"
          />
          The whole organization
        </label>
        <label className="flex items-center gap-2.5 rounded-lg border px-3 py-2 has-checked:border-primary">
          <input
            type="radio"
            name="scope"
            value="projects"
            checked={scope === "projects"}
            onChange={() => setScope("projects")}
            className="size-4 accent-primary"
          />
          Chosen projects
        </label>
        {scope === "projects" &&
          (projects.length === 0 ? (
            <p className="text-muted-foreground">
              The organization has no project.
            </p>
          ) : (
            <ul className="flex flex-col gap-1.5 pl-1">
              {projects.map((project) => (
                <li key={project.grn}>
                  <label className="flex items-center gap-2.5">
                    <input
                      type="checkbox"
                      name="projects"
                      value={project.grn}
                      checked={chosen.includes(project.grn)}
                      onChange={(event) =>
                        setChosen((current) =>
                          event.target.checked
                            ? [...current, project.grn]
                            : current.filter((grn) => grn !== project.grn),
                        )
                      }
                      className="size-4 accent-primary"
                    />
                    {project.name}
                  </label>
                </li>
              ))}
            </ul>
          ))}
      </fieldset>
      {state.status === "error" && <Refusal>{state.message}</Refusal>}
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={!hydrated || pending || incomplete}>
          {pending ? "Installing…" : `Install ${app.name}`}
        </Button>
      </DialogFooter>
    </form>
  );
}
