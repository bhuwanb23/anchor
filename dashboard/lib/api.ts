import axios, { AxiosError, AxiosRequestConfig, InternalAxiosRequestConfig } from "axios";
import type { AuthResponse } from "@/types";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const ACCESS_TOKEN_KEY = "token";
const REFRESH_TOKEN_KEY = "refresh_token";
const SESSION_COOKIE = "has_session";

// ---------------------------------------------------------------------------
// Cookie helpers — the session indicator cookie enables Next.js middleware
// to know whether a user is logged in without reading localStorage (which is
// unavailable in server/edge contexts).
// ---------------------------------------------------------------------------

function isSecure(): boolean {
  return typeof window !== "undefined" && window.location.protocol === "https:";
}

function setSessionCookie(): void {
  const secure = isSecure() ? "; Secure" : "";
  document.cookie = `${SESSION_COOKIE}=1; path=/; SameSite=Lax; Max-Age=${60 * 60 * 24 * 30}${secure}`;
}

function clearSessionCookie(): void {
  document.cookie = `${SESSION_COOKIE}=; path=/; SameSite=Lax; Max-Age=0`;
}

// ---------------------------------------------------------------------------
// Axios instance
// ---------------------------------------------------------------------------

const api = axios.create({
  baseURL: process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080",
  timeout: 90_000,
  headers: {
    "Content-Type": "application/json",
  },
});

// ---------------------------------------------------------------------------
// Request interceptor — attach JWT to every request
// ---------------------------------------------------------------------------

api.interceptors.request.use((config) => {
  if (typeof window !== "undefined") {
    const token = localStorage.getItem(ACCESS_TOKEN_KEY);
    if (token) {
      config.headers.Authorization = `Bearer ${token}`;
    }
  }
  return config;
});

// ---------------------------------------------------------------------------
// Single-flight refresh — if multiple requests 401 simultaneously, only one
// refresh call is made and the rest await the same promise.
// ---------------------------------------------------------------------------

let refreshPromise: Promise<string> | null = null;

function refreshAccessToken(): Promise<string> {
  if (!refreshPromise) {
    refreshPromise = doRefresh().finally(() => {
      refreshPromise = null;
    });
  }
  return refreshPromise;
}

async function doRefresh(): Promise<string> {
  const refreshToken =
    typeof window !== "undefined" ? localStorage.getItem(REFRESH_TOKEN_KEY) : null;
  if (!refreshToken) throw new Error("no refresh token");

  // Use a bare axios instance so this request is not subject to the
  // response interceptor (avoids infinite 401 → refresh → 401 loop).
  const res = await axios.post<AuthResponse>(
    `${api.defaults.baseURL}/api/v1/auth/refresh`,
    { refresh_token: refreshToken }
  );

  if (typeof window !== "undefined") {
    localStorage.setItem(ACCESS_TOKEN_KEY, res.data.access_token);
    localStorage.setItem(REFRESH_TOKEN_KEY, res.data.refresh_token);
    setSessionCookie();
  }
  return res.data.access_token;
}

// ---------------------------------------------------------------------------
// Response interceptor — silent refresh on 401, redirect on failure
// ---------------------------------------------------------------------------

api.interceptors.response.use(
  (response) => response,
  async (error: AxiosError) => {
    const status = error.response?.status;

    // Exclude token-issuing endpoints (would loop).
    const url = error.config?.url ?? "";
    const isTokenIssuingEndpoint =
      url.endsWith("/auth/refresh") ||
      url.endsWith("/auth/login") ||
      url.endsWith("/auth/register");

    const alreadyRetried = (error.config as InternalAxiosRequestConfig & { _retried?: boolean })?._retried;

    if (status === 401 && !isTokenIssuingEndpoint && !alreadyRetried) {
      try {
        const newToken = await refreshAccessToken();
        const config = error.config as InternalAxiosRequestConfig & { _retried?: boolean };
        config._retried = true;
        config.headers.Authorization = `Bearer ${newToken}`;
        return api.request(config as AxiosRequestConfig);
      } catch {
        // Refresh failed — session is over, clean up and redirect.
        // Return a resolved promise so the caller's catch doesn't fire
        // during the redirect (prevents double-action like toast + redirect).
        if (typeof window !== "undefined") {
          localStorage.removeItem(ACCESS_TOKEN_KEY);
          localStorage.removeItem(REFRESH_TOKEN_KEY);
          clearSessionCookie();
          window.location.href = "/login";
        }
        return Promise.reject(error);
      }
    }
    return Promise.reject(error);
  }
);

// ---------------------------------------------------------------------------
// Public helpers — used by auth.ts to persist tokens and the session cookie
// ---------------------------------------------------------------------------

export function saveTokens(accessToken: string, refreshToken: string): void {
  if (typeof window === "undefined") return;
  localStorage.setItem(ACCESS_TOKEN_KEY, accessToken);
  localStorage.setItem(REFRESH_TOKEN_KEY, refreshToken);
  setSessionCookie();
}

export function clearTokens(): void {
  if (typeof window === "undefined") return;
  localStorage.removeItem(ACCESS_TOKEN_KEY);
  localStorage.removeItem(REFRESH_TOKEN_KEY);
  clearSessionCookie();
}

export function getAccessToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(ACCESS_TOKEN_KEY);
}

export default api;
