import type { NextConfig } from "next";

const apiBaseUrl =
  process.env.API_PROXY_TARGET ??
  process.env.NEXT_PUBLIC_API_BASE_URL ??
  "http://127.0.0.1:8180";

const nextConfig: NextConfig = {
  output: "standalone",
  distDir: process.env.NEXT_DIST_DIR ?? ".next",
  reactStrictMode: true,
  poweredByHeader: false,
  // resolve: sidecar 70s < Go 90s < Web 105s; refresh: sidecar 240s < Go 300s < Web 330s < proxy 360s
  experimental: {
    proxyTimeout: 360_000,
  },
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: `${apiBaseUrl}/api/:path*`,
      },
    ];
  },
};

export default nextConfig;
