import api from "./api";
import type { AuthResponse, AuthUser, LoginRequest, RegisterRequest, RegisterResponse } from "@/types";

const ACCESS_TOKEN_KEY = "token";
const REFRESH_TOKEN_KEY = "refresh_token";

export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(ACCESS_TOKEN_KEY);
}

export function getRefreshToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(REFRESH_TOKEN_KEY);
}

function setTokens(res: AuthResponse): void {
  localStorage.setItem(ACCESS_TOKEN_KEY, res.access_token);
  localStorage.setItem(REFRESH_TOKEN_KEY, res.refresh_token);
}

export function setToken(token: string): void {
  localStorage.setItem(ACCESS_TOKEN_KEY, token);
}

export function removeToken(): void {
  localStorage.removeItem(ACCESS_TOKEN_KEY);
  localStorage.removeItem(REFRESH_TOKEN_KEY);
}

export function isLoggedIn(): boolean {
  return !!getToken();
}

export async function login(data: LoginRequest): Promise<AuthResponse> {
  const res = await api.post<AuthResponse>("/api/v1/auth/login", data);
  setTokens(res.data);
  return res.data;
}

// Layer 5A Step 1C: registration creates the account only — no token is
// issued, the user must log in separately.
export async function register(data: RegisterRequest): Promise<RegisterResponse> {
  const res = await api.post<RegisterResponse>("/api/v1/auth/register", data);
  return res.data;
}

// Layer 5A Step 2D: silently exchange the refresh token for a fresh access
// token (and a rotated refresh token).
export async function refreshAccessToken(): Promise<AuthResponse> {
  const refreshToken = getRefreshToken();
  if (!refreshToken) throw new Error("no refresh token");
  const res = await api.post<AuthResponse>("/api/v1/auth/refresh", {
    refresh_token: refreshToken,
  });
  setTokens(res.data);
  return res.data;
}

// Layer 5A Step 4A Level 1: revoke THIS device's session server-side so the
// refresh token can never mint new access tokens, then clear local storage.
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
  removeToken();
}

export async function getMe(): Promise<AuthUser> {
  const res = await api.get<AuthUser>("/api/v1/auth/me");
  return res.data;
}
