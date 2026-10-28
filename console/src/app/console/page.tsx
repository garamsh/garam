import { redirect } from "next/navigation";

/**
 * `/console` is where the OAuth callback and the proxy send a signed-in
 * visitor; the organizations list is the console's first screen.
 */
export default function ConsolePage() {
  redirect("/console/orgs");
}
