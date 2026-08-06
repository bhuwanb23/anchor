"use client";

import { create } from "zustand";
import * as authLib from "@/lib/auth";

interface User {
  id: string;
  email: string;
  name: string;
}

interface AuthState {
  user: User | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  login: (email: string, password: string) => Promise<void>;
  register: (name: string, email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  loadUser: () => Promise<void>;
}

export const useAuthStore = create<AuthState>((set) => ({
  user: null,
  isLoading: true,
  isAuthenticated: false,

  login: async (email, password) => {
    const res = await authLib.login({ email, password });
    set({ user: res.user, isAuthenticated: true });
  },

  register: async (name, email, password) => {
    await authLib.register({ name, email, password });
  },

  logout: async () => {
    await authLib.logout();
    set({ user: null, isAuthenticated: false });
  },

  loadUser: async () => {
    try {
      if (!authLib.isLoggedIn()) {
        set({ isLoading: false, isAuthenticated: false });
        return;
      }
      const user = await authLib.getMe();
      set({ user, isAuthenticated: true, isLoading: false });
    } catch {
      set({ user: null, isAuthenticated: false, isLoading: false });
    }
  },
}));
