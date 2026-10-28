import "server-only";

import { redirect } from "next/navigation";

import { garamClient } from "@garam/platform-sdk-ts/garam";

import { env } from "@/env";

/**
 * The site's client of the platform: the calls `@garam/platform-sdk-ts`
 * makes, bound to the site's backend address. The site is where a
 * person signs in, so a session the backend rejects lands on its own
 * sign-in page. Everything a page or a Server Action needs of the
 * platform is imported from here.
 */
export const { garamRequest, garamFetch } = garamClient({
  apiUrl: env.GARAM_API_URL,
  redirectToSignIn: async () => redirect("/login"),
});

export {
  GaramApiError,
  GaramContractError,
  GaramUnreachableError,
  isUnauthenticated,
  parserFor,
  refusal,
} from "@garam/platform-sdk-ts/garam";
