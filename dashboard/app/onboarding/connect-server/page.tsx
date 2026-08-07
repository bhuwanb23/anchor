"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { useRouter } from "next/navigation";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { getWSClient } from "@/lib/ws";
import api from "@/lib/api";

const TOKEN_TTL_SEC = 60 * 60;

type Method = "console" | "cloudinit";
type Provider = "digitalocean" | "hetzner" | "linode" | "other";

const PROVIDERS: {
  id: Provider;
  name: string;
  consoleLabel: string;
  steps: string[];
  createUrl: string;
}[] = [
  {
    id: "digitalocean",
    name: "DigitalOcean",
    consoleLabel: "Access → Launch Droplet Console",
    createUrl: "https://cloud.digitalocean.com/droplets/new",
    steps: [
      "Open your Droplet in the DigitalOcean control panel.",
      "Click Access → Launch Droplet Console (browser terminal).",
      "Paste the install command below and press Enter.",
      "Wait here — this page updates when the agent connects.",
    ],
  },
  {
    id: "hetzner",
    name: "Hetzner",
    consoleLabel: "Console in Cloud Console",
    createUrl: "https://console.hetzner.cloud/projects",
    steps: [
      "Open your server in the Hetzner Cloud Console.",
      "Click Console to open the web terminal.",
      "Paste the install command and press Enter.",
      "Come back here — we detect the connection automatically.",
    ],
  },
  {
    id: "linode",
    name: "Linode / Akamai",
    consoleLabel: "Launch LISH Console",
    createUrl: "https://cloud.linode.com/linodes",
    steps: [
      "Open your Linode in the Cloud Manager.",
      "Click Launch LISH Console.",
      "Paste the install command and press Enter.",
      "This page advances when your server connects.",
    ],
  },
  {
    id: "other",
    name: "Other VPS",
    consoleLabel: "your provider’s web console or SSH",
    createUrl: "",
    steps: [
      "Open your provider’s web console (or SSH if you already use it).",
      "Log in as root (or a user with sudo).",
      "Paste the install command and press Enter.",
      "Stay on this page — it updates by itself.",
    ],
  },
];

function apiBaseUrl(): string {
  const env = process.env.NEXT_PUBLIC_API_URL;
  if (env) return env.replace(/\/$/, "");
  if (typeof window !== "undefined") {
    // Prefer same host :8080 in local dev when UI is on :3000
    const { protocol, hostname } = window.location;
    if (hostname === "localhost" || hostname === "127.0.0.1") {
      return `${protocol}//${hostname}:8080`;
    }
    return window.location.origin;
  }
  return "http://localhost:8080";
}

function buildInstallCmd(token: string): string {
  const base = apiBaseUrl();
  return `curl -fsSL ${base}/install.sh | sudo sh -s -- --token=${token} --base-url=${base}`;
}

function buildCloudInit(token: string): string {
  const cmd = buildInstallCmd(token);
  return `#cloud-config
# Paste this into "User data" / "Cloud-init" when creating the server.
# The agent installs on first boot — no console needed.

runcmd:
  - ${cmd}
`;
}

