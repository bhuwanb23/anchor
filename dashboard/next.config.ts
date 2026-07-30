import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  async rewrites() {
    return [
      {
        source: "/api/:path*",
        destination: "http://localhost:8080/api/:path*",
      },
      {
        source: "/install.sh",
        destination: "http://localhost:8080/install.sh",
      },
      {
        source: "/releases/:path*",
        destination: "http://localhost:8080/releases/:path*",
      },
    ];
  },
};

export default nextConfig;
