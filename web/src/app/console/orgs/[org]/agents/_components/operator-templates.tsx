"use client";

import { Plus } from "lucide-react";
import { useState } from "react";

import {
  ControlNotice,
  NO_FIX,
  controlBlocker,
  controlReach,
} from "@/components/control-notice";
import { DataTable, type ColumnDef } from "@/components/data-table";
import { DescriptionList } from "@/components/description-list";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { mintOperationAuthority } from "@/features/orgs/actions";
import { useControlRead } from "@/hooks/use-control-read";
import { controlFailureCopy } from "@/lib/control-refusal-copy";
import type { DelegatedOperation, HostedOperator } from "@/features/orgs";
import {
  ProfilesParser,
  TemplateVersionParser,
  TemplatesParser,
  segment,
  type TemplateVersion,
  type VersionRef,
} from "@/lib/operation-authority";

import { orgHref } from "../../../../_components/navigation";
import {
  PublishTemplateForm,
  startingDraft,
  type PublishDraft,
} from "./publish-template-form";

/** One published template, as the control service lists it. */
type Published = VersionRef & { profile: VersionRef };

/**
 * The templates and profiles a hosted operator's control service has
 * published (#1170). Each version is immutable and an agent copies the
 * one it was created from once, so publishing a new version changes no
 * existing agent. Reading needs `agent-template:read`, publishing
 * `agent-template:publish`, and the profiles `execution-profile:read`;
 * publishing is also left out where the server denies the caller it.
 */
