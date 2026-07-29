import api from "./api";
import type { AuthResponse, LoginRequest } from "@/types";

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

export async function register(data: LoginRequest): Promise<AuthResponse> {
  const res = await api.post<AuthResponse>("/api/v1/auth/register", data);
  setToken(res.data.token);
  return res.data;
}

export async function logout(): Promise<void> {
  removeToken();
}

export async function getMe(): Promise<{ id: string; email: string }> {
  const res = await api.get<{ id: string; email: string }>("/api/v1/auth/me");
  return res.data;
}
