import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // A self-contained server, as the console's: the image carries only
  // the runtime it needs.
  output: "standalone",
  // The repository's pnpm-workspace.yaml would make Next trace from the
  // repository root and nest the server under this package's path; the
  // image builds from this folder alone, so the root is this folder.
  outputFileTracingRoot: __dirname,
  outputFileTracingIncludes: {
    "/*": [
      "node_modules/.pnpm/@swc+helpers@*/node_modules/@swc/helpers/esm/**/*",
    ],
  },
};

export default nextConfig;
