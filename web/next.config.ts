import type { NextConfig } from "next";

import { env } from "./src/env";

const nextConfig: NextConfig = {
  reactCompiler: true,
  // Emit a self-contained server so the container image carries only
  // the runtime it needs (deploy/local/Dockerfile.web).
  output: "standalone",
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
