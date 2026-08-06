"use client";

import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import api from "@/lib/api";

export default function ConnectServerPage() {
  const router = useRouter();
  const [installCommand, setInstallCommand] = useState("");
  const [loading, setLoading] = useState(true);
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    (async () => {
      try {
        const srvRes = await api.post("/api/v1/servers", { name: "My Server" });
        const serverId = srvRes.data.id;
        const tokenRes = await api.post(`/api/v1/servers/${serverId}/registration-token`);
        const host = window.location.origin.replace(/^https?:\/\//, "");
        const cmd = `curl -fsSL http://${host}/install.sh | sudo sh -s -- --token=${tokenRes.data.token} --base-url=http://${host}`;
        setInstallCommand(cmd);
      } catch {
        // handled below
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  const copyToClipboard = () => {
    navigator.clipboard.writeText(installCommand);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Connect your server</CardTitle>
      </CardHeader>
      <CardContent className="space-y-6">
        <p className="text-sm text-gray-600 dark:text-gray-400">
          Copy the command below and run it on your server. This installs the agent and connects it to your dashboard.
        </p>
        {loading ? (
          <div className="h-20 animate-pulse rounded bg-gray-200 dark:bg-gray-800" />
        ) : (
          <div className="relative">
            <pre className="overflow-x-auto rounded-lg bg-gray-900 p-4 text-sm text-green-400">
              {installCommand}
            </pre>
            <Button
              size="sm"
              className="absolute top-2 right-2"
              onClick={copyToClipboard}
            >
              {copied ? "Copied!" : "Copy"}
            </Button>
          </div>
        )}
        <div className="flex justify-end gap-3">
          <Button variant="ghost" onClick={() => router.push("/overview")}>
            Skip for now
          </Button>
          <Button onClick={() => router.push("/onboarding/first-deploy")}>
            Continue
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
