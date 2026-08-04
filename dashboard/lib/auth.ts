import api from "./api";
import type { AuthResponse, LoginRequest, RegisterRequest, RegisterResponse } from "@/types";

export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem("token");
}

export function setToken(token: string): void {
  localStorage.setItem("token", token);
}

export function removeToken(): void {
  localStorage.removeItem("token");
}

export function isLoggedIn(): boolean {
  return !!getToken();
}

export async function login(data: LoginRequest): Promise<AuthResponse> {
  const res = await api.post<AuthResponse>("/api/v1/auth/login", data);
  setToken(res.data.token);
  return res.data;
}

// Layer 5A Step 1C: registration creates the account only — no token is
// issued, the user must log in separately.
export async function register(data: RegisterRequest): Promise<RegisterResponse> {
  const res = await api.post<RegisterResponse>("/api/v1/auth/register", data);
  return res.data;
}

export async function logout(): Promise<void> {
  removeToken();
}

export async function getMe(): Promise<{ id: string; email: string; name: string }> {
  const res = await api.get<{ id: string; email: string; name: string }>("/api/v1/auth/me");
  return res.data;
}
