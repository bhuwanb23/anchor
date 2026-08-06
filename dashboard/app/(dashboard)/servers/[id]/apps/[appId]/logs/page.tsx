"use client";

import { Suspense, use } from "react";
import { useRouter } from "next/navigation";
import { useEffect } from "react";

/** Redirect old /logs path to the app detail Logs tab. */
export default function AppLogsRedirect({
  params,
}: {
  params: Promise<{ id: string; appId: string }>;
}) {
  const { id, appId } = use(params);
  const router = useRouter();
  useEffect(() => {
    router.replace(`/servers/${id}/apps/${appId}?tab=logs`);
  }, [id, appId, router]);
  return (
    <Suspense>
      <p className="text-sm text-gray-500">Opening logs…</p>
    </Suspense>
  );
}
