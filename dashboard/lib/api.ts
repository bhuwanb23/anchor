import axios, { AxiosError, AxiosRequestConfig, InternalAxiosRequestConfig } from "axios";
import type { AuthResponse } from "@/types";

const ACCESS_TOKEN_KEY = "token";
const REFRESH_TOKEN_KEY = "refresh_token";

const api = axios.create({
  baseURL: process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080",
  headers: {
    "Content-Type": "application/json",
  },
});

api.interceptors.request.use((config) => {
  if (typeof window !== "undefined") {
    const token = localStorage.getItem(ACCESS_TOKEN_KEY);
    if (token) {
      config.headers.Authorization = `Bearer ${token}`;
    }
  }
  return config;
});

// Single-flight refresh: if several requests 401 at once, only one refresh
// call is issued and the rest await the same promise.
let refreshPromise: Promise<string> | null = null;

function refreshAccessToken(): Promise<string> {
  if (!refreshPromise) {
    refreshPromise = doRefresh().finally(() => {
      refreshPromise = null;
    });
  }
  return refreshPromise;
}

// The refresh call uses a bare axios instance so it is not subject to this
// interceptor (avoiding an infinite 401 → refresh → 401 loop).
async function doRefresh(): Promise<string> {
  const refreshToken =
    typeof window !== "undefined" ? localStorage.getItem(REFRESH_TOKEN_KEY) : null;
  if (!refreshToken) throw new Error("no refresh token");

  const res = await axios.post<AuthResponse>(
    `${process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080"}/api/v1/auth/refresh`,
    { refresh_token: refreshToken }
  );

  if (typeof window !== "undefined") {
    localStorage.setItem(ACCESS_TOKEN_KEY, res.data.access_token);
    localStorage.setItem(REFRESH_TOKEN_KEY, res.data.refresh_token);
  }
  return res.data.access_token;
}

api.interceptors.response.use(
  (response) => response,
  async (error: AxiosError) => {
    const status = error.response?.status;
    const isAuthEndpoint =
      typeof error.config?.url === "string" &&
      error.config.url.includes("/auth/");
    const alreadyRetried = (error.config as InternalAxiosRequestConfig & { _retried?: boolean })?._retried;

    // Only attempt a silent refresh on 401 from protected endpoints, once.
    if (status === 401 && !isAuthEndpoint && !alreadyRetried) {
      try {
        const newToken = await refreshAccessToken();
        const config = error.config as InternalAxiosRequestConfig & { _retried?: boolean };
        config._retried = true;
        config.headers.Authorization = `Bearer ${newToken}`;
        return api.request(config as AxiosRequestConfig);
      } catch {
        // Refresh failed (expired/revoked) — session is over.
        if (typeof window !== "undefined") {
          localStorage.removeItem(ACCESS_TOKEN_KEY);
          localStorage.removeItem(REFRESH_TOKEN_KEY);
          window.location.href = "/login";
        }
      }
    }
    return Promise.reject(error);
  }
);

export default api;