export default function ConnectServerPage() {
  const router = useRouter();
  const [serverId, setServerId] = useState<string | null>(null);
  const [token, setToken] = useState("");
  const [installCommand, setInstallCommand] = useState("");
  const [cloudInit, setCloudInit] = useState("");
  const [loading, setLoading] = useState(true);
  const [copied, setCopied] = useState<"cmd" | "cloud" | null>(null);
  const [expiresIn, setExpiresIn] = useState(TOKEN_TTL_SEC);
  const [expired, setExpired] = useState(false);
  const [connected, setConnected] = useState(false);
  const [serverName, setServerName] = useState("");
  const [provider, setProvider] = useState<Provider>("digitalocean");
  const [method, setMethod] = useState<Method>("console");
  const [fetchError, setFetchError] = useState<string | null>(null);
  const unsubRef = useRef<(() => void)[]>([]);

  const fetchCommand = useCallback(async () => {
    setLoading(true);
    setExpired(false);
    setExpiresIn(TOKEN_TTL_SEC);
    setFetchError(null);
    try {
      let sid = serverId;
      if (!sid) {
        const srvRes = await api.post("/api/v1/servers", { name: "My Server" });
        sid = srvRes.data.id;
        setServerId(sid);
      }
      const tokenRes = await api.post<{ token: string; expires_at?: string }>(
        `/api/v1/servers/${sid}/registration-token`
      );
      const t = tokenRes.data.token;
      setToken(t);
      setInstallCommand(buildInstallCmd(t));
      setCloudInit(buildCloudInit(t));
      if (tokenRes.data.expires_at) {
        const expires = new Date(tokenRes.data.expires_at).getTime();
        setExpiresIn(Math.max(0, Math.floor((expires - Date.now()) / 1000)));
      }
    } catch (e) {
      setFetchError(
        e instanceof Error ? e.message : "Could not create an install command. Try again."
      );
    } finally {
      setLoading(false);
    }
  }, [serverId]);

  useEffect(() => {
    fetchCommand();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

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

  useEffect(() => {
    if (!serverId) return;
    const client = getWSClient();
    const unsub = client.on("agent_connected", (msg) => {
      const payload = msg.payload as { server_id?: string; name?: string } | undefined;
      if (payload?.server_id === serverId) {
        setConnected(true);
        setServerName(payload.name || "Your Server");
        setTimeout(() => router.push("/onboarding/first-deploy"), 1500);
      }
    });
    unsubRef.current.push(unsub);
    client.connect();
    return () => {
      unsubRef.current.forEach((fn) => fn());
      unsubRef.current = [];
    };
  }, [serverId, router]);

  // Refresh generated scripts when token exists but base URL helpers need recompute
  useEffect(() => {
    if (!token) return;
    setInstallCommand(buildInstallCmd(token));
    setCloudInit(buildCloudInit(token));
  }, [token]);

  const copy = async ( which: "cmd" | "cloud") => {
    const text = which === "cmd" ? installCommand : cloudInit;
    await navigator.clipboard.writeText(text);
    setCopied(which);
    setTimeout(() => setCopied(null), 2000);
  };

  const skipForLater = () => {
    router.push("/overview");
  };

  const minutes = Math.floor(expiresIn / 60);
  const seconds = expiresIn % 60;
  const countdown = `${minutes}:${seconds.toString().padStart(2, "0")}`;
  const activeProvider = PROVIDERS.find((p) => p.id === provider)!;

  return (
    <Card>
      <CardContent className="space-y-6 py-8">
        {connected ? (
          <div className="space-y-4 text-center">
            <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/30">
              <svg className="h-8 w-8 text-green-600" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" d="M4.5 12.75l6 6 9-13.5" />
              </svg>
            </div>
            <div>
              <h2 className="text-xl font-semibold text-[var(--color-ink)]">Server connected!</h2>
              <p className="mt-1 text-sm text-[var(--color-muted)]">{serverName}</p>
            </div>
            <p className="text-sm text-[var(--color-muted)]">Moving to deploy step…</p>
          </div>
        ) : (
          <>
            <div>
              <h2 className="text-xl font-semibold text-[var(--color-ink)]">
                Connect your server
              </h2>
              <p className="mt-1 text-sm text-[var(--color-muted)]">
                One-time setup. After this, you manage everything from the dashboard — no terminal needed.
              </p>
            </div>

            {/* Method tabs */}
            <div className="flex gap-2 rounded-lg bg-[var(--color-paper-2)] p-1">
              <button
                type="button"
                onClick={() => setMethod("console")}
                className={`flex-1 rounded-md px-3 py-2 text-sm font-medium ${
                  method === "console"
                    ? "bg-[var(--color-surface)] text-[var(--color-ink)] shadow"
                    : "text-[var(--color-muted)]"
                }`}
              >
                Paste in console
              </button>
              <button
                type="button"
                onClick={() => setMethod("cloudinit")}
                className={`flex-1 rounded-md px-3 py-2 text-sm font-medium ${
                  method === "cloudinit"
                    ? "bg-[var(--color-surface)] text-[var(--color-ink)] shadow"
                    : "text-[var(--color-muted)]"
                }`}
              >
                Cloud-init (auto-install)
              </button>
            </div>

            {/* Provider picker */}
            <div>
              <p className="mb-2 text-xs font-medium text-[var(--color-muted)]">Where is your server?</p>
              <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
                {PROVIDERS.map((p) => (
                  <button
                    key={p.id}
                    type="button"
                    onClick={() => setProvider(p.id)}
                    className={`rounded-lg border px-3 py-2 text-sm ${
                      provider === p.id
                        ? "border-[var(--color-accent)] bg-[var(--color-accent-soft)] font-medium text-[var(--color-accent)]"
                        : "border-[var(--color-border)] text-gray-700 hover:border-gray-300 border-[var(--color-border)] text-[var(--color-muted)]"
                    }`}
                  >
                    {p.name}
                  </button>
                ))}
              </div>
            </div>

            {method === "console" ? (
              <>
                {/* Visual step strip (screenshot substitute) */}
                <div className="overflow-hidden rounded-xl border border-[var(--color-border)]">
                  <div className="bg-gradient-to-br from-slate-800 to-slate-900 px-4 py-3 text-xs text-slate-300">
                    {activeProvider.name} · web console
                  </div>
                  <div className="grid gap-0 sm:grid-cols-3">
                    {["Open console", "Paste command", "Wait here"].map((label, i) => (
                      <div
                        key={label}
                        className="border-t border-[var(--color-border)] bg-[var(--color-paper-2)] p-4 border-[var(--color-border)] /50 sm:border-t-0 sm:border-l first:sm:border-l-0"
                      >
                        <div className="mb-2 flex h-8 w-8 items-center justify-center rounded-full bg-[var(--color-accent)] text-sm font-bold text-white">
                          {i + 1}
                        </div>
                        <p className="text-sm font-medium text-[var(--color-ink)]">{label}</p>
                        <p className="mt-1 text-xs text-[var(--color-muted)]">
                          {i === 0 && activeProvider.consoleLabel}
                          {i === 1 && "Use the Copy button below"}
                          {i === 2 && "We detect the connection"}
                        </p>
                      </div>
                    ))}
                  </div>
                </div>

                <ol className="list-decimal space-y-2 pl-5 text-sm text-[var(--color-muted)]">
                  {activeProvider.steps.map((s) => (
                    <li key={s}>{s}</li>
                  ))}
                </ol>

                {activeProvider.createUrl && (
                  <a
                    href={activeProvider.createUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-block text-sm text-[var(--color-accent)] hover:underline"
                  >
                    Open {activeProvider.name} →
                  </a>
                )}
              </>
            ) : (
              <>
                <div className="rounded-xl border border-emerald-200 bg-emerald-50 p-4 text-sm text-emerald-900 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-100">
                  <p className="font-medium">No console needed</p>
                  <p className="mt-1">
                    When creating a new droplet/server, paste the cloud-init script into{" "}
                    <strong>User data</strong> (DigitalOcean), <strong>Cloud config</strong>{" "}
                    (Hetzner), or equivalent. The agent installs on first boot.
                  </p>
                </div>
                <ol className="list-decimal space-y-2 pl-5 text-sm text-[var(--color-muted)]">
                  <li>Create a new Ubuntu 22.04+ server at your provider.</li>
                  <li>Find “User data” / “Cloud-init” / “Initialization script”.</li>
                  <li>Paste the script below (Copy button).</li>
                  <li>Create the server, then wait on this page.</li>
                </ol>
                {activeProvider.createUrl && (
                  <a
                    href={activeProvider.createUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="inline-block text-sm text-[var(--color-accent)] hover:underline"
                  >
                    Create on {activeProvider.name} →
                  </a>
                )}
              </>
            )}

            {fetchError && (
              <p className="text-sm text-red-600">{fetchError}</p>
            )}

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
                <pre className="max-h-48 overflow-auto rounded-lg bg-gray-900 p-4 font-mono text-xs text-green-400 whitespace-pre-wrap break-all">
                  {method === "console" ? installCommand : cloudInit}
                </pre>
                <Button
                  size="sm"
                  className="absolute top-2 right-2"
                  onClick={() => copy(method === "console" ? "cmd" : "cloud")}
                >
                  {copied ? "Copied" : "Copy"}
                </Button>
              </div>
            )}

            {!expired && !loading && (
              <p className="text-xs text-[var(--color-muted)]">Expires in {countdown}</p>
            )}

            {!expired && !loading && (
              <div className="space-y-2 text-center">
                <div className="flex items-center justify-center gap-2 text-sm text-[var(--color-muted)]">
                  <svg className="h-4 w-4 animate-spin" fill="none" viewBox="0 0 24 24">
                    <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                    <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                  </svg>
                  Waiting for your server to connect…
                </div>
                <p className="text-xs text-[var(--color-muted)]">
                  This page updates automatically — no refresh needed.
                </p>
              </div>
            )}

            <div className="flex flex-col items-center gap-3 border-t border-[var(--color-border)] pt-4 border-[var(--color-border)]">
              <Button variant="secondary" onClick={skipForLater}>
                I&apos;ll do this later — browse the dashboard
              </Button>
              <p className="text-xs text-[var(--color-muted)]">
                You can connect a server anytime from Servers → Add Server.
              </p>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}
