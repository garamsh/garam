import { createContext, useContext, type ReactNode } from "react";

/**
 * What a block that embeds something cowork keeps renders through: the
 * app supplies the component, so a plugin reaches the app only through
 * this, as through the registry (ADR-0127 §Decision 9). Where no app
 * supplies one, an embed renders nothing.
 */
export type Embed = (what: { kind: "artifact"; id: string }) => ReactNode;

export const EmbedContext = createContext<Embed | null>(null);

export const useEmbed = () => useContext(EmbedContext);
