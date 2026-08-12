"use client";

import { ArrowRight, Cpu, Loader2, Server as ServerIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import type { InferenceTemplate, PlatformInfo } from "@/types";
import type { WsConnectionStatus } from "@/hooks/use-infer";

// Connection dot shown in the page header (green/yellow/grey).
function ConnectionDot({ status }: { status: WsConnectionStatus }) {
  const color =
    status === "connected"
      ? "bg-[var(--color-success)]"
      : status === "connecting"
        ? "animate-pulse bg-[var(--color-warning)]"
        : "bg-[var(--color-muted)]";
  const label =
    status === "connected"
      ? "Connected to control plane"
      : status === "connecting"
        ? "Reconnecting…"
        : "Disconnected";
  return (
    <span
      className="ml-1.5 inline-flex items-center gap-1.5 text-xs font-semibold text-[var(--color-muted)]"
      title={label}
    >
      <span className={`h-2 w-2 rounded-full ${color}`} />
      {status === "connected" ? "Live" : status === "connecting" ? "Reconnecting" : "Offline"}
    </span>
  );
}

// ---------------------------------------------------------------------------
// Helpers — human-readable readiness strings
// ---------------------------------------------------------------------------

function formatOptimization(platform: PlatformInfo | null): string {
  if (!platform) return "Waiting for server…";
  if (!platform.is_arm64) return "No Arm optimization (x86 build)";
  if (platform.optimization_label) {
    // "Full (SVE + I8MM)" → "Full Arm Optimization (SVE + I8MM)"
    return platform.optimization_label.replace(/^(\w+)\s*\(/, "$1 Arm Optimization (");
  }
  return "Arm optimization detected";
}

function formatHardware(platform: PlatformInfo | null): string {
  if (!platform) return "Waiting for server…";
  const parts: string[] = [];
  if (platform.cpu_microarchitecture) parts.push(platform.cpu_microarchitecture);
  if (platform.cpu_cloud_provider_hint) parts.push(platform.cpu_cloud_provider_hint);
  if (parts.length === 0) return platform.cpu_model_name || "Unknown hardware";
  return parts.join(" · ");
}

interface ModelSelectionProps {
  serverId: string | null;
  serverName: string | null;
  serverConnected: boolean;
  wsStatus: WsConnectionStatus;
  serversCount: number;
  templates: InferenceTemplate[];
  templatesLoading: boolean;
  templatesError: string | null;
  platform: PlatformInfo | null;
  platformLoading: boolean;
  platformError: string | null;
  selectedTemplateId: string | null;
  deploying: boolean;
  onSelectTemplate: (id: string) => void;
  onDeploy: (templateId: string) => void;
  onPickServer: () => void;
}

export function ModelSelection({
  serverId,
  serverName,
  serverConnected,
  wsStatus,
  serversCount,
  templates,
  templatesLoading,
  templatesError,
  platform,
  platformLoading,
  platformError,
  selectedTemplateId,
  deploying,
  onSelectTemplate,
  onDeploy,
  onPickServer,
}: ModelSelectionProps) {
  const optimization = formatOptimization(platform);
  const hardware = formatHardware(platform);

  return (
    <section className="space-y-6">
      {/* Header */}
      <div>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <h1 className="text-3xl font-extrabold tracking-tight text-[var(--color-ink)]">
            Anchor Infer
          </h1>
          <ConnectionDot status={wsStatus} />
        </div>
        <p className="mt-1 text-[var(--color-muted)]">
          Deploy AI models on Arm hardware, automatically optimized
        </p>
      </div>

      {/* Server status — shown when a server is connected */}
      {serverId ? (
        <Card>
          <CardContent className="flex flex-wrap items-center justify-between gap-4">
            <div className="flex min-w-0 items-center gap-3">
              <div
                className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-[var(--radius-md)] ${
                  serverConnected
                    ? "bg-[var(--color-success-soft)] text-[var(--color-success)]"
                    : "bg-[var(--color-paper-2)] text-[var(--color-muted)]"
                }`}
              >
                <ServerIcon className="h-5 w-5" />
              </div>
              <div className="min-w-0">
                <div className="flex items-center gap-2">
                  <span
                    className={`h-2 w-2 shrink-0 rounded-full ${
                      serverConnected
                        ? "bg-[var(--color-success)]"
                        : "bg-[var(--color-muted)]"
                    }`}
                  />
                  <p className="truncate font-semibold text-[var(--color-ink)]">
                    {serverName || "Your server"}
                  </p>
                  <Badge variant={serverConnected ? "success" : "default"}>
                    {serverConnected ? "Connected" : "Disconnected"}
                  </Badge>
                </div>
                {platformLoading ? (
                  <Skeleton className="mt-1.5 h-4 w-56" />
                ) : (
                  <p className="mt-1 truncate text-sm text-[var(--color-muted)]">
                    {optimization}
                  </p>
                )}
                {!platformLoading && (
                  <p className="truncate text-xs text-[var(--color-muted)]">
                    {hardware}
                  </p>
                )}
              </div>
            </div>

            {serversCount > 1 && (
              <Button variant="secondary" size="sm" onClick={onPickServer}>
                Switch server
              </Button>
            )}
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardContent className="space-y-3 py-8 text-center">
            <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-full bg-[var(--color-accent-soft)] text-[var(--color-accent)]">
              <ServerIcon className="h-6 w-6" />
            </div>
            <p className="font-semibold text-[var(--color-ink)]">
              No server connected
            </p>
            <p className="mx-auto max-w-sm text-sm text-[var(--color-muted)]">
              Connect a server to deploy AI models with Anchor Infer.
            </p>
            <Button onClick={onPickServer}>Connect a server</Button>
          </CardContent>
        </Card>
      )}

      {/* Model template cards */}
      <div>
        <h2 className="mb-3 text-lg font-bold tracking-tight text-[var(--color-ink)]">
          Choose a model
        </h2>

        {templatesLoading && templates.length === 0 ? (
          <div className="grid gap-4 sm:grid-cols-2">
            <Skeleton className="h-44 rounded-[var(--radius-lg)]" />
            <Skeleton className="h-44 rounded-[var(--radius-lg)]" />
          </div>
        ) : templatesError && templates.length === 0 ? (
          <p className="text-sm text-[var(--color-muted)]">
            Could not load model templates. Try again in a moment.
          </p>
        ) : templates.length === 0 ? (
          <p className="text-sm text-[var(--color-muted)]">
            No model templates available yet.
          </p>
        ) : (
          <div className="grid gap-4 sm:grid-cols-2">
            {templates.map((t) => {
              const selected = selectedTemplateId === t.id;
              const modelLabel = `${t.model.family} ${t.model.size} ${t.model.variant || ""}`.trim();
              return (
                <button
                  key={t.id}
                  type="button"
                  onClick={() => onSelectTemplate(t.id)}
                  className={`group relative rounded-[var(--radius-lg)] border bg-[var(--color-surface)] p-5 text-left shadow-[var(--shadow-card)] transition-all duration-[var(--dur-med)] ease-[var(--ease-out)] focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-focus)] ${
                    selected
                      ? "border-[var(--color-accent)] ring-2 ring-[var(--color-accent-soft)]"
                      : "border-[var(--color-border)] hover:border-[var(--color-accent-2)] hover:shadow-[var(--shadow-lift)]"
                  }`}
                >
                  {selected && (
                    <span className="absolute right-4 top-4 flex h-6 w-6 items-center justify-center rounded-full bg-[var(--color-accent)] text-xs font-bold text-[var(--color-accent-fg)]">
                      ✓
                    </span>
                  )}

                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <h3 className="text-xl font-extrabold tracking-tight text-[var(--color-ink)]">
                        {t.name}
                      </h3>
                      <p className="mt-0.5 text-sm font-medium text-[var(--color-accent)]">
                        {modelLabel}
                      </p>
                    </div>
                    <Cpu className="mt-1 h-5 w-5 shrink-0 text-[var(--color-muted)]" />
                  </div>

                  <p className="mt-2 text-sm text-[var(--color-muted)]">
                    {t.description}
                  </p>

                  <div className="mt-4 space-y-1.5 text-sm">
                    <p className="text-[var(--color-ink)]">
                      <span className="font-semibold">You get: </span>
                      an OpenAI-compatible{" "}
                      <code className="rounded bg-[var(--color-paper-2)] px-1 py-0.5 font-mono text-xs">
                        {t.runtime.api_path}
                      </code>{" "}
                      endpoint
                    </p>
                    <p className="text-[var(--color-ink)]">
                      <span className="font-semibold">Requires: </span>
                      {t.resources.min_ram_gb}GB RAM, {t.resources.min_disk_gb}GB disk
                    </p>
                    <p className="text-[var(--color-ink)]">
                      <span className="font-semibold">Optimization: </span>
                      {platform && platform.is_arm64
                        ? `Will use ${platform.optimization_label || "Arm"} acceleration`
                        : "Works on any server — Arm recommended"}
                    </p>
                  </div>
                </button>
              );
            })}
          </div>
        )}
      </div>

      {/* Deploy button */}
      <div className="flex flex-wrap items-center justify-between gap-3 border-t border-[var(--color-border)] pt-5">
        <p className="text-sm text-[var(--color-muted)]">
          {selectedTemplateId
            ? "Ready to deploy. The agent picks the optimized build for your hardware."
            : "Select a model to enable deployment."}
        </p>
        <Button
          size="lg"
          disabled={!selectedTemplateId || deploying || !serverId}
          onClick={() => selectedTemplateId && onDeploy(selectedTemplateId)}
          className="gap-2"
        >
          {deploying ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <ArrowRight className="h-4 w-4" />
          )}
          {deploying ? "Deploying…" : "Deploy to Arm Server →"}
        </Button>
      </div>

      {platformError && (
        <p className="text-xs text-[var(--color-muted)]">{platformError}</p>
      )}
    </section>
  );
}
