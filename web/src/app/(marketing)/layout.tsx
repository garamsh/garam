import Link from "next/link";

import { Button } from "@/components/ui/button";

/** The public shell: no session is read, so the page stays static. */
export default function MarketingLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <>
      <header className="sticky top-0 z-10 border-b border-border/60 bg-background/80 backdrop-blur">
        <div className="mx-auto flex w-full max-w-6xl items-center justify-between gap-4 px-6 py-4">
          <Link href="/" className="font-heading text-lg font-semibold">
            garam
          </Link>
          <Button asChild size="sm">
            <Link href="/console">Sign in</Link>
          </Button>
        </div>
      </header>
      <main className="flex-1">{children}</main>
      <footer className="border-t border-border/60">
        <div className="mx-auto w-full max-w-6xl px-6 py-8 text-sm text-muted-foreground">
          garam — identity, organization, and authorization for shared work.
        </div>
      </footer>
    </>
  );
}
