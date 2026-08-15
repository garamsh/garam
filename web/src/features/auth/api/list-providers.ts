import { garamFetch } from "@/lib/garam";

/**
 * Returns the sign-in providers the backend has enabled. The login
 * page renders an option per name; `dev` appears only when the backend
 * runs with GARAM_DEV_AUTH=true (ADR-0010).
 */
export async function listProviders(): Promise<string[]> {
  return garamFetch<string[]>("/auth/providers");
}
