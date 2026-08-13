"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";
import {
  LogOut,
  Plus,
  LifeBuoy,
  User,
  LayoutDashboard,
  Server,
  Bell,
  HardDrive,
  Cpu,
} from "lucide-react";
import { useAuth } from "@/hooks/use-auth";
import { useServerStore } from "@/store/server-store";
import type { ServerStatus } from "@/types";

const statusColors: Record<ServerStatus, string> = {
  connected: "bg-[var(--color-success)]",
  updating: "bg-[var(--color-warning)]",
  pending: "bg-[var(--color-warning)]",
  disconnected: "bg-[var(--color-muted)]",
  error: "bg-[var(--color-danger)]",
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

function NavLabel({ children }: { children: React.ReactNode }) {
  return (
    <div className="mb-2 px-3 text-[10px] font-bold uppercase tracking-[0.14em] text-[var(--color-muted)]">
      {children}
    </div>
  );
}

function NavItem({
  href,
  active,
  onClick,
  icon,
  children,
}: {
  href: string;
  active: boolean;
  onClick?: () => void;
  icon: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <Link
      href={href}
      onClick={onClick}
      className={`relative flex items-center gap-3 rounded-[var(--radius-md)] px-3 py-2.5 text-sm transition-colors duration-[var(--dur-fast)] ${
        active
          ? "bg-[var(--color-accent-soft)] font-semibold text-[var(--color-accent)]"
          : "font-medium text-[var(--color-muted)] hover:bg-[var(--color-paper-2)] hover:text-[var(--color-ink)]"
      }`}
    >
      {active && (
        <span className="absolute left-0 top-1/2 h-5 w-1 -translate-y-1/2 rounded-r-full bg-[var(--color-accent)]" />
      )}
      <span className="shrink-0 opacity-80">{icon}</span>
      <span className="truncate">{children}</span>
    </Link>
  );
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
      <aside className="flex h-full w-14 flex-col items-center border-r border-[var(--color-border)] bg-[var(--color-surface)] py-4">
        <button
          type="button"
          onClick={onExpand}
          className="mb-6 flex h-9 w-9 items-center justify-center rounded-[var(--radius-md)] bg-[var(--color-accent)] text-xs font-bold text-[var(--color-accent-fg)] shadow-sm"
          title="Open menu"
        >
          A
        </button>
        <nav className="flex flex-1 flex-col items-center gap-2 overflow-y-auto">
          {servers.map((s) => (
            <button
              key={s.id}
              type="button"
              onClick={() => handleSelectServer(s.id)}
              className={`relative flex h-9 w-9 items-center justify-center rounded-[var(--radius-md)] text-xs font-semibold transition ${
                activeId === s.id
                  ? "bg-[var(--color-accent-soft)] text-[var(--color-accent)]"
                  : "text-[var(--color-muted)] hover:bg-[var(--color-paper-2)]"
              }`}
              title={s.name}
            >
              {s.name.charAt(0).toUpperCase()}
              <span
                className={`absolute -right-0.5 -top-0.5 h-2.5 w-2.5 rounded-full border-2 border-[var(--color-surface)] ${
                  statusColors[s.status] || "bg-[var(--color-muted)]"
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
            className="mt-1 flex h-9 w-9 items-center justify-center rounded-[var(--radius-md)] text-[var(--color-muted)] hover:bg-[var(--color-paper-2)]"
            title="Add Server"
          >
            <Plus className="h-4 w-4" />
          </button>
        </nav>
      </aside>
    );
  }

  return (
    <aside className="flex h-full w-64 flex-col border-r border-[var(--color-border)] bg-[var(--color-surface)]">
      <div className="flex h-16 items-center gap-3 border-b border-[var(--color-border)] px-5">
        <Link
          href="/overview"
          className="flex h-9 w-9 items-center justify-center rounded-[var(--radius-md)] bg-[var(--color-accent)] text-sm font-extrabold text-[var(--color-accent-fg)] shadow-sm"
        >
          A
        </Link>
        <div className="min-w-0">
          <Link href="/overview" className="block truncate text-base font-extrabold tracking-tight text-[var(--color-ink)]">
            Anchor
          </Link>
          <p className="truncate text-[10px] font-medium uppercase tracking-wider text-[var(--color-muted)]">
            Ops console
          </p>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto px-3 py-4">
        <NavLabel>Menu</NavLabel>
        <div className="mb-5 space-y-0.5">
          <NavItem
            href="/overview"
            active={pathname === "/overview"}
            onClick={onNavigate}
            icon={<LayoutDashboard className="h-4 w-4" />}
          >
            Overview
          </NavItem>
          <NavItem
            href="/servers"
            active={pathname === "/servers"}
            onClick={onNavigate}
            icon={<Server className="h-4 w-4" />}
          >
            Servers
          </NavItem>
          <NavItem
            href="/infer"
            active={pathname === "/infer" || pathname.startsWith("/infer")}
            onClick={onNavigate}
            icon={<Cpu className="h-4 w-4" />}
          >
            Infer
          </NavItem>
        </div>

        <NavLabel>Servers</NavLabel>
        <div className="mb-2 space-y-0.5">
          {servers.map((s) => {
            const isActive = activeId === s.id;
            return (
              <button
                key={s.id}
                type="button"
                onClick={() => handleSelectServer(s.id)}
                className={`relative flex w-full items-center gap-3 rounded-[var(--radius-md)] px-3 py-2.5 text-sm transition-colors ${
                  isActive
                    ? "bg-[var(--color-accent-soft)] font-semibold text-[var(--color-accent)]"
                    : "font-medium text-[var(--color-muted)] hover:bg-[var(--color-paper-2)] hover:text-[var(--color-ink)]"
                }`}
              >
                {isActive && (
                  <span className="absolute left-0 top-1/2 h-5 w-1 -translate-y-1/2 rounded-r-full bg-[var(--color-accent)]" />
                )}
                <span
                  className={`h-2 w-2 shrink-0 rounded-full ${
                    statusColors[s.status] || "bg-[var(--color-muted)]"
                  }`}
                />
                <span className="truncate">{s.name}</span>
              </button>
            );
          })}
          {servers.length === 0 && (
            <p className="px-3 py-2 text-xs text-[var(--color-muted)]">No servers yet</p>
          )}
        </div>

        <button
          type="button"
          onClick={() => {
            router.push("/onboarding/connect-server");
            onNavigate?.();
          }}
          className="mb-5 flex w-full items-center gap-2 rounded-[var(--radius-md)] px-3 py-2.5 text-sm font-medium text-[var(--color-muted)] hover:bg-[var(--color-paper-2)] hover:text-[var(--color-accent)]"
        >
          <Plus className="h-4 w-4" />
          Add Server
        </button>

        {activeId && (
          <>
            <NavLabel>Server tools</NavLabel>
            <div className="space-y-0.5">
              <NavItem
                href={`/servers/${activeId}/alerts`}
                active={pathname.includes("/alerts")}
                onClick={onNavigate}
                icon={<Bell className="h-4 w-4" />}
              >
                Alerts
              </NavItem>
              <NavItem
                href={`/servers/${activeId}/backups`}
                active={pathname.includes("/backups")}
                onClick={onNavigate}
                icon={<HardDrive className="h-4 w-4" />}
              >
                Backups
              </NavItem>
            </div>
          </>
        )}
      </div>

      <div className="border-t border-[var(--color-border)] p-3">
        <NavLabel>General</NavLabel>
        <Link
          href="/account"
          onClick={onNavigate}
          className="flex items-center gap-3 rounded-[var(--radius-md)] px-3 py-2 text-sm font-medium text-[var(--color-muted)] hover:bg-[var(--color-paper-2)] hover:text-[var(--color-ink)]"
        >
          <User className="h-4 w-4" />
          Account
        </Link>
        <a
          href="https://docs.yourplatform.app/support"
          target="_blank"
          rel="noopener noreferrer"
          className="flex items-center gap-3 rounded-[var(--radius-md)] px-3 py-2 text-sm font-medium text-[var(--color-muted)] hover:bg-[var(--color-paper-2)] hover:text-[var(--color-ink)]"
        >
          <LifeBuoy className="h-4 w-4" />
          Support
        </a>
        <div className="mt-2 border-t border-[var(--color-border)] pt-2">
          <div className="truncate px-3 text-xs text-[var(--color-muted)]">{user?.email}</div>
          <button
            type="button"
            onClick={handleLogout}
            className="mt-1 flex w-full items-center gap-3 rounded-[var(--radius-md)] px-3 py-2 text-sm font-medium text-[var(--color-muted)] hover:bg-[var(--color-paper-2)] hover:text-[var(--color-ink)]"
          >
            <LogOut className="h-4 w-4" />
            Sign out
          </button>
        </div>
      </div>
    </aside>
  );
}
