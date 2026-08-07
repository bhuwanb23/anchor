"use client";

import Link from "next/link";
import { CloudOff, AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";

export function PageError({
  message = "We could not load this page. Try again in a moment.",
  requestId,
  onRetry,
}: {
  message?: string;
  requestId?: string | null;
  onRetry?: () => void;
}) {
  const rid = requestId || `req-${Date.now().toString(36)}`;
  return (
    <div className="mx-auto flex max-w-md flex-col items-center gap-4 py-16 text-center">
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-[var(--color-warning-soft)]">
        <AlertTriangle className="h-6 w-6 text-[var(--color-warning)]" />
      </div>
      <h2 className="text-xl font-bold tracking-tight text-[var(--color-ink)]">Something went wrong</h2>
      <p className="text-sm text-[var(--color-muted)]">{message}</p>
      <div className="flex flex-wrap items-center justify-center gap-3">
        {onRetry && (
          <Button onClick={onRetry}>Try again</Button>
        )}
        <Link href="/overview" className="text-sm font-semibold text-[var(--color-accent)] hover:underline">
          Go to dashboard
        </Link>
      </div>
      <p className="text-xs text-[var(--color-muted)]">
        If this keeps happening, contact support. Request ID: {rid}
      </p>
    </div>
  );
}

export function ServerDisconnectedBanner({ lastSeen }: { lastSeen?: string | null }) {
  const ago = lastSeen
    ? (() => {
        const mins = Math.floor((Date.now() - new Date(lastSeen).getTime()) / 60_000);
        if (mins < 1) return "just now";
        if (mins < 60) return `${mins} minute${mins === 1 ? "" : "s"} ago`;
        const hrs = Math.floor(mins / 60);
        return `${hrs} hour${hrs === 1 ? "" : "s"} ago`;
      })()
    : null;

  return (
    <div className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-paper-2)] p-5">
      <div className="flex gap-3">
        <CloudOff className="mt-0.5 h-5 w-5 shrink-0 text-[var(--color-muted)]" />
        <div className="space-y-1">
          <p className="font-semibold text-[var(--color-ink)]">
            Your server is not connected right now.
          </p>
          <p className="text-sm text-[var(--color-muted)]">
            Your apps are still running. This often happens during a brief network blip or agent restart.
          </p>
          <p className="text-sm text-[var(--color-muted)]">We will reconnect automatically.</p>
          {ago && (
            <p className="text-xs text-[var(--color-muted)]">Last connected {ago}</p>
          )}
        </div>
      </div>
    </div>
  );
}

export function ServerOverviewSkeleton() {
  return (
    <div className="mx-auto max-w-5xl space-y-8">
      <div className="flex justify-between">
        <Skeleton className="h-9 w-48" />
        <Skeleton className="h-10 w-36 rounded-[var(--radius-md)]" />
      </div>
      <div className="grid gap-4 sm:grid-cols-3">
        <Skeleton className="h-32 rounded-[var(--radius-lg)]" />
        <Skeleton className="h-32 rounded-[var(--radius-lg)]" />
        <Skeleton className="h-32 rounded-[var(--radius-lg)]" />
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Skeleton className="h-32 rounded-[var(--radius-lg)]" />
        <Skeleton className="h-32 rounded-[var(--radius-lg)]" />
      </div>
      <Skeleton className="h-4 w-full rounded" />
    </div>
  );
}

export function BackupsPageSkeleton() {
  return (
    <div className="space-y-6">
      <Skeleton className="h-8 w-40" />
      <Skeleton className="h-20 w-full rounded-[var(--radius-lg)]" />
      <Skeleton className="h-40 w-full rounded-[var(--radius-lg)]" />
      <div className="space-y-2">
        <Skeleton className="h-16 w-full rounded-[var(--radius-md)]" />
        <Skeleton className="h-16 w-full rounded-[var(--radius-md)]" />
        <Skeleton className="h-16 w-full rounded-[var(--radius-md)]" />
      </div>
    </div>
  );
}

export function FadeIn({ children }: { children: React.ReactNode }) {
  return <div className="opacity-100 transition-opacity duration-150">{children}</div>;
}
