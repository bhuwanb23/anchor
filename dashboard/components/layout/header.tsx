"use client";

import { useState, useEffect, useRef } from "react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { LogOut, Settings, User, Menu, Plus, Rocket } from "lucide-react";
import { useAuth } from "@/hooks/use-auth";
import { useWSStore } from "@/store/ws-store";
import { useServerStore } from "@/store/server-store";
import NotificationCenter from "@/components/dashboard/notification-center";
import { Button } from "@/components/ui/button";

function getPageMeta(pathname: string, serverName?: string): { title: string; subtitle: string } {
  if (pathname === "/overview") {
    return { title: "Overview", subtitle: "Fleet health at a glance" };
  }
  if (pathname === "/servers") {
    return { title: "Servers", subtitle: "Manage your connected machines" };
  }
  if (pathname === "/account") {
    return { title: "Account", subtitle: "Profile, team, and billing" };
  }
  if (pathname.match(/^\/servers\/[^/]+\/backups$/)) {
    return { title: "Backups", subtitle: "Snapshots and restore points" };
  }
  if (pathname.match(/^\/servers\/[^/]+\/alerts$/)) {
    return { title: "Alerts", subtitle: "Issues that need attention" };
  }
  if (pathname.match(/^\/servers\/[^/]+\/apps\/new$/)) {
    return { title: "Deploy", subtitle: "Ship a new app to this server" };
  }
  if (pathname.match(/^\/servers\/[^/]+\/apps\/[^/]+/)) {
    return { title: "App", subtitle: "Runtime, deploys, and logs" };
  }
  if (pathname.match(/^\/servers\/[^/]+$/)) {
    return { title: serverName || "Server", subtitle: "Metrics, apps, and status" };
  }
  return { title: "Dashboard", subtitle: "Your calm ops console" };
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
  const { title, subtitle } = getPageMeta(pathname, server?.name);

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

  const primaryCta =
    pathname === "/overview" || pathname === "/servers"
      ? {
          label: "Connect server",
          href: "/onboarding/connect-server",
          icon: <Plus className="h-4 w-4" />,
        }
      : pathServerId && !pathname.includes("/apps/new")
        ? {
            label: "Deploy app",
            href: `/servers/${pathServerId}/apps/new`,
            icon: <Rocket className="h-4 w-4" />,
          }
        : null;

  return (
    <header className="sticky top-0 z-40 border-b border-[var(--color-border)] bg-[var(--color-surface)]/85 px-4 py-4 backdrop-blur-md sm:px-6 lg:px-8">
      <div className="flex items-start justify-between gap-4">
        <div className="flex min-w-0 items-start gap-3">
          <button
            type="button"
            onClick={onMenuClick}
            className="mt-1 rounded-[var(--radius-sm)] p-1.5 text-[var(--color-muted)] hover:bg-[var(--color-paper-2)] lg:hidden"
            aria-label="Open menu"
          >
            <Menu className="h-5 w-5" />
          </button>
          <div className="min-w-0">
            <h1 className="truncate text-2xl font-extrabold tracking-tight text-[var(--color-ink)] sm:text-[1.75rem]">
              {title}
            </h1>
            <p className="mt-0.5 truncate text-sm text-[var(--color-muted)]">{subtitle}</p>
          </div>
        </div>

        <div className="flex shrink-0 items-center gap-2 sm:gap-3">
          <div
            className={`hidden items-center gap-2 rounded-full px-2.5 py-1 text-xs font-semibold sm:flex ${
              wsStatus === "connected"
                ? "bg-[var(--color-success-soft)] text-[var(--color-success)]"
                : wsStatus === "connecting"
                  ? "bg-[var(--color-warning-soft)] text-[var(--color-warning)]"
                  : "bg-[var(--color-paper-2)] text-[var(--color-muted)]"
            }`}
            title={
              wsStatus === "connected"
                ? "Connected to control plane"
                : wsStatus === "connecting"
                  ? "Reconnecting..."
                  : "Disconnected"
            }
          >
            <span
              className={`h-1.5 w-1.5 rounded-full ${
                wsStatus === "connected"
                  ? "bg-[var(--color-success)]"
                  : wsStatus === "connecting"
                    ? "animate-pulse bg-[var(--color-warning)]"
                    : "bg-[var(--color-muted)]"
              }`}
            />
            {wsStatus === "connected"
              ? "Live"
              : wsStatus === "connecting"
                ? "Reconnecting"
                : "Offline"}
          </div>

          {primaryCta && (
            <Button
              size="sm"
              className="hidden gap-1.5 sm:inline-flex"
              onClick={() => router.push(primaryCta.href)}
            >
              {primaryCta.icon}
              {primaryCta.label}
            </Button>
          )}

          <NotificationCenter />

          <div className="relative" ref={dropdownRef}>
            <button
              type="button"
              onClick={() => setDropdownOpen(!dropdownOpen)}
              className="flex items-center gap-2 rounded-[var(--radius-md)] px-2 py-1.5 text-sm text-[var(--color-ink)] hover:bg-[var(--color-paper-2)]"
            >
              <div className="flex h-8 w-8 items-center justify-center rounded-full bg-[var(--color-accent-soft)] text-xs font-bold text-[var(--color-accent)]">
                {user?.name?.charAt(0)?.toUpperCase() || user?.email?.charAt(0)?.toUpperCase() || "U"}
              </div>
              <span className="hidden max-w-[8rem] truncate font-medium sm:inline">
                {user?.name || user?.email}
              </span>
            </button>

            {dropdownOpen && (
              <div className="absolute right-0 top-full z-50 mt-2 w-52 overflow-hidden rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] py-1 shadow-[var(--shadow-lift)]">
                <div className="border-b border-[var(--color-border)] px-3 py-2.5">
                  <p className="text-sm font-semibold text-[var(--color-ink)]">{user?.name}</p>
                  <p className="text-xs text-[var(--color-muted)]">{user?.email}</p>
                </div>
                <Link
                  href="/account"
                  onClick={() => setDropdownOpen(false)}
                  className="flex items-center gap-2 px-3 py-2 text-sm text-[var(--color-ink)] hover:bg-[var(--color-paper-2)]"
                >
                  <User className="h-4 w-4" />
                  Account Settings
                </Link>
                <Link
                  href="/account"
                  onClick={() => setDropdownOpen(false)}
                  className="flex items-center gap-2 px-3 py-2 text-sm text-[var(--color-ink)] hover:bg-[var(--color-paper-2)]"
                >
                  <Settings className="h-4 w-4" />
                  Settings
                </Link>
                <div className="border-t border-[var(--color-border)]" />
                <button
                  type="button"
                  onClick={handleLogout}
                  className="flex w-full items-center gap-2 px-3 py-2 text-sm text-[var(--color-ink)] hover:bg-[var(--color-paper-2)]"
                >
                  <LogOut className="h-4 w-4" />
                  Logout
                </button>
              </div>
            )}
          </div>
        </div>
      </div>
    </header>
  );
}
