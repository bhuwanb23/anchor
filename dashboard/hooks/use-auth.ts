"use client";

import { create } from "zustand";
import type { User } from "@/types";
import { getMe, login as authLogin, logout as authLogout, isLoggedIn } from "@/lib/auth";
import type { LoginRequest } from "@/types";

interface AuthState {
  user: User | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  login: (data: LoginRequest) => Promise<void>;
  logout: () => Promise<void>;
  loadUser: () => Promise<void>;
}

export const useAuth = create<AuthState>((set) => ({
  user: null,
  isLoading: true,
  isAuthenticated: false,

  login: async (data: LoginRequest) => {
    set({ isLoading: true });
    try {
      await authLogin(data);
      const user = await getMe();
      set({ user, isAuthenticated: true, isLoading: false });
    } catch (e) {
      set({ user: null, isAuthenticated: false, isLoading: false });
      throw e;
    }
  },

  logout: async () => {
    await authLogout();
    set({ user: null, isAuthenticated: false, isLoading: false });
  },

  loadUser: async () => {
    if (!isLoggedIn()) {
      set({ user: null, isAuthenticated: false, isLoading: false });
      return;
    }
    set({ isLoading: true });
    try {
      const user = await getMe();
      set({ user, isAuthenticated: true, isLoading: false });
    } catch {
      set({ user: null, isAuthenticated: false, isLoading: false });
    }
  },
}));
