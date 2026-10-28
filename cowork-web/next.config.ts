import path from "node:path";

import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // A self-contained server, as the console's: the image carries only
  // the runtime it needs.
  output: "standalone",
  // Dependencies are installed once for the workspace, in the root's
  // node_modules, so tracing starts there and the server sits at
  // .next/standalone/cowork-web/server.js.
  outputFileTracingRoot: path.join(__dirname, ".."),
  outputFileTracingIncludes: {
    "/*": [
      "../node_modules/.pnpm/@swc+helpers@*/node_modules/@swc/helpers/esm/**/*",
    ],
  },
};

export default nextConfig;
