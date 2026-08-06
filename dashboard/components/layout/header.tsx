"use client";

import { useState, useEffect, useRef } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { LogOut, Settings, User, Menu } from "lucide-react";
import { useAuth } from "@/hooks/use-auth";
import { useWSStore } from "@/store/ws-store";
import { useServerStore } from "@/store/server-store";
import NotificationCenter from "@/components/dashboard/notification-center";

function getTitle(pathname: string, serverName?: string): string {
  if (pathname === "/overview") return "Overview";
  if (pathname === "/servers") return "Servers";
  if (pathname === "/account") return "Account Settings";
  if (pathname.match(/^\/servers\/[^/]+\/backups$/)) return "Backups";
  if (pathname.match(/^\/servers\/[^/]+\/alerts$/)) return "Alerts";
  if (pathname.match(/^\/servers\/[^/]+\/apps\/new$/)) return "Deploy New App";
  if (pathname.match(/^\/servers\/[^/]+\/apps\/[^/]+/)) return "App";
  if (pathname.match(/^\/servers\/[^/]+$/)) return serverName || "Server";
  return "Dashboard";
}

interface HeaderProps {
  onMenuClick?: () => void;
}

export function Header({ onMenuClick }: HeaderProps) {
  const pathname = usePathname();
  const router = useRouter();
  const { user, logout } = useAuth();
  const wsStatus = useWSStore((s) => s.status);
  const servers = useServerStore((s) => s.servers);
  const selectedServerId = useServerStore((s) => s.selectedServerId);

  const [dropdownOpen, setDropdownOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);

  const pathServerId = pathname.match(/^\/servers\/([^/]+)/)?.[1];
  const server = servers.find((s) => s.id === (pathServerId || selectedServerId));
  const title = getTitle(pathname, server?.name);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setDropdownOpen(false);
      }
    };
    document.addEventListener("mousedown", handler);
    return () => document.removeEventListener("mousedown", handler);
  }, []);

  const handleLogout = async () => {
    setDropdownOpen(false);
    await logout();
    router.push("/login");
  };

  return (
    <header className="sticky top-0 z-40 flex h-14 items-center justify-between border-b border-gray-200 bg-white/80 px-4 backdrop-blur dark:border-gray-800 dark:bg-gray-900/80 sm:px-6">
      <div className="flex items-center gap-3">
        <button
          type="button"
          onClick={onMenuClick}
          className="rounded-lg p-1.5 text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-800 lg:hidden"
          aria-label="Open menu"
        >
          <Menu className="h-5 w-5" />
        </button>
        <h1 className="truncate text-lg font-semibold text-gray-900 dark:text-white">{title}</h1>
      </div>

      <div className="flex items-center gap-2">
        <span
          className={`h-2 w-2 rounded-full ${
            wsStatus === "connected"
              ? "bg-green-500"
              : wsStatus === "connecting"
              ? "animate-pulse bg-yellow-400"
              : "bg-gray-400"
          }`}
          title={
            wsStatus === "connected"
              ? "Connected to control plane"
              : wsStatus === "connecting"
              ? "Reconnecting..."
              : "Disconnected"
          }
        />

        <NotificationCenter />

        <div className="relative" ref={dropdownRef}>
          <button
            type="button"
            onClick={() => setDropdownOpen(!dropdownOpen)}
            className="flex items-center gap-2 rounded-lg px-2 py-1.5 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800"
          >
            <div className="flex h-7 w-7 items-center justify-center rounded-full bg-blue-100 text-xs font-medium text-blue-700 dark:bg-blue-900 dark:text-blue-300">
              {user?.name?.charAt(0)?.toUpperCase() || user?.email?.charAt(0)?.toUpperCase() || "U"}
            </div>
            <span className="hidden max-w-[8rem] truncate sm:inline">{user?.name || user?.email}</span>
          </button>

          {dropdownOpen && (
            <div className="absolute right-0 top-full z-50 mt-1 w-48 rounded-lg border border-gray-200 bg-white py-1 shadow-lg dark:border-gray-700 dark:bg-gray-900">
              <div className="border-b px-3 py-2 dark:border-gray-700">
                <p className="text-sm font-medium text-gray-900 dark:text-white">{user?.name}</p>
                <p className="text-xs text-gray-500">{user?.email}</p>
              </div>
              <Link
                href="/account"
                onClick={() => setDropdownOpen(false)}
                className="flex items-center gap-2 px-3 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800"
              >
                <User className="h-4 w-4" />
                Account Settings
              </Link>
              <Link
                href="/account"
                onClick={() => setDropdownOpen(false)}
                className="flex items-center gap-2 px-3 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800"
              >
                <Settings className="h-4 w-4" />
                Settings
              </Link>
              <div className="border-t dark:border-gray-700" />
              <button
                type="button"
                onClick={handleLogout}
                className="flex w-full items-center gap-2 px-3 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-800"
              >
                <LogOut className="h-4 w-4" />
                Logout
              </button>
            </div>
          )}
        </div>
      </div>
    </header>
  );
}
