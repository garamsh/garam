import { Suspense } from "react";

import { OrgShellRead } from "../../_components/org-shell-read";

/**
 * Everything inside one organization. Its only work is reading what
 * the shell shows of it — the caller's role, the sections they may
 * read, what the palette finds — behind `<Suspense>` so that no page
 * below waits on it and every `loading.tsx` still streams
 * (stack-nextjs.md §Server Components — pages and layouts).
 */
export default function OrgLayout({
  children,
  params,
}: Readonly<{
  children: React.ReactNode;
  params: Promise<{ org: string }>;
}>) {
  return (
    <>
      <Suspense fallback={null}>
        <OrgShellRead org={params.then((resolved) => resolved.org)} />
      </Suspense>
      {children}
    </>
  );
}
