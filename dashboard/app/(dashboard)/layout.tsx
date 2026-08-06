"use client";

import { useEffect, useState, useCallback } from "react";
import { usePathname, useRouter } from "next/navigation";
import { isLoggedIn } from "@/lib/auth";
import { useAuth } from "@/hooks/use-auth";
import { useWSStore } from "@/store/ws-store";
import { useServerStore } from "@/store/server-store";
import { useRealtimeShell } from "@/hooks/use-realtime-shell";
import { Sidebar } from "@/components/layout/sidebar";
import { Header } from "@/components/layout/header";

function serverIdFromPath(pathname: string): string | null {
  const m = pathname.match(/^\/servers\/([^/]+)/);
  return m ? m[1] : null;
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
    if (!user && !isLoading) {
      loadUser();
    }
  }, [user, isLoading, loadUser, router]);

  useEffect(() => {
    connect();
  }, [connect]);

  // Keep store selection in sync with URL (survives refresh / deep links)
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
      <div className="flex min-h-screen items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-4 border-blue-600 border-t-transparent" />
      </div>
    );
  }

  return (
    <div className="flex h-screen overflow-hidden">
      {/* Icon bar on small/medium screens */}
      <div className="flex shrink-0 lg:hidden">
        <Sidebar collapsed onExpand={() => setMobileMenuOpen(true)} />
      </div>

      {/* Full sidebar on large screens */}
      <div className="hidden lg:flex lg:shrink-0">
        <Sidebar />
      </div>

      {/* Full sidebar overlay (hamburger) */}
      {mobileMenuOpen && (
        <div className="fixed inset-0 z-50 lg:hidden">
          <div className="fixed inset-0 bg-black/40" onClick={closeMobileMenu} />
          <div className="fixed inset-y-0 left-0 z-50 w-60 shadow-xl">
            <Sidebar onNavigate={closeMobileMenu} />
          </div>
        </div>
      )}

      <div className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <Header onMenuClick={() => setMobileMenuOpen(true)} />
        <main className="flex-1 overflow-y-auto p-4 sm:p-6 lg:p-8">{children}</main>
      </div>
    </div>
  );
}