export function OperatorTemplates({
  org,
  orgGrn,
  hosted,
  publishDenied,
}: {
  org: string;
  orgGrn: string;
  hosted: HostedOperator | null;
  publishDenied: boolean;
}) {
  const [generation, setGeneration] = useState(0);
  const [selected, setSelected] = useState<VersionRef | null>(null);
  // The draft lives here rather than in the dialog, so closing the
  // dialog by Escape or a click outside it keeps what was typed.
  const [publishing, setPublishing] = useState(false);
  const [draft, setDraft] = useState<PublishDraft>(() => startingDraft(null));
  const [published, setPublished] = useState<VersionRef | null>(null);
  const base = `/v1/orgs/${segment(org)}`;
  const reach = (operation: DelegatedOperation, path: string) =>
    hosted !== null && controlBlocker(hosted, operation) === null
      ? {
          endpoint: hosted.endpoint,
          audience: hosted.grn,
          path,
          target: orgGrn,
        }
      : null;

  const read = controlReach(hosted, "agent-template:read");
  const templates = useControlRead(
    org,
    "agent-template:read",
    reach("agent-template:read", `${base}/templates`),
    TemplatesParser,
    mintOperationAuthority,
    generation,
  );
  const detail = useControlRead(
    org,
    "agent-template:read",
    selected === null
      ? null
      : reach(
          "agent-template:read",
          `${base}/templates/${segment(selected.name)}/versions/${segment(selected.version)}`,
        ),
    TemplateVersionParser,
    mintOperationAuthority,
  );
  const profiles = useControlRead(
    org,
    "execution-profile:read",
    reach("execution-profile:read", `${base}/profiles`),
    ProfilesParser,
    mintOperationAuthority,
  );

  if (read.status === "blocked") {
    return (
      <ControlNotice
        operatorsHref={orgHref(org, "operators")}
        {...read.block}
      />
    );
  }
  if (templates === null || templates.status === "loading") {
    return (
      <p role="status" className="text-muted-foreground">
        Reading {read.hosted.name}&apos;s templates…
      </p>
    );
  }
  if (templates.status !== "ok") {
    return (
      <ControlNotice
        operatorsHref={orgHref(org, "operators")}
        reason={controlFailureCopy(templates, "agent-template:read")}
        fix={NO_FIX}
      />
    );
  }

  const publish = controlReach(hosted, "agent-template:publish");
  const canPublish = publish.status === "ready" && !publishDenied;
  const profileBlock = controlBlocker(hosted, "execution-profile:read");
  const columns: ColumnDef<Published, unknown>[] = [
    { id: "name", header: "Template", accessorKey: "name" },
    {
      id: "version",
      header: "Version",
      accessorFn: (t) => String(t.version),
      cell: ({ row }) => (
        <span className="tabular-nums">{row.original.version}</span>
      ),
    },
    {
      id: "profile",
      header: "Profile",
      accessorFn: (t) => `${t.profile.name} version ${t.profile.version}`,
    },
    {
      id: "view",
      header: () => <span className="sr-only">Actions</span>,
      enableSorting: false,
      enableGlobalFilter: false,
      cell: ({ row }) => (
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={() =>
            setSelected({
              name: row.original.name,
              version: row.original.version,
            })
          }
        >
          View {row.original.name}
        </Button>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-muted-foreground">
          Published by {read.hosted.name}. Each version is immutable: an agent
          copies the version it is created from once, and a later version never
          changes it.
        </p>
        {canPublish && (
          <Button type="button" onClick={() => setPublishing(true)}>
            <Plus aria-hidden />
            Publish version
          </Button>
        )}
      </div>
      {published !== null && (
        <p role="status" className="text-muted-foreground">
          Published {published.name} version {published.version}. Agents created
          from earlier versions keep what they copied.
        </p>
      )}
      {publish.status === "blocked" && (
        <ControlNotice
          operatorsHref={orgHref(org, "operators")}
          {...publish.block}
        />
      )}
      <DataTable
        label="Published templates"
        columns={columns}
        data={templates.data.templates}
        getRowId={(t) => t.name}
        empty={
          <p className="text-muted-foreground">No template is published.</p>
        }
      />
      <section aria-labelledby="profiles-title" className="flex flex-col gap-1">
        <h3 id="profiles-title" className="font-medium">
          Execution profiles
        </h3>
        {profileBlock !== null ? (
          <ControlNotice
            operatorsHref={orgHref(org, "operators")}
            {...profileBlock}
          />
        ) : profiles === null ||
          profiles.status === "loading" ? null : profiles.status === "ok" ? (
          <p>
            {profiles.data.profiles.length === 0
              ? "No profile is published."
              : `Published: ${profiles.data.profiles
                  .map((p) => `${p.name} version ${p.version}`)
                  .join(", ")}.`}{" "}
            <span className="text-muted-foreground">
              Profiles are published by the deployment, not from the console.
            </span>
          </p>
        ) : (
          <ControlNotice
            operatorsHref={orgHref(org, "operators")}
            reason={controlFailureCopy(profiles, "execution-profile:read")}
            fix={NO_FIX}
          />
        )}
      </section>

      <Dialog
        open={selected !== null}
        onOpenChange={(open) => {
          if (!open) setSelected(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {selected?.name} version {selected?.version}
            </DialogTitle>
            <DialogDescription>
              What this version configures. It cannot change.
            </DialogDescription>
          </DialogHeader>
          {detail === null || detail.status === "loading" ? (
            <p role="status" className="text-muted-foreground">
              Reading the version…
            </p>
          ) : detail.status === "ok" ? (
            <>
              <VersionFacts version={detail.data} />
              {canPublish && (
                <div>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() => {
                      setSelected(null);
                      setDraft(startingDraft(detail.data));
                      setPublishing(true);
                    }}
                  >
                    Start a new version from this
                  </Button>
                </div>
              )}
            </>
          ) : (
            <p role="alert" className="text-danger">
              {controlFailureCopy(detail, "agent-template:read")}
            </p>
          )}
        </DialogContent>
      </Dialog>

      {publish.status === "ready" && (
        <Dialog open={publishing} onOpenChange={setPublishing}>
          <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
            <DialogHeader>
              <DialogTitle>Publish a template version</DialogTitle>
              <DialogDescription>
                The control service numbers the version. A secret is named by
                reference, never typed here.
              </DialogDescription>
            </DialogHeader>
            <PublishTemplateForm
              org={org}
              orgGrn={orgGrn}
              hosted={publish.hosted}
              values={draft}
              onValuesChange={setDraft}
              onPublished={(version) => {
                setPublishing(false);
                setDraft(startingDraft(null));
                setPublished(version);
                setGeneration((g) => g + 1);
              }}
            />
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

/** One template version's configuration, its secrets by reference. */
function VersionFacts({ version }: { version: TemplateVersion }) {
  const { model, tools } = version.configuration;
  return (
    <DescriptionList
      items={[
        {
          term: "Profile",
          description: `${version.profile.name} version ${version.profile.version}`,
        },
        { term: "Model", description: `${model.provider} · ${model.name}` },
        {
          term: "API key",
          description: (
            <span className="font-mono">reference {model.apiKeyRef}</span>
          ),
        },
        {
          term: "Embedding",
          description:
            model.embedding === undefined ? "None" : model.embedding.name,
        },
        {
          term: "Tools",
          description: Object.keys(tools).join(", ") || "None",
        },
      ]}
    />
  );
}
