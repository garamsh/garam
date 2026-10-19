import { redirect } from "next/navigation";

type Params = { searchParams: Promise<{ user_code?: string | string[] }> };

/**
 * The verification page `garam login` names (ADR-0120). The page itself
 * lives inside the console, whose sign-in gate and return path bring a
 * signed-out visitor back to it, so this address only forwards there,
 * keeping the code the CLI put in it.
 */
export default async function DeviceRedirect({ searchParams }: Params) {
  const { user_code: code } = await searchParams;
  redirect(
    typeof code === "string" && code !== ""
      ? `/console/device?user_code=${encodeURIComponent(code)}`
      : "/console/device",
  );
}
