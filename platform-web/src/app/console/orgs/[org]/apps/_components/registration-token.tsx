"use client";

import { SecretReveal } from "@/components/secret-reveal";
import { Time } from "@/components/time";

/**
 * The registration token an instance is created or reissued with,
 * shown once (ADR-0192 decision 1). garam keeps its hash alone, so the
 * caller holds the token in the state the action returned and nowhere
 * else, and this dialog holds the page shut until it was kept.
 */
export function RegistrationToken({
  name,
  token,
  expiresAt,
  onDone,
}: {
  name: string;
  token: string | null;
  expiresAt?: string;
  onDone: () => void;
}) {
  return (
    <SecretReveal
      open={token !== null}
      title="Registration token"
      description={
        <>
          Registers {name} once, and stops working{" "}
          {expiresAt ? <Time value={expiresAt} /> : "soon"}. Hand it to the
          connector now: garam keeps no copy to show again.
        </>
      }
      secret={token ?? ""}
      noun="registration token"
      onDone={onDone}
    />
  );
}
