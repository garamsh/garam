import { NotFoundBlock } from "./_components/not-found-block";

/**
 * What `notFound()` renders anywhere in the console outside an
 * organization, inside the shell so the sidebar and the palette stay
 * to hand.
 */
export default function ConsoleNotFound() {
  return <NotFoundBlock />;
}
