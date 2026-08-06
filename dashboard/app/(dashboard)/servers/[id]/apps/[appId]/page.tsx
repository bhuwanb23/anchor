"use client";

import { Suspense, use } from "react";
import AppDetailClient from "./app-detail-client";

export default function Page({
  params,
}: {
  params: Promise<{ id: string; appId: string }>;
}) {
  const { id, appId } = use(params);
  return (
    <Suspense
      fallback={
        <div className="flex items-center gap-2 text-gray-500">
          <div className="h-4 w-4 animate-spin rounded-full border-2 border-blue-600 border-t-transparent" />
          Loading…
        </div>
      }
    >
      <AppDetailClient serverId={id} appId={appId} />
    </Suspense>
  );
}
