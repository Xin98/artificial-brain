import type { NextConfig } from "next";

// API_INTERNAL_URL is validated inline here because next.config.ts loads
// outside Next's react-server condition: importing the shared/server modules
// marked with the "server-only" guard would throw at build time. The default
// and validation mirror shared/server/runtime-config.ts.
function apiInternalURL(): string {
  const value = process.env.API_INTERNAL_URL ?? "http://localhost:8080";
  const url = new URL(value);

  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error("API_INTERNAL_URL must be an absolute HTTP(S) URL");
  }

  return url.toString().replace(/\/$/, "");
}

const nextConfig: NextConfig = {
  output: "standalone",
  async rewrites() {
    const base = apiInternalURL();
    return [
      {
        source: "/api/v1/:path*",
        destination: `${base}/api/v1/:path*`,
      },
    ];
  },
};

export default nextConfig;
