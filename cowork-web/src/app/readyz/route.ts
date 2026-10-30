/**
 * Readiness: whether to send this process traffic. It reads nothing, so
 * an outage behind this app takes no replica out of rotation (ADR-0023).
 */
export { probe as GET } from "@/lib/probe";
