import { describe, expect, it } from "vitest";

import { documentRefusal } from "@/features/orgs/api/document-refusal";
import { GaramApiError } from "@/lib/garam";
import { REFUSAL_COPY } from "@garam/platform-sdk-ts/refusal-copy";

/**
 * A refusal as the seam raises it once it carries garam's reason
 * (`platform/api/openapi.yaml` `Error.reason`, `Error.path`).
 */
function located(path: string): GaramApiError {
  return new GaramApiError(
    400,
    "Effect must be Allow or Deny",
    "invalid_argument",
    { code: "policy_document_effect_invalid", path },
  );
}

describe("documentRefusal", () => {
  it("keeps the place garam named for the document it refused", () => {
    expect(
      documentRefusal(located("/Statement/1/Effect"), "addPolicyVersion"),
    ).toMatchObject({ field: "document", path: "/Statement/1/Effect" });
  });

  it("names the document but no place where garam named none", () => {
    const refused = documentRefusal(
      new GaramApiError(400, "bad", "invalid_argument"),
      "addPolicyVersion",
    );
    expect(refused).toEqual({
      status: "error",
      message: REFUSAL_COPY.policyDocumentRefused,
      field: "document",
    });
  });

  it("leaves every other refusal on the action, not the field", () => {
    // The control: the same place on a refusal that is not the engine
    // refusing the document is not the document's.
    const denied = new GaramApiError(403, "no", "permission_denied", {
      code: "x",
      path: "/Statement/0",
    });
    expect(documentRefusal(denied, "addPolicyVersion")).toEqual({
      status: "error",
      message: REFUSAL_COPY.permissionDenied,
    });
  });
});
