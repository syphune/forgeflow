import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  reactStrictMode: true,
  output: "standalone",
  poweredByHeader: false,
  async rewrites() {
    const backendURL = (process.env.FORGEFLOW_BACKEND_URL ?? "http://localhost:18080").replace(/\/+$/, "");
    return [
      { source: "/api/v1/:path*", destination: `${backendURL}/api/v1/:path*` },
      { source: "/health/:path*", destination: `${backendURL}/health/:path*` },
    ];
  },
  async headers() {
    const configuredAPI = process.env.NEXT_PUBLIC_FORGEFLOW_API_URL;
    let apiOrigin = "'self'";
    if (configuredAPI) {
      try {
        apiOrigin = new URL(configuredAPI).origin;
      } catch {
        apiOrigin = "'self'";
      }
    }
    return [{
      source: "/(.*)",
      headers: [
        { key: "Content-Security-Policy", value: `default-src 'self'; base-uri 'self'; frame-ancestors 'none'; object-src 'none'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self' ${apiOrigin}` },
        { key: "Permissions-Policy", value: "camera=(), geolocation=(), microphone=()" },
        { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
        { key: "X-Content-Type-Options", value: "nosniff" },
        { key: "X-Frame-Options", value: "DENY" },
      ],
    }];
  },
};

export default nextConfig;
