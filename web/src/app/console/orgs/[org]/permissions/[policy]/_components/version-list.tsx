import { Badge } from "@/components/ui/badge";
import type { PolicyVersion } from "@/features/orgs";
import { formatPolicyDocument } from "@/lib/policy-document";

import { MakeDefaultForm } from "./make-default-form";

/**
 * A policy's versions, newest first, each with its document behind a
 * disclosure. The default is marked; every other version can be made
 * the default.
 */
export function VersionList({
  org,
  policy,
  versions,
  defaultVersion,
  authorOf,
  active,
  reach,
  orgWide,
}: {
  org: string;
  policy: string;
  versions: PolicyVersion[];
  defaultVersion: number;
  /** How the person who saved a version is named. */
  authorOf: (version: PolicyVersion) => string;
  active: boolean;
  reach: string;
  orgWide: boolean;
}) {
  return (
    <ol className="flex flex-col divide-y rounded-lg border">
      {[...versions].reverse().map((version) => (
        <li
          key={version.version}
          aria-label={`Version ${version.version}`}
          className="flex flex-col gap-3 p-4"
        >
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="flex flex-col gap-1">
              <div className="flex items-center gap-2">
                <span className="font-medium">Version {version.version}</span>
                {version.version === defaultVersion && (
                  <Badge variant="secondary">Default</Badge>
                )}
              </div>
              <span className="text-xs text-muted-foreground">
                Saved{" "}
                <time dateTime={version.createdAt}>
                  {version.createdAt.slice(0, 10)}
                </time>{" "}
                by {authorOf(version)}
              </span>
            </div>
            {version.version !== defaultVersion && (
              <MakeDefaultForm
                org={org}
                policy={policy}
                current={defaultVersion}
                target={version.version}
                active={active}
                reach={reach}
                orgWide={orgWide}
              />
            )}
          </div>
          <details>
            <summary className="cursor-pointer text-sm text-muted-foreground">
              Document
            </summary>
            <pre className="mt-2 overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs">
              {formatPolicyDocument(version.document)}
            </pre>
          </details>
        </li>
      ))}
    </ol>
  );
}
