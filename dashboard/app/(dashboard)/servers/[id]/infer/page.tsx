"use client";

import { use, useEffect } from "react";
import { useRouter } from "next/navigation";
import { useServerStore } from "@/store/server-store";

/** Per-server Infer URL redirects to the canonical /infer page. */
export default function InferRedirect({ params }: { params: Promise<{ id: string }> }) {
  const { id } = use(params);
  const router = useRouter();
  const selectServer = useServerStore((s) => s.selectServer);

  useEffect(() => {
    if (id) selectServer(id);
    router.replace("/infer");
  }, [id, router, selectServer]);

  return null;
}
