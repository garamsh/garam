import path from "node:path";

import type { NextConfig } from "next";

import { env } from "./src/env";

const nextConfig: NextConfig = {
  reactCompiler: true,
  // lib/design/ and platform/client ship TypeScript source, which this app
  // compiles.
  transpilePackages: ["@garam/design", "@garam/platform-sdk-ts"],
  // Emit a self-contained server so the container image carries only
  // the runtime it needs (deploy/local/Dockerfile.web).
  output: "standalone",
  // Dependencies are installed once for the workspace, in the root's
  // node_modules, so tracing starts there and the server sits at
  // .next/standalone/site/server.js.
  outputFileTracingRoot: path.join(__dirname, ".."),
  // @vercel/nft resolves package exports under `require`, which takes
  // @swc/helpers' cjs/ half; Node 22.12+ enables require(esm), so the
  // standalone server resolves the same specifiers to esm/ and that
  // half has to be traced as well.
  outputFileTracingIncludes: {
    "/*": [
      "../node_modules/.pnpm/@swc+helpers@*/node_modules/@swc/helpers/esm/**/*",
    ],
  },
  // The backend is reachable under `/api/*` on this origin so the
  // `garam_session` cookie stays same-origin and no CORS layer is
  // needed (ADR-0009). The backend's own paths are unprefixed, so the
  // `/api` segment is stripped here.
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${env.GARAM_API_URL}/:path*`,
      },
    ];
  },
};

export default nextConfig;
