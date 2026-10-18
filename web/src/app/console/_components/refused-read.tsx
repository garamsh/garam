import { Banner } from "@/components/banner";

/**
 * A read garam refused the caller, said in the one wording the console
 * uses for it, so a refusal never reads as an empty list or a missing
 * object. `subject` is what was refused, as the title names it: "this
 * organization's teams". `children` adds what the refusal leaves out.
 */
export function RefusedRead({
  subject,
  children,
}: {
  subject: string;
  children?: React.ReactNode;
}) {
  return (
    <Banner tone="info" title={`You can't view ${subject}`}>
      Access is granted by this organization&apos;s roles and policies.
      {children && <> {children}</>}
    </Banner>
  );
}
