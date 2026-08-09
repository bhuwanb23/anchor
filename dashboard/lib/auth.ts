import api, { saveTokens, clearTokens, getAccessToken } from "./api";
import type { AuthResponse, AuthUser, LoginRequest, RegisterRequest, RegisterResponse } from "@/types";

const REFRESH_TOKEN_KEY = "refresh_token";

function getRefreshToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(REFRESH_TOKEN_KEY);
}

// ---------------------------------------------------------------------------
// Token accessors (re-export for components that need raw token values)
// ---------------------------------------------------------------------------

export function getToken(): string | null {
  return getAccessToken();
}

export function isLoggedIn(): boolean {
  return !!getAccessToken();
}

// ---------------------------------------------------------------------------
// Auth actions
// ---------------------------------------------------------------------------

export async function login(data: LoginRequest): Promise<AuthResponse> {
  const res = await api.post<AuthResponse>("/api/v1/auth/login", data);
  saveTokens(res.data.access_token, res.data.refresh_token);
  return res.data;
}

// Registration creates the account only — no token is issued, the user must
// log in separately.
export async function register(data: RegisterRequest): Promise<RegisterResponse> {
  const res = await api.post<RegisterResponse>("/api/v1/auth/register", data);
  return res.data;
}

// Silently exchange the refresh token for a fresh access token (and a rotated
// refresh token). This uses a bare axios instance via api.post but sends the
// refresh token in the body as required by the Go /auth/refresh endpoint.
export async function refreshAccessToken(): Promise<AuthResponse> {
  const refreshToken = getRefreshToken();
  if (!refreshToken) throw new Error("no refresh token");
  const res = await api.post<AuthResponse>("/api/v1/auth/refresh", {
    refresh_token: refreshToken,
  });
  saveTokens(res.data.access_token, res.data.refresh_token);
  return res.data;
}

// Revoke THIS device's session server-side so the refresh token can never mint
// new access tokens, then clear local storage and the session cookie.
// The API call is best-effort — if it fails (offline, already-expired token)
// local cleanup still happens; the access token expires on its own in 24h.
export async function logout(): Promise<void> {
  const refreshToken = getRefreshToken();
  if (refreshToken) {
    try {
      await api.post("/api/v1/auth/logout", { refresh_token: refreshToken });
    } catch {
      // Swallow — revocation failure must not block local logout.
    }
  }
  clearTokens();
}

export async function getMe(): Promise<AuthUser> {
  const res = await api.get<AuthUser>("/api/v1/auth/me");
  return res.data;
}
