import Link from "next/link";
import { redirect } from "next/navigation";

import { getSession } from "@/features/auth/api/get-session";
import { UserMenu } from "@/features/auth/components/user-menu";

/**
 * The authenticated shell. It resolves the session against the backend
 * — `proxy.ts` only checks that a cookie exists — so an expired or
 * revoked session lands on the login page rather than on an empty
 * dashboard.
 */
export default async function AppLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  const user = await getSession();
  if (!user) {
    redirect("/login");
  }
  return (
    <>
      <header className="border-b">
        <div className="mx-auto flex w-full max-w-5xl items-center justify-between gap-4 px-6 py-4">
          <Link href="/orgs" className="font-heading text-lg font-semibold">
            garam
          </Link>
          <UserMenu user={user} />
        </div>
      </header>
      <main className="mx-auto w-full max-w-5xl flex-1 px-6 py-8">
        {children}
      </main>
    </>
  );
}
