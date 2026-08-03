import { SiteFooter } from "./_components/site-footer";
import { SiteHeader } from "./_components/site-header";

/** The public shell: no session is read, so the page stays static. */
export default function MarketingLayout({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  return (
    <>
      <SiteHeader />
      <main className="flex-1">{children}</main>
      <SiteFooter />
    </>
  );
}
