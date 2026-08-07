"use client";

import { useEffect, useState, useCallback } from "react";
import { usePathname, useRouter } from "next/navigation";
import { isLoggedIn } from "@/lib/auth";
import { useAuth } from "@/hooks/use-auth";
import { useWSStore } from "@/store/ws-store";
import { useServerStore } from "@/store/server-store";
import { useRealtimeShell } from "@/hooks/use-realtime-shell";
import { useAlerts } from "@/hooks/use-alerts";
import { Sidebar } from "@/components/layout/sidebar";
import { Header } from "@/components/layout/header";
import { CriticalAlertBanner } from "@/components/alerts/alert-banner";

function serverIdFromPath(pathname: string): string | null {
  const m = pathname.match(/^\/servers\/([^/]+)/);
  return m ? m[1] : null;
}

function CriticalBannerHost({ serverId }: { serverId: string | null }) {
  useAlerts(serverId || "", !!serverId);
  const storeAlerts = useServerStore((s) => s.alerts);
  return <CriticalAlertBanner serverId={serverId} alerts={storeAlerts} />;
}

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const { user, isLoading, loadUser } = useAuth();
  const connect = useWSStore((s) => s.connect);
  const selectServer = useServerStore((s) => s.selectServer);
  const selectedServerId = useServerStore((s) => s.selectedServerId);
  const [mobileMenuOpen, setMobileMenuOpen] = useState(false);

  useEffect(() => {
    if (!isLoggedIn()) {
      router.replace("/login");
      return;
    }
    if (!user) {
      loadUser();
    }
  }, [user, loadUser, router]);

  useEffect(() => {
    connect();
  }, [connect]);

  useEffect(() => {
    const id = serverIdFromPath(pathname);
    if (id && id !== selectedServerId) {
      selectServer(id);
    }
  }, [pathname, selectedServerId, selectServer]);

  useRealtimeShell(selectedServerId);

  const closeMobileMenu = useCallback(() => setMobileMenuOpen(false), []);

  if (isLoading) {
    return (
      <div className="flex min-h-screen items-center justify-center bg-[var(--color-paper)]">
        <div className="h-9 w-9 animate-spin rounded-full border-[3px] border-[var(--color-accent-soft)] border-t-[var(--color-accent)]" />
      </div>
    );
  }

  return (
    <div className="flex h-screen overflow-hidden bg-[var(--color-paper)]">
      <div className="flex shrink-0 lg:hidden">
        <Sidebar collapsed onExpand={() => setMobileMenuOpen(true)} />
      </div>

      <div className="hidden lg:flex lg:shrink-0">
        <Sidebar />
      </div>

      {mobileMenuOpen && (
        <div className="fixed inset-0 z-50 lg:hidden">
          <div className="fixed inset-0 bg-[oklch(22%_0.02_165/0.4)]" onClick={closeMobileMenu} />
          <div className="fixed inset-y-0 left-0 z-50 w-64 shadow-[var(--shadow-lift)]">
            <Sidebar onNavigate={closeMobileMenu} />
          </div>
        </div>
      )}

      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <Header onMenuClick={() => setMobileMenuOpen(true)} />
        <CriticalBannerHost serverId={selectedServerId} />
        <main className="flex-1 overflow-y-auto px-4 py-5 sm:px-6 sm:py-6 lg:px-8 lg:py-8">
          {children}
        </main>
      </div>
    </div>
  );
}
