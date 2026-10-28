import { redirect } from "next/navigation";

import { deviceCodeOf, type DeviceCodeParams } from "@/lib/device-code";

type Params = { searchParams: Promise<DeviceCodeParams> };

/**
 * The verification page `garam login` names (ADR-0120). The page itself
 * lives inside the console, whose sign-in gate and return path bring a
 * signed-out visitor back to it, so this address only forwards there,
 * keeping the code the CLI put in it.
 */
export default async function DeviceRedirect({ searchParams }: Params) {
  const code = deviceCodeOf(await searchParams);
  redirect(
    code === ""
      ? "/console/device"
      : `/console/device?code=${encodeURIComponent(code)}`,
  );
}
