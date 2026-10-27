import type { TaskStatus } from "@/lib/cowork";

/**
 * What each status is called on screen. A plain module, so server and
 * client components read the same object: one exported from a client
 * module reaches a server component as a reference, not as its value.
 */
export const STATUS_LABEL: Record<TaskStatus, string> = {
  todo: "To do",
  doing: "Doing",
  done: "Done",
};
