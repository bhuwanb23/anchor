"use client";

import { create } from "zustand";
import type { User } from "@/types";
import { getMe, login as authLogin, logout as authLogout } from "@/lib/auth";
import type { LoginRequest } from "@/types";

interface AuthState {
  user: User | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  login: (data: LoginRequest) => Promise<void>;
  logout: () => void;
  loadUser: () => Promise<void>;
}

export const useAuth = create<AuthState>((set) => ({
  user: null,
  isLoading: true,
  isAuthenticated: false,

  login: async (data: LoginRequest) => {
    await authLogin(data);
    const user = await getMe();
    set({ user, isAuthenticated: true });
  },

  logout: () => {
    authLogout();
    set({ user: null, isAuthenticated: false });
  },

  loadUser: async () => {
    try {
      const user = await getMe();
      set({ user, isAuthenticated: true, isLoading: false });
    } catch {
      set({ user: null, isAuthenticated: false, isLoading: false });
    }
  },
}));
