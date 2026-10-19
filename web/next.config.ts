import type { NextConfig } from "next";

import { env } from "./src/env";

const nextConfig: NextConfig = {
  reactCompiler: true,
  // Emit a self-contained server so the container image carries only
  // the runtime it needs (deploy/local/Dockerfile.web).
  output: "standalone",
  // @vercel/nft resolves package exports under `require`, which takes
  // @swc/helpers' cjs/ half; Node 22.12+ enables require(esm), so the
  // standalone server resolves the same specifiers to esm/ and that
  // half has to be traced as well.
  outputFileTracingIncludes: {
    "/*": [
      "node_modules/.pnpm/@swc+helpers@*/node_modules/@swc/helpers/esm/**/*",
    ],
  },
  // People and Agents were one Members list until ADR-0116; a bookmark
  // of it lands on People.
  async redirects() {
    return [
      {
        source: "/console/orgs/:org/members/:path*",
        destination: "/console/orgs/:org/people/:path*",
        permanent: false,
      },
    ];
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
