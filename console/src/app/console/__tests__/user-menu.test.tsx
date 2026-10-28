import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { UserMenu } from "@/app/console/_components/user-menu";
import { SidebarProvider } from "@garam/design/ui/sidebar";
import { TooltipProvider } from "@garam/design/ui/tooltip";
import { THEME_STORAGE_KEY } from "@garam/design/lib/theme";
import { stubLocalStorage } from "@garam/design/testing/storage";

// The Server Action revokes the session and redirects; the menu's own
// contract is that submitting the control reaches it.
const loggedOut = vi.hoisted(() => vi.fn());

vi.mock("@/features/auth/actions", () => ({ logOut: loggedOut }));

beforeEach(() => {
  stubLocalStorage();
  document.documentElement.className = "";
});

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
    const user = userEvent.setup();
    renderMenu("Alice");

    // Presence, as above: the nickname is one part of the trigger's.
    await user.click(screen.getByRole("button", { name: /Alice/ }));
    await user.click(await screen.findByRole("menuitem", { name: "Sign out" }));

    expect(loggedOut).toHaveBeenCalled();
  });

  it("offers the theme and applies the one chosen", async () => {
    const user = userEvent.setup();
    renderMenu("Alice");

    await user.click(screen.getByRole("button", { name: /Alice/ }));
    expect(
      await screen.findByRole("menuitemradio", { name: "System" }),
    ).toHaveAttribute("aria-checked", "true");
    await user.click(screen.getByRole("menuitemradio", { name: "Dark" }));

    expect(document.documentElement).toHaveClass("dark");
    expect(window.localStorage.getItem(THEME_STORAGE_KEY)).toBe("dark");
  });
});
