"use client";

import { use, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import api from "@/lib/api";
import { isLoggedIn } from "@/lib/auth";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export default function AcceptInvitePage({
  params,
}: {
  params: Promise<{ token: string }>;
}) {
  const { token } = use(params);
  const router = useRouter();
  const [status, setStatus] = useState<"idle" | "working" | "done" | "error">("idle");
  const [message, setMessage] = useState("");

  useEffect(() => {
    if (!isLoggedIn()) {
      router.replace(`/login?next=${encodeURIComponent(`/invite/${token}`)}`);
    }
  }, [token, router]);

  const accept = async () => {
    setStatus("working");
    try {
      await api.post(`/api/v1/invitations/${token}/accept`);
      setStatus("done");
      toast.success("You joined the team");
      setTimeout(() => router.push("/overview"), 800);
    } catch (e) {
      const msg =
        (e as { response?: { data?: { error?: string } } })?.response?.data?.error ||
        "We could not accept this invitation. It may have expired.";
      setMessage(msg);
      setStatus("error");
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center px-4">
      <Card className="w-full max-w-md">
        <CardHeader>
          <CardTitle>Team invitation</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <p className="text-sm text-gray-600 dark:text-gray-300">
            You&apos;ve been invited to collaborate on a shared server.
          </p>
          {status === "error" && (
            <p className="text-sm text-red-600">{message}</p>
          )}
          {status === "done" ? (
            <p className="text-sm text-green-600">Joined — redirecting…</p>
          ) : (
            <Button onClick={accept} disabled={status === "working"} className="w-full">
              {status === "working" ? "Joining…" : "Accept invitation"}
            </Button>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
