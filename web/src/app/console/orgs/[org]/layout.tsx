import { Suspense } from "react";

import { OrgRoleRead } from "../../_components/org-role-read";

/**
 * Everything inside one organization. Its only work is reading the
 * caller's role there for the sidebar, behind `<Suspense>` so that no
 * page below waits on it and every `loading.tsx` still streams
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
        <OrgRoleRead org={params.then((resolved) => resolved.org)} />
      </Suspense>
      {children}
    </>
  );
}
