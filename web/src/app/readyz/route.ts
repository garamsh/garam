/**
 * Readiness: whether to send this process traffic. It reads nothing, so
 * an API outage takes no replica out of rotation (ADR-0023), and it is
 * the same answer liveness gives.
 */
export { probe as GET } from "@/lib/probe";
