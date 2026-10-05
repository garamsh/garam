"use client";

import { useState } from "react";

import { Button } from "@/components/ui/button";
import type { HostedOperator } from "@/features/orgs";
import {
  ProfilesParser,
  TemplateVersionParser,
  TemplatesParser,
  segment,
  type VersionRef,
} from "@/lib/operation-authority";

import { ControlNotice, UNAVAILABLE, controlBlocker } from "./control-notice";
import { PublishTemplateForm } from "./publish-template-form";
import { useControlRead } from "./use-control-read";

/**
 * The templates and profiles a hosted operator's control service has
 * published (#1170). Each version is immutable and an agent copies the
 * one it was created from once, so publishing a new version changes no
 * existing agent. Reading needs `agent-template:read`, publishing
 * `agent-template:publish`, and the profiles `execution-profile:read`.
 */
export function OperatorTemplates({
  org,
  orgGrn,
  hosted,
}: {
  org: string;
  orgGrn: string;
  hosted: HostedOperator | null;
}) {
  const [generation, setGeneration] = useState(0);
  const [selected, setSelected] = useState<VersionRef | null>(null);
  const base = `/v1/orgs/${segment(org)}`;
  const reach = (operation: string, path: string) =>
    hosted !== null && controlBlocker(hosted, operation) === null
      ? {
          endpoint: hosted.endpoint,
          audience: hosted.grn,
          path,
          target: orgGrn,
        }
      : null;

  const blocker = controlBlocker(hosted, "agent-template:read");
  const templates = useControlRead(
    org,
    "agent-template:read",
    reach("agent-template:read", `${base}/templates`),
    TemplatesParser,
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
  );
  const profiles = useControlRead(
    org,
    "execution-profile:read",
    reach("execution-profile:read", `${base}/profiles`),
    ProfilesParser,
  );

  if (blocker !== null || hosted === null) {
    return <ControlNotice>{blocker}</ControlNotice>;
  }
  if (templates === null || templates.status === "loading") {
    return (
      <ControlNotice>Reading {hosted.name}&apos;s templates…</ControlNotice>
    );
  }
  if (templates.status === "unavailable") {
    return <ControlNotice>{UNAVAILABLE}</ControlNotice>;
  }
  if (templates.status === "refused") {
    return (
      <p role="alert" className="text-sm text-destructive">
        {templates.message}
      </p>
    );
  }

  const canPublish = controlBlocker(hosted, "agent-template:publish") === null;
  return (
    <div className="flex flex-col gap-4">
      <p className="text-sm text-muted-foreground">
        Published by {hosted.name}. Each version is immutable: an agent copies
        the version it is created from once, and a later version never changes
        it.
      </p>
      {templates.data.templates.length === 0 ? (
        <p className="text-sm">No template is published.</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {templates.data.templates.map((t) => (
            <li key={t.name} className="flex flex-wrap items-center gap-3">
              <span className="font-medium">{t.name}</span>
              <span className="text-sm text-muted-foreground">
                version {t.version}, profile {t.profile.name} version{" "}
                {t.profile.version}
              </span>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() =>
                  setSelected({ name: t.name, version: t.version })
                }
              >
                View {t.name}
              </Button>
            </li>
          ))}
        </ul>
      )}
      {detail !== null && detail.status === "ok" && (
        <section
          aria-label={`${detail.data.name} version ${detail.data.version}`}
          className="flex flex-col gap-1 text-sm"
        >
          <h3 className="font-medium">
            {detail.data.name} version {detail.data.version}
          </h3>
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
            <dt className="text-muted-foreground">Model</dt>
            <dd>
              {detail.data.configuration.model.provider} ·{" "}
              {detail.data.configuration.model.name}
            </dd>
            <dt className="text-muted-foreground">API key</dt>
            <dd className="font-mono">
              reference {detail.data.configuration.model.apiKeyRef}
            </dd>
            <dt className="text-muted-foreground">Tools</dt>
            <dd>
              {Object.keys(detail.data.configuration.tools).join(", ") ||
                "none"}
            </dd>
          </dl>
        </section>
      )}
      {detail !== null && detail.status === "refused" && (
        <p role="alert" className="text-sm text-destructive">
          {detail.message}
        </p>
      )}
      <section className="flex flex-col gap-1">
        <h3 className="text-sm font-medium">Execution profiles</h3>
        {profiles === null ? (
          <ControlNotice>
            {controlBlocker(hosted, "execution-profile:read")}
          </ControlNotice>
        ) : profiles.status === "ok" ? (
          <p className="text-sm">
            {profiles.data.profiles.length === 0
              ? "No profile is published."
              : `Published: ${profiles.data.profiles
                  .map((p) => `${p.name} version ${p.version}`)
                  .join(", ")}.`}{" "}
            Profiles are published by the deployment, not from the console.
          </p>
        ) : profiles.status === "loading" ? null : (
          <ControlNotice>
            {profiles.status === "unavailable" ? UNAVAILABLE : profiles.message}
          </ControlNotice>
        )}
      </section>
      {canPublish ? (
        <PublishTemplateForm
          key={
            detail?.status === "ok"
              ? `${detail.data.name}/${detail.data.version}`
              : ""
          }
          org={org}
          orgGrn={orgGrn}
          hosted={hosted}
          from={detail?.status === "ok" ? detail.data : null}
          onPublished={() => setGeneration((g) => g + 1)}
        />
      ) : (
        <ControlNotice>
          {controlBlocker(hosted, "agent-template:publish")}
        </ControlNotice>
      )}
    </div>
  );
}
