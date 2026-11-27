import type { Metadata } from "next";
import Link from "next/link";

export const metadata: Metadata = {
  title: "Report abuse — garam",
  description: "How to report phishing, malware or other abuse on garam.run.",
};

/**
 * The abuse contact for `garam.run`, the domain that serves what garam's
 * users and bots make. It is public and static; `/.well-known/security.txt`
 * (RFC 9116) names the same address and this page as its policy.
 */
export default function AbusePage() {
  return (
    <main className="mx-auto flex w-full max-w-prose flex-1 flex-col gap-4 px-6 py-14">
      <h1 className="font-heading text-2xl font-semibold">Report abuse</h1>
      <p>
        garam.run hosts content made by garam&rsquo;s users and bots: cowork
        results at <code>c-&lt;id&gt;.garam.run</code> and workspaces at{" "}
        <code>m-&lt;label&gt;.garam.run</code>.
      </p>
      <p>
        To report phishing, malware or other abuse on any garam.run name, email{" "}
        <a className="underline" href="mailto:security@garam.sh">
          security@garam.sh
        </a>{" "}
        with the full URL.
      </p>
      <p>
        We answer within 30 days, and aim to answer much sooner. The same
        address takes security reports about garam.
      </p>
      <p>
        <Link className="underline" href="/">
          Back to the home page
        </Link>
      </p>
    </main>
  );
}
