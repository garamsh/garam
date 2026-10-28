import type { StatusTone } from "@/components/status-dot";
import type { OperatorKey } from "@/features/orgs";

/** A status as a dot's tone and the words beside it. */
export type Status = { tone: StatusTone; label: string };

/** Where one key stands (ADR-0102). */
export const KEY_STATUS: Record<OperatorKey["state"], Status> = {
  active: { tone: "ok", label: "Active" },
  retiring: { tone: "warn", label: "Retiring" },
  revoked: { tone: "neutral", label: "Revoked" },
};

/**
 * How many keys an operator holds live at once: a planned rotation adds
 * one beside the key in use, and garam refuses a third (ADR-0102).
 */
export const MAX_LIVE_KEYS = 2;

/** The keys that still verify assertions. */
export function liveKeys(keys: readonly OperatorKey[]): OperatorKey[] {
  return keys.filter((key) => key.state !== "revoked");
}

/**
 * Where an operator stands in authenticating with a key, from its keys
 * alone: none live, live but never exchanged, or exchanged. garam never
 * dials an operator, so a key's last exchange is the one sign of it
 * this page has.
 */
export function keyConnection(keys: readonly OperatorKey[]): Status {
  const live = liveKeys(keys);
  if (live.length === 0) return { tone: "neutral", label: "No key" };
  if (live.some((key) => key.lastExchangedAt !== undefined)) {
    return { tone: "ok", label: "Key in use" };
  }
  return { tone: "info", label: "Waiting for first exchange" };
}
