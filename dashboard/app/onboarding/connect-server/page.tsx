"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { useRouter } from "next/navigation";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { getWSClient } from "@/lib/ws";
import api from "@/lib/api";

const TOKEN_TTL_SEC = 60 * 60; // 60 minutes

export default function ConnectServerPage() {
  const router = useRouter();

  // Server + command state
  const [serverId, setServerId] = useState<string | null>(null);
  const [installCommand, setInstallCommand] = useState("");
  const [loading, setLoading] = useState(true);
  const [copied, setCopied] = useState(false);

  // Countdown
  const [expiresIn, setExpiresIn] = useState(TOKEN_TTL_SEC);
  const [expired, setExpired] = useState(false);

  // Connection state
  const [connected, setConnected] = useState(false);
  const [serverName, setServerName] = useState("");

  // Refs for cleanup
  const unsubRef = useRef<(() => void)[]>([]);

  // Fetch server + registration token
  const fetchCommand = useCallback(async () => {
    setLoading(true);
    setExpired(false);
    setExpiresIn(TOKEN_TTL_SEC);
    try {
      let sid = serverId;
      if (!sid) {
        const srvRes = await api.post("/api/v1/servers", { name: "My Server" });
        sid = srvRes.data.id;
        setServerId(sid);
      }
      const tokenRes = await api.post(`/api/v1/servers/${sid}/registration-token`);
      const host = window.location.origin.replace(/^https?:\/\//, "");
      const cmd = `curl -fsSL http://${host}/install.sh | sudo sh -s -- --token=${tokenRes.data.token} --base-url=http://${host}`;
      setInstallCommand(cmd);

      // Parse token expiry
      if (tokenRes.data.expires_at) {
        const expires = new Date(tokenRes.data.expires_at).getTime();
        const secs = Math.max(0, Math.floor((expires - Date.now()) / 1000));
        setExpiresIn(secs);
      }
    } catch {
      // handled by loading state
    } finally {
      setLoading(false);
    }
  }, [serverId]);

  // Initial fetch
  useEffect(() => {
    fetchCommand();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // Countdown timer
  useEffect(() => {
    if (expired || connected) return;
    const interval = setInterval(() => {
      setExpiresIn((prev) => {
        if (prev <= 1) {
          clearInterval(interval);
          setExpired(true);
          return 0;
        }
        return prev - 1;
      });
    }, 1000);
    return () => clearInterval(interval);
  }, [expired, connected]);

  // WebSocket listener for agent_connected
  useEffect(() => {
    if (!serverId) return;

    const client = getWSClient();

    const unsub = client.on("agent_connected", (msg) => {
      const payload = msg.payload as { server_id?: string } | undefined;
      if (payload?.server_id === serverId) {
        setConnected(true);
        setServerName("Your Server");
        // Auto-advance after 1.5s
        setTimeout(() => {
          router.push("/onboarding/first-deploy");
        }, 1500);
      }
    });
    unsubRef.current.push(unsub);

    client.connect();

    return () => {
      unsubRef.current.forEach((fn) => fn());
      unsubRef.current = [];
    };
  }, [serverId, router]);

  // Copy to clipboard
  const copyToClipboard = () => {
    navigator.clipboard.writeText(installCommand);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  // Format countdown
  const minutes = Math.floor(expiresIn / 60);
  const seconds = expiresIn % 60;
  const countdown = `${minutes}:${seconds.toString().padStart(2, "0")}`;

  return (
    <Card>
      <CardContent className="space-y-6 py-8">
        {/* Connected state */}
        {connected ? (
          <div className="space-y-4 text-center">
            <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/30">
              <svg className="h-8 w-8 text-green-600" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" d="M4.5 12.75l6 6 9-13.5" />
              </svg>
            </div>
            <div>
              <h2 className="text-xl font-semibold text-gray-900 dark:text-white">Server connected!</h2>
              <p className="mt-1 text-sm text-gray-500">{serverName}</p>
            </div>
            <p className="text-sm text-gray-400">Moving to deploy step...</p>
          </div>
        ) : (
          <>
            {/* Section 1: Instruction */}
            <div>
              <h2 className="text-xl font-semibold text-gray-900 dark:text-white">
                Run this command on your server
              </h2>
              <p className="mt-1 text-sm text-gray-500">
                This installs the agent and connects it to your dashboard.
              </p>
            </div>

            {/* Section 2: Install command */}
            {loading ? (
              <div className="h-24 animate-pulse rounded-lg bg-gray-200 dark:bg-gray-800" />
            ) : expired ? (
              <div className="space-y-3 rounded-lg border border-amber-200 bg-amber-50 p-4 dark:border-amber-800 dark:bg-amber-950/30">
                <p className="text-sm text-amber-700 dark:text-amber-300">
                  This command has expired.
                </p>
                <Button onClick={fetchCommand} size="sm">
                  Generate a new install command
                </Button>
              </div>
            ) : (
              <div className="relative">
                <pre className="overflow-x-auto rounded-lg bg-gray-900 p-4 font-mono text-sm text-green-400">
                  {installCommand}
                </pre>
                <Button
                  size="sm"
                  className="absolute top-2 right-2"
                  onClick={copyToClipboard}
                >
                  {copied ? (
                    <span className="flex items-center gap-1">
                      <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor">
                        <path strokeLinecap="round" strokeLinejoin="round" d="M4.5 12.75l6 6 9-13.5" />
                      </svg>
                      Copied
                    </span>
                  ) : (
                    "Copy"
                  )}
                </Button>
              </div>
            )}

            {/* Section 3: Countdown */}
            {!expired && !loading && (
              <p className="text-xs text-gray-400">
                This command expires in {countdown}
              </p>
            )}

            {/* Section 4: Waiting indicator */}
            {!expired && !loading && (
              <div className="space-y-2 text-center">
                <div className="flex items-center justify-center gap-2 text-sm text-gray-500">
                  <svg className="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                    <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                  </svg>
                  Waiting for your server to connect...
                </div>
                <p className="text-xs text-gray-400">
                  This page will update automatically when your server connects.
                </p>
              </div>
            )}

            {/* Section 5: Help */}
            <div className="pt-4 text-center">
              <a
                href="https://docs.yourplatform.app/getting-started/connect-server"
                target="_blank"
                rel="noopener noreferrer"
                className="text-xs text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
              >
                How do I open a terminal on my server?
              </a>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}
