import { Button } from "@/components/ui/button";
import { logOut } from "@/features/auth/api/log-out";
import type { User } from "@/features/auth/api/get-session";

/** The signed-in identity and the way out of it. */
export function UserMenu({ user }: { user: User }) {
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
