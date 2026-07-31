/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  experimental: {
    typedRoutes: true,
  },
  // Proxy /api/proxy/* to the orchestrator so the browser can use a same-origin URL.
  // Phase 1: only `/v1/chat` and `/v1/plan` and `/v1/run` are used.
  async rewrites() {
    const orch = process.env.NEXT_PUBLIC_API_BASE || "http://localhost:8080";
    return [
      { source: "/api/proxy/:path*", destination: `${orch}/:path*` },
    ];
  },
};

module.exports = nextConfig;
