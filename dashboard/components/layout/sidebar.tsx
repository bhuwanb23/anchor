"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";
import { LogOut, Plus, LifeBuoy, User } from "lucide-react";
import { useAuth } from "@/hooks/use-auth";
import { useServerStore } from "@/store/server-store";
import type { ServerStatus } from "@/types";

const statusColors: Record<ServerStatus, string> = {
  connected: "bg-green-500",
  updating: "bg-yellow-400",
  pending: "bg-yellow-400",
  disconnected: "bg-gray-400",
  error: "bg-red-500",
};

interface SidebarProps {
  collapsed?: boolean;
  onNavigate?: () => void;
  onExpand?: () => void;
}

function activeServerId(pathname: string, selected: string | null): string | null {
  const m = pathname.match(/^\/servers\/([^/]+)/);
  return m?.[1] || selected;
}

export function Sidebar({ collapsed, onNavigate, onExpand }: SidebarProps) {
  const pathname = usePathname();
  const router = useRouter();
  const { user, logout } = useAuth();
  const { servers, selectedServerId, selectServer, fetchServers } = useServerStore();
  const activeId = activeServerId(pathname, selectedServerId);

  useEffect(() => {
    fetchServers();
    const interval = setInterval(fetchServers, 15_000);
    return () => clearInterval(interval);
  }, [fetchServers]);

  const handleSelectServer = (id: string) => {
    selectServer(id);
    router.push(`/servers/${id}`);
    onNavigate?.();
  };

  const handleLogout = async () => {
    await logout();
    router.push("/login");
  };

  if (collapsed) {
    return (
      <aside className="flex h-full w-14 flex-col items-center border-r bg-white py-4 dark:border-gray-800 dark:bg-gray-900">
        <button
          type="button"
          onClick={onExpand}
          className="mb-6 flex h-8 w-8 items-center justify-center rounded-lg bg-blue-600 text-xs font-bold text-white"
          title="Open menu"
        >
          Y
        </button>
        <nav className="flex flex-1 flex-col items-center gap-2 overflow-y-auto">
          {servers.map((s) => (
            <button
              key={s.id}
              type="button"
              onClick={() => handleSelectServer(s.id)}
              className={`relative flex h-8 w-8 items-center justify-center rounded-lg text-xs font-medium transition ${
                activeId === s.id
                  ? "bg-blue-50 text-blue-700 dark:bg-blue-950 dark:text-blue-300"
                  : "text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800"
              }`}
              title={s.name}
            >
              {s.name.charAt(0).toUpperCase()}
              <span
                className={`absolute -right-0.5 -top-0.5 h-2.5 w-2.5 rounded-full border-2 border-white dark:border-gray-900 ${
                  statusColors[s.status] || "bg-gray-400"
                }`}
              />
            </button>
          ))}
          <button
            type="button"
            onClick={() => {
              router.push("/onboarding/connect-server");
              onNavigate?.();
            }}
            className="mt-1 flex h-8 w-8 items-center justify-center rounded-lg text-gray-400 hover:bg-gray-100 dark:hover:bg-gray-800"
            title="Add Server"
          >
            <Plus className="h-4 w-4" />
          </button>
        </nav>
      </aside>
    );
  }

  return (
    <aside className="flex h-full w-60 flex-col border-r bg-white dark:border-gray-800 dark:bg-gray-900">
      <div className="flex h-14 items-center gap-2 border-b px-4 dark:border-gray-800">
        <Link
          href="/overview"
          className="flex h-8 w-8 items-center justify-center rounded-lg bg-blue-600 text-xs font-bold text-white"
        >
          Y
        </Link>
        <span className="text-sm font-semibold text-gray-900 dark:text-white">YourPlatform</span>
      </div>

      <div className="flex-1 overflow-y-auto p-3">
        <div className="mb-2 px-3 text-xs font-medium uppercase tracking-wider text-gray-400">
          Servers
        </div>
        <div className="space-y-0.5">
          {servers.map((s) => {
            const isActive = activeId === s.id;
            return (
              <button
                key={s.id}
                type="button"
                onClick={() => handleSelectServer(s.id)}
                className={`flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm transition ${
                  isActive
                    ? "bg-blue-50 font-medium text-blue-700 dark:bg-blue-950 dark:text-blue-300"
                    : "text-gray-600 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800"
                }`}
              >
                <span
                  className={`h-2.5 w-2.5 shrink-0 rounded-full ${
                    statusColors[s.status] || "bg-gray-400"
                  }`}
                />
                <span className="truncate">{s.name}</span>
              </button>
            );
          })}
          {servers.length === 0 && (
            <p className="px-3 py-2 text-xs text-gray-400">No servers yet</p>
          )}
        </div>

        <button
          type="button"
          onClick={() => {
            router.push("/onboarding/connect-server");
            onNavigate?.();
          }}
          className="mt-2 flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800"
        >
          <Plus className="h-4 w-4" />
          Add Server
        </button>
      </div>

      <div className="border-t p-3 dark:border-gray-800">
        <Link
          href="/account"
          onClick={onNavigate}
          className="flex items-center gap-3 rounded-lg px-3 py-2 text-sm text-gray-600 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800"
        >
          <User className="h-4 w-4" />
          Account
        </Link>
        <a
          href="https://docs.yourplatform.app/support"
          target="_blank"
          rel="noopener noreferrer"
          className="flex items-center gap-3 rounded-lg px-3 py-2 text-sm text-gray-600 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800"
        >
          <LifeBuoy className="h-4 w-4" />
          Support
        </a>
        <div className="mt-2 border-t pt-2 dark:border-gray-800">
          <div className="truncate px-3 text-xs text-gray-500">{user?.email}</div>
          <button
            type="button"
            onClick={handleLogout}
            className="mt-1 flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm text-gray-600 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800"
          >
            <LogOut className="h-4 w-4" />
            Sign out
          </button>
        </div>
      </div>
    </aside>
  );
}
