import "server-only";

import { garamClient } from "@garam/platform-sdk-ts/garam";

import { env } from "@/env";
import { redirectToSignIn } from "@/lib/sign-in-redirect";

/**
 * The console's client of the platform: the calls
 * `@garam/platform-sdk-ts` makes, bound to the console's backend
 * address and its sign-in redirect. Everything a page or a Server
 * Action needs of the platform is imported from here.
 */
export const { garamRequest, garamFetch, garamFetchAuthenticated } =
  garamClient({ apiUrl: env.GARAM_API_URL, redirectToSignIn });

export {
  GaramApiError,
  GaramContractError,
  GaramUnreachableError,
  isUnauthenticated,
  parserFor,
  refusal,
} from "@garam/platform-sdk-ts/garam";
