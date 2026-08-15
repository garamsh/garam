import Link from "next/link";
import { Suspense } from "react";

import { UserMenu } from "@/features/auth/components/user-menu";

/**
 * The authenticated shell. The session re-verification lives in the
 * header's `UserMenu`, behind `<Suspense>`: awaiting the uncached `/me`
 * fetch at this level would block the whole segment and `loading.tsx`
 * could never stream (stack-nextjs.md §2).
 */
export default function AppLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <>
      <header className="border-b">
        <div className="mx-auto flex w-full max-w-5xl items-center justify-between gap-4 px-6 py-4">
          <Link href="/console/orgs" className="font-heading text-lg font-semibold">
            garam
          </Link>
          <Suspense>
            <UserMenu />
          </Suspense>
        </div>
      </header>
      <main className="mx-auto w-full max-w-5xl flex-1 px-6 py-8">
        {children}
      </main>
    </>
  );
}
