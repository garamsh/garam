/**
 * Joins class names so a later Tailwind utility beats an earlier one.
 * The shadcn CLI writes its primitives against the `cn` package itself;
 * everything else imports it from here, so both name one function.
 */
export { cn } from "cn";
