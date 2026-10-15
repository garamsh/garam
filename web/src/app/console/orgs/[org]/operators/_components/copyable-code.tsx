"use client";

import { Check, Copy } from "lucide-react";

import { Button } from "@/components/ui/button";
import { useCopy } from "@/hooks/use-copy";

/**
 * A value a person compares or pastes elsewhere — a key's fingerprint,
 * an image's digest or reference — shown as `shown` and copied whole.
 * `label` names it for the copy button: "Copy {label}".
 */
export function CopyableCode({
  value,
  shown,
  label,
}: {
  value: string;
  shown: string;
  label: string;
}) {
  const { outcome, copy } = useCopy(value);
  return (
    <span className="flex min-w-0 items-center gap-1">
      <code className="font-mono text-xs wrap-anywhere" title={value}>
        {shown}
      </code>
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        aria-label={`Copy ${label}`}
        onClick={copy}
      >
        {outcome === "copied" ? <Check className="text-ok" /> : <Copy />}
      </Button>
    </span>
  );
}
