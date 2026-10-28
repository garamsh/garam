import { notFound } from "next/navigation";

/**
 * Any console address no route serves. Answering it here rather than
 * at the root keeps the shell around the not-found page.
 */
export default function MissingConsolePage(): never {
  notFound();
}
