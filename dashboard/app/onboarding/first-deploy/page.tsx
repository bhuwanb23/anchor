"use client";

import { useState, useEffect, useCallback, useRef } from "react";
import { useRouter } from "next/navigation";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import { getWSClient, type WSMessage } from "@/lib/ws";
import api from "@/lib/api";

// ---------------------------------------------------------------------------
// Templates
// ---------------------------------------------------------------------------

const templates = [
  {
    name: "Next.js + Postgres",
    description: "Full-stack React with a managed database",
    image: "node:18-alpine",
    port: 3000,
    appName: "my-nextjs-app",
  },
  {
    name: "WordPress",
    description: "CMS with MySQL database",
    image: "wordpress:latest",
    port: 80,
    appName: "my-wordpress",
  },
  {
    name: "Django + Postgres",
    description: "Python web framework with a managed database",
    image: "python:3.12-slim",
    port: 8000,
    appName: "my-django-app",
  },
];

// ---------------------------------------------------------------------------
// Deploy phases (mapped from command_progress messages)
// ---------------------------------------------------------------------------

const deploySteps = [
  { key: "pull", label: "Pulling image" },
  { key: "start", label: "Starting your app" },
  { key: "https", label: "Configuring HTTPS" },
];

type Phase = "form" | "sending" | "progress" | "success" | "failure";

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export default function FirstDeployPage() {
  const router = useRouter();

  // Form state
  const [selectedTemplate, setSelectedTemplate] = useState<number | null>(null);
  const [image, setImage] = useState("");
  const [appName, setAppName] = useState("");
  const [port, setPort] = useState(3000);
  const [servers, setServers] = useState<{ id: string; name: string }[]>([]);
  const [selectedServer, setSelectedServer] = useState("");

  // Deploy state
  const [phase, setPhase] = useState<Phase>("form");
  const [currentStep, setCurrentStep] = useState(0);
  const [deployUrl, setDeployUrl] = useState("");
  const [deployError, setDeployError] = useState("");
  const [logs, setLogs] = useState<string[]>([]);
  const [sending, setSending] = useState(false);

  // Refs
  const unsubRef = useRef<(() => void)[]>([]);

  // Fetch servers
  useEffect(() => {
    api.get("/api/v1/servers").then((res) => {
      const list = res.data.map((s: { id: string; name: string }) => ({ id: s.id, name: s.name }));
      setServers(list);
      if (list.length > 0) setSelectedServer(list[0].id);
    });
  }, []);

  // Template selection fills in form
  const selectTemplate = (idx: number) => {
    const t = templates[idx];
    setSelectedTemplate(idx);
    setImage(t.image);
    setAppName(t.appName);
    setPort(t.port);
  };

  // Deploy handler
  const handleDeploy = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedServer || !image || !appName) return;
    setSending(true);
    setPhase("sending");

    try {
      // Create app
      const appRes = await api.post(`/api/v1/servers/${selectedServer}/apps`, {
        project_name: appName,
      });
      const appId = appRes.data.id;

      // Deploy
      const deployRes = await api.post(
        `/api/v1/servers/${selectedServer}/apps/${appId}/deploy`,
        { image, port }
      );
      const commandId = deployRes.data.command_id;

      // Listen for progress
      setPhase("progress");
      setupProgressListener(selectedServer, commandId, appId);
    } catch {
      setPhase("failure");
      setDeployError("Failed to start deployment. Please try again.");
      setSending(false);
    }
  };

  // WebSocket progress listener
  const setupProgressListener = useCallback(
    (serverId: string, commandId: string, appId: string) => {
      const client = getWSClient();

      // Track which steps are done
      const stepStates: Record<string, "pending" | "active" | "done"> = {
        pull: "pending",
        start: "pending",
        https: "pending",
      };

      const unsub = client.on("command_progress", (msg: WSMessage) => {
        const payload = msg.payload as {
          command_id?: string;
          status?: string;
          message?: string;
        } | undefined;

        if (payload?.command_id !== commandId) return;

        // Map progress to steps
        const msgLower = (payload.message || "").toLowerCase();
        if (msgLower.includes("pull") || msgLower.includes("image")) {
          stepStates.pull = payload.status === "success" ? "done" : "active";
        } else if (msgLower.includes("start") || msgLower.includes("container")) {
          stepStates.start = payload.status === "success" ? "done" : "active";
          if (stepStates.pull !== "done") stepStates.pull = "done";
        } else if (msgLower.includes("https") || msgLower.includes("domain") || msgLower.includes("caddy")) {
          stepStates.https = payload.status === "success" ? "done" : "active";
          if (stepStates.pull !== "done") stepStates.pull = "done";
          if (stepStates.start !== "done") stepStates.start = "done";
        }

        // Calculate current step index
        const keys = ["pull", "start", "https"];
        let idx = 0;
        for (let i = 0; i < keys.length; i++) {
          if (stepStates[keys[i]] === "done") idx = i + 1;
        }
        setCurrentStep(Math.min(idx, deploySteps.length - 1));
      });
      unsubRef.current.push(unsub);

      // Listen for result
      const unsub2 = client.on("command_result", (msg: WSMessage) => {
        const payload = msg.payload as {
          command_id?: string;
          status?: string;
          result?: string;
        } | undefined;

        if (payload?.command_id !== commandId) return;

        if (payload.status === "success") {
          setCurrentStep(deploySteps.length);
          // Fetch app to get domain
          api
            .get(`/api/v1/servers/${serverId}/apps/${appId}`)
            .then((res) => {
              const domain = res.data.platform_domain || res.data.custom_domains;
              setDeployUrl(domain ? `https://${domain}` : "");
            })
            .catch(() => {});
          setPhase("success");
        } else {
          setDeployError(payload.result || "Your app did not start correctly. Your server is fine.");
          // Fetch recent logs
          api
            .get(`/api/v1/servers/${serverId}/apps/${appId}`)
            .then(() => {
              // Logs would come from the agent; show error message
              setLogs([payload.result || "Deploy failed"]);
            })
            .catch(() => {});
          setPhase("failure");
        }
      });
      unsubRef.current.push(unsub2);

      client.connect();
    },
    []
  );

  // Cleanup
  useEffect(() => {
    return () => {
      unsubRef.current.forEach((fn) => fn());
      unsubRef.current = [];
    };
  }, []);

  // ---------------------------------------------------------------------------
  // Render: Success
  // ---------------------------------------------------------------------------

  if (phase === "success") {
    return (
      <Card>
        <CardContent className="space-y-6 py-8 text-center">
          <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/30">
            <svg className="h-8 w-8 text-green-600" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" d="M4.5 12.75l6 6 9-13.5" />
            </svg>
          </div>
          <div>
            <h2 className="text-2xl font-bold text-gray-900 dark:text-white">Your app is live!</h2>
            <p className="mt-2 text-gray-500">{appName}</p>
            {deployUrl && (
              <a
                href={deployUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="mt-2 inline-block text-blue-600 hover:underline"
              >
                {deployUrl} →
              </a>
            )}
          </div>
          <div className="mx-auto max-w-xs space-y-2 text-left text-sm text-gray-600 dark:text-gray-400">
            <div className="flex items-center gap-2">
              <svg className="h-4 w-4 text-green-500" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" d="M4.5 12.75l6 6 9-13.5" />
              </svg>
              Daily backups enabled
            </div>
            <div className="flex items-center gap-2">
              <svg className="h-4 w-4 text-green-500" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" d="M4.5 12.75l6 6 9-13.5" />
              </svg>
              HTTPS automatic
            </div>
            <div className="flex items-center gap-2">
              <svg className="h-4 w-4 text-green-500" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" d="M4.5 12.75l6 6 9-13.5" />
              </svg>
              Auto-restart enabled
            </div>
          </div>
          <Button onClick={() => router.push("/overview")} className="mt-4">
            Go to Dashboard →
          </Button>
        </CardContent>
      </Card>
    );
  }

  // ---------------------------------------------------------------------------
  // Render: Failure
  // ---------------------------------------------------------------------------

  if (phase === "failure") {
    return (
      <Card>
        <CardContent className="space-y-6 py-8">
          <div className="text-center">
            <h2 className="text-2xl font-bold text-gray-900 dark:text-white">Deploy failed</h2>
            <p className="mt-2 text-sm text-gray-500">
              Your app did not start correctly. Your server is fine.
            </p>
          </div>

          {logs.length > 0 && (
            <pre className="max-h-48 overflow-y-auto rounded-lg bg-gray-900 p-4 font-mono text-xs text-red-400">
              {logs.join("\n")}
            </pre>
          )}

          <div className="space-y-2 text-sm text-gray-600 dark:text-gray-400">
            <p className="font-medium">Common causes:</p>
            <ul className="list-disc space-y-1 pl-5">
              <li>Missing or incorrect environment variable</li>
              <li>App trying to connect to a database that is not ready</li>
              <li>Port mismatch (app listening on wrong port)</li>
            </ul>
          </div>

          <div className="flex justify-center gap-3">
            <Button
              onClick={() => {
                setPhase("form");
                setDeployError("");
                setLogs([]);
                setCurrentStep(0);
              }}
            >
              Try Again
            </Button>
            <Button variant="ghost" onClick={() => router.push("/overview")}>
              View Dashboard
            </Button>
          </div>
        </CardContent>
      </Card>
    );
  }

  // ---------------------------------------------------------------------------
  // Render: Sending / Progress
  // ---------------------------------------------------------------------------

  if (phase === "sending" || phase === "progress") {
    return (
      <Card>
        <CardContent className="space-y-6 py-8">
          <h2 className="text-xl font-semibold text-gray-900 dark:text-white">
            Deploying {appName}...
          </h2>

          <div className="space-y-3">
            {deploySteps.map((step, i) => {
              let state: "done" | "active" | "pending" = "pending";
              if (i < currentStep) state = "done";
              else if (i === currentStep) state = phase === "sending" ? "pending" : "active";

              return (
                <div key={step.key} className="flex items-center gap-3">
                  {state === "done" ? (
                    <svg className="h-5 w-5 text-green-500" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" d="M4.5 12.75l6 6 9-13.5" />
                    </svg>
                  ) : state === "active" ? (
                    <svg className="h-5 w-5 animate-spin text-blue-500" fill="none" viewBox="0 0 24 24">
                      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
                      <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
                    </svg>
                  ) : (
                    <div className="h-5 w-5 rounded-full border-2 border-gray-300 dark:border-gray-600" />
                  )}
                  <span
                    className={`text-sm ${
                      state === "done"
                        ? "text-gray-500 line-through"
                        : state === "active"
                        ? "font-medium text-gray-900 dark:text-white"
                        : "text-gray-400"
                    }`}
                  >
                    {step.label}
                  </span>
                </div>
              );
            })}
          </div>

          {/* Progress bar */}
          <div className="h-2 overflow-hidden rounded-full bg-gray-200 dark:bg-gray-700">
            <div
              className="h-full rounded-full bg-blue-600 transition-all duration-500"
              style={{ width: `${(currentStep / deploySteps.length) * 100}%` }}
            />
          </div>

          <p className="text-xs text-gray-400">This usually takes 30-90 seconds</p>
        </CardContent>
      </Card>
    );
  }

  // ---------------------------------------------------------------------------
  // Render: Form
  // ---------------------------------------------------------------------------

  return (
    <Card>
      <CardContent className="space-y-6 py-8">
        {/* Template picker */}
        <div>
          <h2 className="text-xl font-semibold text-gray-900 dark:text-white">
            Deploy your first app
          </h2>
          <p className="mt-1 text-sm text-gray-500">Pick a template or use your own Docker image.</p>
        </div>

        <div className="grid gap-3 sm:grid-cols-3">
          {templates.map((t, i) => (
            <button
              key={t.name}
              type="button"
              className={`rounded-lg border p-4 text-left transition ${
                selectedTemplate === i
                  ? "border-blue-500 bg-blue-50 dark:border-blue-400 dark:bg-blue-950/30"
                  : "border-gray-200 hover:border-gray-300 dark:border-gray-700 dark:hover:border-gray-600"
              }`}
              onClick={() => selectTemplate(i)}
            >
              <div className="font-medium text-gray-900 dark:text-white">{t.name}</div>
              <div className="mt-1 text-xs text-gray-500">{t.description}</div>
            </button>
          ))}
        </div>

        {/* Divider */}
        <div className="relative">
          <div className="absolute inset-0 flex items-center">
            <div className="w-full border-t border-gray-200 dark:border-gray-700" />
          </div>
          <div className="relative flex justify-center text-xs">
            <span className="bg-white px-2 text-gray-400 dark:bg-gray-900">or use your own Docker image</span>
          </div>
        </div>

        {/* Custom form */}
        <form onSubmit={handleDeploy} className="space-y-4">
          {servers.length === 0 ? (
            <p className="text-sm text-amber-600">
              No servers connected yet. Go back and connect a server first.
            </p>
          ) : null}

          <Input
            label="Docker image"
            value={image}
            onChange={(e) => setImage(e.target.value)}
            placeholder="nginx:latest"
          />
          <Input
            label="App name"
            value={appName}
            onChange={(e) => setAppName(e.target.value)}
            placeholder="my-app"
          />
          <Input
            label="Port"
            type="number"
            value={port}
            onChange={(e) => setPort(Number(e.target.value))}
          />

          <Button
            type="submit"
            className="w-full"
            disabled={sending || !image || !appName || !selectedServer}
          >
            {sending ? "Sending..." : "Deploy →"}
          </Button>
        </form>
      </CardContent>
    </Card>
  );
}
