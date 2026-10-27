import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // A self-contained server, as the console's: the image carries only
  // the runtime it needs.
  output: "standalone",
  outputFileTracingIncludes: {
    "/*": [
      "node_modules/.pnpm/@swc+helpers@*/node_modules/@swc/helpers/esm/**/*",
    ],
  },
};

export default nextConfig;
