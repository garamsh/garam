/**
 * The user code a `/device` address carries. garam's
 * `verification_uri_complete` names it `code` (`api/openapi.yaml`,
 * `DeviceCode`); `user_code`, RFC 8628's own name for it, is read where
 * `code` is absent.
 */
export type DeviceCodeParams = {
  code?: string | string[];
  user_code?: string | string[];
};

/** The code the address carries, or "" where it carries none. */
export function deviceCodeOf(params: DeviceCodeParams): string {
  for (const value of [params.code, params.user_code]) {
    if (typeof value === "string" && value !== "") return value;
  }
  return "";
}
