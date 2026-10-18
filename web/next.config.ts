import type { NextConfig } from "next";

import { env } from "./src/env";

const nextConfig: NextConfig = {
  reactCompiler: true,
  // Emit a self-contained server so the container image carries only
  // the runtime it needs (deploy/local/Dockerfile.web).
  output: "standalone",
  // @vercel/nft resolves package exports under `require`, which takes
  // @swc/helpers' cjs/ half; Node 22.12+ enables require(esm), so the
  // standalone server resolves the same specifiers to esm/ and that half
  // has to be traced as well.
  outputFileTracingIncludes: {
    "/*": [
      "node_modules/.pnpm/@swc+helpers@*/node_modules/@swc/helpers/esm/**/*",
    ],
  },
  // The console's Groups section became Teams (ADR-0072), Policies
  // became Permissions (ADR-0073), and People and the agents list became
  // Members (ADR-0113); a bookmark of an old address lands on the new
  // one. `members` was People's address before it was Members'.
  async redirects() {
    return [
      {
        source: "/console/orgs/:org/groups",
        destination: "/console/orgs/:org/teams",
        permanent: false,
      },
      // An agent keeps its page under `agents/`, so only the list's
      // address moves.
      {
        source: "/console/orgs/:org/people",
        destination: "/console/orgs/:org/members?kind=person",
        permanent: false,
      },
      {
        source: "/console/orgs/:org/agents",
        destination: "/console/orgs/:org/members?kind=agent",
        permanent: false,
      },
      {
        source: "/console/orgs/:org/policies",
        destination: "/console/orgs/:org/permissions",
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
