import type { App, InstanceKey } from "@/features/orgs";
import type { StatusTone } from "@/components/status-dot";

/** How many instances an app runs, in the words the console uses. */
export const INSTANCES_LABEL: Record<App["instances"], string> = {
  none: "None",
  one: "One",
  many: "Many",
};

/** Who may see an app, in the words the console uses. */
export const VISIBILITY_LABEL: Record<App["visibility"], string> = {
  private: "Private to its organization",
  public: "Public",
};

/** Where one instance key stands (ADR-0184 decision 7). */
export const KEY_STATUS: Record<
  InstanceKey["state"],
  { tone: StatusTone; label: string }
> = {
  active: { tone: "ok", label: "Active" },
  retiring: { tone: "warn", label: "Retiring" },
  revoked: { tone: "neutral", label: "Revoked" },
};
