# Tailwind CSS

## Contents
- Core principle
- Verify before adopting or upgrading
- Tailwind v4 specifics
- Composition: inline utilities first
- `@apply` and `@utility`: when each earns its keep
- Theme tokens via `@theme`
- Common idioms
- Anti-patterns

## Core principle

Compose simple utilities in markup. The cheapest style is the one the framework already ships. Custom CSS is only justified when a *specific* combination repeats across many sites and no single utility captures it.

## Verify before adopting or upgrading

Tailwind shifts between major versions. Confirm the install pattern, config syntax, and browser requirements against the live docs (`tailwindcss.com/docs/installation`, `tailwindcss.com/docs/upgrade-guide`) before adopting — pre-trained recollection is a starting point, not source of truth.

## Tailwind v4 specifics

- Install: `npm install tailwindcss @tailwindcss/vite` (Vite) or `@tailwindcss/postcss` (PostCSS). CLI lives in `@tailwindcss/cli`.
- Import: `@import "tailwindcss";` in your CSS. **No `@tailwind base/components/utilities` directives** — those were removed in v4.
- Config is CSS-first via `@theme { ... }` blocks in your stylesheet. **No `tailwind.config.js` required.**
- Browser target: Safari 16.4+, Chrome 111+, Firefox 128+. For older browsers, stay on v3.4.
- Custom utilities: `@utility <name> { ... }` (replaces `@layer utilities` from v3).
- `corePlugins`, `safelist`, `resolveConfig`, `theme()` function — all gone or replaced in v4. The upgrade tool (`npx @tailwindcss/upgrade`) handles most migrations.
- Native CSS nesting supported — no preprocessor step needed.

## Composition: inline utilities first

Default to composing utilities directly in markup. Order is irrelevant; output is deterministic.

```html
<button class="inline-flex items-center gap-2 px-4 py-2 rounded-md
               bg-blue-500 text-white font-medium
               hover:bg-blue-600 active:bg-blue-700
               focus:outline-2 focus:outline-offset-2 focus:outline-blue-500
               disabled:opacity-50 disabled:cursor-not-allowed">
  Submit
</button>
```

Inline utilities make components transparent: a reviewer reads the markup and sees every style applied, with no layer of indirection.

## `@apply` and `@utility`: when each earns its keep

Decision order:

1. **Inline utilities** — one-off layouts and styles. Default.
2. **`@apply`** — when the same 5+ utility chain appears in 3+ components. Pull the chain into a class via `@apply` and let the build resolve it.
3. **`@utility <name> { ... }`** — when you need a *new* class that responds to variants (`hover:`, `focus:`, `lg:`) Tailwind doesn't ship.

Avoid `@apply` for 1–3 utilities — inline them. `@apply` chains longer than ~10 utilities usually signal the wrong abstraction; the design likely needs a token or a component, not more classes.

```css
/* In your CSS — when a chain repeats */
@utility btn-primary {
  @apply inline-flex items-center gap-2 px-4 py-2 rounded-md
         bg-blue-500 text-white font-medium
         hover:bg-blue-600 focus:outline-2 focus:outline-blue-500;
}
```

## Theme tokens via `@theme`

Define design tokens once. Components reference tokens, never raw values.

```css
@theme {
  --color-brand-500: oklch(0.65 0.18 250);
  --font-display: "Inter", "system-ui", sans-serif;
  --radius-card: 0.75rem;
}
```

Use as `bg-brand-500`, `font-display`, `rounded-card`. Hardcoded hex or px in markup is a smell — extract it.

## Common idioms

- **Responsive** — mobile-first: `md:grid-cols-2 lg:grid-cols-3`.
- **State** — `hover:`, `focus:`, `active:`, `disabled:`, `group-hover:`, `peer-*`.
- **Dark mode** — `dark:bg-gray-900`. Toggle via `<html class="dark">` or `@media (prefers-color-scheme: dark)`.
- **Layout** — prefer flex/grid + `gap-*` over `space-x-*` / `space-y-*` (v4 selector change for performance).
- **Sizing** — `size-*` for square; `w-*`/`h-*` otherwise.
- **Color with opacity** — `bg-black/50` (slash syntax). v3's `bg-opacity-*` is removed.
- **Arbitrary values** — `bg-[#ff00aa]` or `bg-(--brand-color)` for CSS vars. Search the docs first; a utility usually exists.

## Anti-patterns

- Long arbitrary-value chains. If the chain survives 3 uses, extract it to a token or `@utility`.
- A single CSS class wrapping one or two utilities — the class then duplicates what a utility does.
- Custom CSS where a utility already exists. Search `/docs` before writing CSS.
- Mixing v3 and v4 patterns — `@tailwind` directives and `tailwind.config.js` are v3; they break under v4.
- Reaching for `--tw-*` internal variables in user code. They're an implementation detail.
- Designing styles that need to be hand-tuned per breakpoint instead of using Tailwind's responsive prefixes.