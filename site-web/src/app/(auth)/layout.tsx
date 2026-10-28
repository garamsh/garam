import { BrandMark } from "@garam/design/components/brand-mark";

/** Facts a visitor at the door can verify once inside. */
const ASSURANCES = [
  "Membership by invitation, with an org role",
  "Password sign-in, server-side sessions",
  "Policy-based authorization on every request",
];

/**
 * The sign-in shell: the brand panel, on the console sidebar's dimmed
 * surface, states what the product is, and the right column carries
 * the form. Below `lg` only the form remains.
 */
export default function AuthLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <div className="flex flex-1 lg:grid lg:grid-cols-12">
      <aside className="hidden flex-col gap-12 bg-sidebar p-12 text-sidebar-foreground lg:col-span-5 lg:flex">
        <BrandMark className="text-xl text-sidebar-accent-foreground" />
        <div className="my-auto flex flex-col gap-6">
          <p className="max-w-sm text-2xl font-semibold tracking-tight text-balance text-sidebar-accent-foreground">
            Who belongs to your organization, and what they may do.
          </p>
          <ul className="flex flex-col gap-2">
            {ASSURANCES.map((assurance) => (
              <li key={assurance} className="flex items-center gap-2.5">
                <span
                  aria-hidden
                  className="size-1.5 rounded-full bg-sidebar-primary"
                />
                {assurance}
              </li>
            ))}
          </ul>
        </div>
      </aside>
      <main className="flex flex-1 flex-col items-center justify-center gap-8 px-4 py-10 sm:p-6 lg:col-span-7">
        <BrandMark className="text-xl lg:hidden" />
        {children}
      </main>
    </div>
  );
}
