"use client";

import { usePathname } from "next/navigation";
import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";

/** The name an item page gave its entry, and the page that gave it. */
type ItemName = { pathname: string; label: string };

const ItemCrumbContext = createContext<{
  name: ItemName | null;
  setName: (name: ItemName | null) => void;
} | null>(null);

/**
 * Carries an item page's name for its entry up to the breadcrumb. The
 * breadcrumb renders in the layout, which does not re-render on
 * navigation and cannot read the entry itself, so the page that did
 * read it hands the name over.
 */
export function ItemCrumbProvider({ children }: { children: ReactNode }) {
  const [name, setName] = useState<ItemName | null>(null);
  return (
    <ItemCrumbContext.Provider value={{ name, setName }}>
      {children}
    </ItemCrumbContext.Provider>
  );
}

/** Names the current page's entry in the breadcrumb; renders nothing. */
export function ItemCrumb({ label }: { label: string }) {
  const pathname = usePathname();
  const setName = useContext(ItemCrumbContext)?.setName;

  useEffect(() => {
    setName?.({ pathname, label });
    return () => setName?.(null);
  }, [pathname, label, setName]);

  return null;
}

/**
 * The name the page at `pathname` gave its entry. A name another page
 * left behind is not this page's, so it is ignored rather than shown.
 */
export function useItemCrumb(pathname: string): string | undefined {
  const name = useContext(ItemCrumbContext)?.name;
  return name?.pathname === pathname ? name.label : undefined;
}
