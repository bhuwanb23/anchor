"use client";

import { create } from "zustand";
import * as authLib from "@/lib/auth";
import type { AuthUser } from "@/types";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const USER_STORAGE_KEY = "yp_user";

// ---------------------------------------------------------------------------
// Persistence helpers — persist user to localStorage so no flash of
// unauthenticated state on page refresh.
// ---------------------------------------------------------------------------

function loadPersistedUser(): AuthUser | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = localStorage.getItem(USER_STORAGE_KEY);
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
}

function persistUser(user: AuthUser | null): void {
  if (typeof window === "undefined") return;
  if (user) {
    localStorage.setItem(USER_STORAGE_KEY, JSON.stringify(user));
  } else {
    localStorage.removeItem(USER_STORAGE_KEY);
  }
}

// ---------------------------------------------------------------------------
// Auth Store
// ---------------------------------------------------------------------------

interface AuthState {
  user: AuthUser | null;
  isLoading: boolean;
  isAuthenticated: boolean;
  login: (email: string, password: string) => Promise<void>;
  register: (name: string, email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  loadUser: () => Promise<void>;
}

// Initialize once at module load
const initialUser = loadPersistedUser();

export const useAuthStore = create<AuthState>((set) => ({
  // Initialize from localStorage to avoid flash on page refresh
  user: initialUser,
  isLoading: true,
  isAuthenticated: !!initialUser,

  login: async (email, password) => {
    try {
      const res = await authLib.login({ email, password });
      persistUser(res.user);
      set({ user: res.user, isAuthenticated: true, isLoading: false });
    } catch (error) {
      // Reset loading state on error so components don't show infinite spinner
      set({ isLoading: false });
      throw error;
    }
  },

  register: async (name, email, password) => {
    await authLib.register({ name, email, password });
  },

  logout: async () => {
    await authLib.logout();
    persistUser(null);
    set({ user: null, isAuthenticated: false, isLoading: false });
  },

  loadUser: async () => {
    try {
      if (!authLib.isLoggedIn()) {
        persistUser(null);
        set({ user: null, isLoading: false, isAuthenticated: false });
        return;
      }
      const user = await authLib.getMe();
      persistUser(user);
      set({ user, isAuthenticated: true, isLoading: false });
    } catch {
      persistUser(null);
      set({ user: null, isAuthenticated: false, isLoading: false });
    }
  },
}));
