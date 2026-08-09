import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // Smaller Docker/runtime image — only the files Next needs to serve.
  output: "standalone",
  // Dev-only: proxy API/install/releases to local control plane.
  // Production dashboard calls NEXT_PUBLIC_API_URL directly.
  async rewrites() {
    if (process.env.NODE_ENV === "production") {
      return [];
    }
    const api = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";
    return [
      { source: "/api/:path*", destination: `${api}/api/:path*` },
      { source: "/install.sh", destination: `${api}/install.sh` },
      { source: "/releases/:path*", destination: `${api}/releases/:path*` },
    ];
  },
};

export default nextConfig;
