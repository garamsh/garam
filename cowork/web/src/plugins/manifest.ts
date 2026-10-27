import { artifactPlugin } from "./core/artifact";
import { codePlugin } from "./core/code";
import { dividerPlugin } from "./core/divider";
import { headingsPlugin } from "./core/headings";
import { imagePlugin } from "./core/image";
import { linkPlugin } from "./core/link";
import { listsPlugin } from "./core/lists";
import { quotePlugin } from "./core/quote";
import { textPlugin } from "./core/text";
import { createRegistry, type Plugin } from "./registry";

/**
 * The plugins this build bundles (ADR-0127 §Decision 9), in the order
 * the insert menu lists their blocks. The built-in ones are listed here
 * as any other plugin would be: through `register`, and nothing else.
 */
export const plugins: Plugin[] = [
  textPlugin,
  headingsPlugin,
  listsPlugin,
  quotePlugin,
  codePlugin,
  dividerPlugin,
  imagePlugin,
  artifactPlugin,
  linkPlugin,
];

export const registry = createRegistry();
for (const plugin of plugins) registry.register(plugin);
