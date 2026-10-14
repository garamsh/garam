import { BrandMark } from "@/components/brand-mark";

/** Facts a visitor at the door can verify once inside. */
const ASSURANCES = [
  "Membership by invitation, with an org role",
  "Password sign-in, server-side sessions",
  "Policy-based authorization on every request",
];

/**
 * The sign-in shell: the brand panel states what the product is, the
 * right column carries the form. Below `lg` only the form remains.
 */
export default function AuthLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <div className="flex flex-1 lg:grid lg:grid-cols-2">
      <aside className="relative hidden flex-col justify-between overflow-hidden border-r border-border/60 p-12 lg:flex">
        <div
          aria-hidden
          className="ruled-plane pointer-events-none absolute inset-0"
        />
        <BrandMark className="relative" />
        <div className="relative flex flex-col gap-6">
          <p className="font-heading max-w-md text-title font-semibold text-balance">
            Who belongs to your organization, and what they may do.
          </p>
          <ul className="flex flex-col gap-2">
            {ASSURANCES.map((assurance) => (
              <li
                key={assurance}
                className="flex items-center gap-2.5 text-sm text-muted-foreground"
              >
                <span aria-hidden className="size-1 rounded-full bg-brand" />
                {assurance}
              </li>
            ))}
          </ul>
        </div>
        <span className="relative font-mono text-xs text-muted-foreground">
          grn:&lt;organization&gt;:&lt;project&gt;:&lt;service_type&gt;:&lt;identifier&gt;
        </span>
      </aside>
      <main className="flex flex-1 flex-col items-center justify-center gap-8 p-6">
        <BrandMark className="lg:hidden" />
        {children}
      </main>
    </div>
  );
}
