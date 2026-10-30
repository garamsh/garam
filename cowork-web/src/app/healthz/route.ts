/**
 * Liveness: whether to restart this process. A route handler, not a
 * page, so navigation is not a deployment interface (ADR-0023).
 */
export { probe as GET } from "@/lib/probe";
