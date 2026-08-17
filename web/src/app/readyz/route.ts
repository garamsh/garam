/**
 * Readiness: whether to send this process traffic. It reads nothing —
 * an API outage leaves the console serving so it can report the outage
 * (ADR-0023) — so it is the same answer liveness gives.
 */
export { probe as GET } from "@/lib/probe";
