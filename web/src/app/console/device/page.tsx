import type { Metadata } from "next";

import { getSession } from "@/features/auth";
import { deviceCodeOf, type DeviceCodeParams } from "@/lib/device-code";

import { DEVICE_LABEL } from "../_components/navigation";
import { PageHeader } from "../_components/page-header";
import { DeviceForm } from "./_components/device-form";

/**
 * Head metadata for the device route; Next.js reads it by name. The
 * code can ride in the address, so no link followed from here sends it
 * on as a referrer.
 */
export const metadata: Metadata = {
  title: `${DEVICE_LABEL} — garam`,
  referrer: "no-referrer",
};

type Params = { searchParams: Promise<DeviceCodeParams> };

/**
 * Where `garam login` sends a person (ADR-0120): the code their
 * terminal printed, prefilled from `?code=` when the CLI opened
 * the page with it, and the decision on it. The console's sign-in gate
 * and return path bring a signed-out visitor back here, code and all.
 * The page says whom approving signs the CLI in as, since anyone who
 * gets a person to approve a code they did not start signs a CLI in as
 * them.
 */
export default async function DevicePage({ searchParams }: Params) {
  const [params, session] = await Promise.all([searchParams, getSession()]);
  const who = session.status === "signed-in" ? session.user.nickname : "you";
  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title={DEVICE_LABEL}
        description={`This lets the CLI act as ${who}.`}
      />
      <DeviceForm initialCode={deviceCodeOf(params)} />
    </div>
  );
}
