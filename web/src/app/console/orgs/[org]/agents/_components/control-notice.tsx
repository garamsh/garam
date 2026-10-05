import type { HostedOperator } from "@/features/orgs";

/**
 * Why a control service panel cannot act, or null where it can: no
 * operator has a registered endpoint, or its delegation does not carry
 * the operation. Everything else on the page stays available either
 * way; only the panel is disabled.
 */
export function controlBlocker(
  hosted: HostedOperator | null,
  operation: string,
): string | null {
  if (hosted === null) {
    return "No control service is registered for this organization. An administrator registers one on an operator's page.";
  }
  if (hosted.delegation === null) {
    return `${hosted.name} holds no delegation, so its control service is not reachable from here.`;
  }
  if (!hosted.delegation.operations.some((op) => op === operation)) {
    return `${hosted.name}'s delegation does not include ${operation}.`;
  }
  return null;
}

/** The sentence a disabled control panel shows. */
export function ControlNotice({ children }: { children: React.ReactNode }) {
  return (
    <p className="max-w-2xl text-sm text-muted-foreground">
      {children}
    </p>
  );
}

/** What an unanswered control service looks like on every panel. */
export const UNAVAILABLE =
  "The control service is unavailable, so its controls are disabled. Teams, people and agents are unaffected.";
