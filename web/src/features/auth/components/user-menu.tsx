import { redirect } from "next/navigation";

import { Button } from "@/components/ui/button";
import { getSession } from "@/features/auth/api/get-session";
import { logOut } from "@/features/auth/api/log-out";

/**
 * The signed-in identity and the way out of it. The layout renders it
 * behind `<Suspense>` so the uncached `/me` call does not block the
 * shell; the check itself stays load-bearing — `proxy.ts` only checks
 * that a cookie exists, so an expired or revoked session lands on the
 * login page rather than on an empty dashboard.
 */
export async function UserMenu() {
  const user = await getSession();
  if (!user) {
    redirect("/login");
  }
  return (
    <div className="flex items-center gap-3">
      <span className="text-sm text-muted-foreground">{user.nickname}</span>
      <form action={logOut}>
        <Button type="submit" variant="outline" size="sm">
          Sign out
        </Button>
      </form>
    </div>
  );
}
