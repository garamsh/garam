"use client";

import {
  ChevronsUpDown,
  LogOut,
  Monitor,
  Moon,
  Sun,
  UserRound,
} from "lucide-react";

import { Avatar, AvatarFallback } from "@/components/ui/avatar";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar";
import { logOut } from "@/features/auth/actions";
import { useTheme } from "@/hooks/use-theme";
import { parseThemePreference } from "@/lib/theme";

const THEMES = [
  { value: "system", label: "System", icon: Monitor },
  { value: "light", label: "Light", icon: Sun },
  { value: "dark", label: "Dark", icon: Moon },
] as const;

/**
 * The signed-in identity, the console's theme, and the way out.
 * `nickname` is null when the API did not say who is signed in; the
 * theme and signing out stay offered, since neither needs the answer.
 */
export function UserMenu({ nickname }: { nickname: string | null }) {
  const { isMobile } = useSidebar();
  const { theme, setTheme } = useTheme();

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton
              size="lg"
              className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
            >
              <span className="sr-only">Open account menu</span>
              <Avatar className="size-8 rounded-lg">
                <AvatarFallback className="rounded-lg text-xs">
                  {nickname ? (
                    nickname.slice(0, 2).toUpperCase()
                  ) : (
                    <UserRound className="size-4" />
                  )}
                </AvatarFallback>
              </Avatar>
              {nickname ? (
                <span className="flex-1 truncate text-left font-medium text-sidebar-accent-foreground">
                  {nickname}
                </span>
              ) : (
                <span className="grid flex-1 text-left leading-tight">
                  <span className="truncate font-medium text-sidebar-accent-foreground">
                    Account
                  </span>
                  <span className="truncate text-xs text-muted-foreground">
                    Can’t load your account
                  </span>
                </span>
              )}
              <ChevronsUpDown className="ml-auto" />
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            className="w-(--radix-dropdown-menu-trigger-width) min-w-56"
            align="end"
            side={isMobile ? "bottom" : "right"}
            sideOffset={4}
          >
            <DropdownMenuLabel className="text-xs text-muted-foreground">
              Theme
            </DropdownMenuLabel>
            <DropdownMenuRadioGroup
              value={theme}
              onValueChange={(value) => setTheme(parseThemePreference(value))}
            >
              {THEMES.map(({ value, label, icon: Icon }) => (
                <DropdownMenuRadioItem key={value} value={value}>
                  <Icon />
                  {label}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
            <DropdownMenuSeparator />
            <form action={logOut}>
              <DropdownMenuItem asChild>
                <button type="submit" className="w-full">
                  <LogOut />
                  Sign out
                </button>
              </DropdownMenuItem>
            </form>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}
