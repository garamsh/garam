import { garamFetch } from "@/lib/garam";

/**
 * Returns the sign-in providers the backend has enabled. The login
 * page renders an option per name; `dev` appears only when the backend
 * runs with GARAM_DEV_AUTH=true (ADR-0010). A backend that cannot
 * answer yields no names rather than a rejection.
 */
export async function listProviders(): Promise<string[]> {
  try {
    return await garamFetch<string[]>("/auth/providers");
  } catch (error) {
    console.error("listing the sign-in providers failed", error);
    return [];
  }
}
