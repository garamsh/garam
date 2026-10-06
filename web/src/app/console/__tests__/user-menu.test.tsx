import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { UserMenu } from "@/app/console/_components/user-menu";
import { SidebarProvider } from "@/components/ui/sidebar";
import { TooltipProvider } from "@/components/ui/tooltip";
import { setupUser } from "@/testing/user";

// The Server Action revokes the session and redirects; the menu's own
// contract is that submitting the control reaches it.
const loggedOut = vi.hoisted(() => vi.fn());

vi.mock("@/features/auth/actions", () => ({ logOut: loggedOut }));

function renderMenu(nickname: string) {
  return render(
    <TooltipProvider>
      <SidebarProvider>
        <UserMenu nickname={nickname} />
      </SidebarProvider>
    </TooltipProvider>,
  );
}

describe("user menu", () => {
  it("names the signed-in user", () => {
    renderMenu("Alice");

    // Presence: the trigger's name is the sr-only "Open account menu",
    // the avatar's initials, the nickname and "Signed in".
    expect(screen.getByRole("button", { name: /Alice/ })).toBeInTheDocument();
  });

  it("signs the user out", async () => {
    const user = setupUser();
    renderMenu("Alice");

    // Presence, as above: the nickname is one part of the trigger's.
    await user.click(screen.getByRole("button", { name: /Alice/ }));
    await user.click(await screen.findByRole("menuitem", { name: "Sign out" }));

    expect(loggedOut).toHaveBeenCalled();
  });
});
