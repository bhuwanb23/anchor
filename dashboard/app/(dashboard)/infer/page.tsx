"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { ChevronDown } from "lucide-react";
import { useServers } from "@/hooks/use-server";
import { useServerStore } from "@/store/server-store";
import {
  useInferTemplates,
  usePlatformInfo,
  useInferDeploy,
} from "@/hooks/use-infer";
import { ModelSelection } from "@/components/infer/model-selection";
import { DeployProgress } from "@/components/infer/deploy-progress";
import { LiveEndpoint } from "@/components/infer/live-endpoint";
import { BenchmarkCard } from "@/components/infer/benchmark-card";
import { Button } from "@/components/ui/button";
import { FadeIn } from "@/components/ui/page-states";
import api from "@/lib/api";
import type { BenchmarkComparison, InferenceDeployResult } from "@/types";

export default function InferPage() {
  const router = useRouter();
  const { servers, isLoading: serversLoading } = useServers(15_000);
  const selectServer = useServerStore((s) => s.selectServer);
  const [serverId, setServerId] = useState<string | null>(null);
  const restoredRef = useRef(false);

  // Default to the first connected server (or first server) once loaded.
  useEffect(() => {
    if (!serverId && servers.length > 0) {
      const connected = servers.find((s) => s.status === "connected");
      setServerId(connected?.id || servers[0].id);
    }
  }, [serverId, servers]);

  // Keep the shared server store in sync so the shell's live status applies.
  useEffect(() => {
    if (serverId) selectServer(serverId);
  }, [serverId, selectServer]);

  const selectedServer = useMemo(
    () => servers.find((s) => s.id === serverId) || null,
    [servers, serverId]
  );

  const { templates, isLoading: templatesLoading, error: templatesError } =
    useInferTemplates();
  const { platform, error: platformError } = usePlatformInfo(serverId);

  const [selectedTemplateId, setSelectedTemplateId] = useState<string | null>(null);
  const deploy = useInferDeploy(serverId);

  // On page load, restore pre-computed results so a demo shows Sections 3 + 4
  // immediately (the benchmark runs ahead of the presentation).
  useEffect(() => {
    if (!serverId || restoredRef.current) return;
    restoredRef.current = true;
    let cancelled = false;

    (async () => {
      try {
        const statusRes = await api.get<{
          deployed: boolean;
          details?: InferenceDeployResult;
        }>(`/api/v1/servers/${serverId}/infer/status`);
        if (!cancelled && statusRes.data?.deployed && statusRes.data.details) {
          const details = statusRes.data.details;
          // Fold in the persisted benchmark comparison if available.
          try {
            const benchRes = await api.get<BenchmarkComparison>(
              `/api/v1/servers/${serverId}/infer/benchmark`
            );
            if (!cancelled && benchRes.data?.optimized) {
              details.benchmark_comparison = benchRes.data;
              details.benchmarked_at = benchRes.data.benchmarked_at || details.benchmarked_at;
            }
          } catch {
            // No stored benchmark yet — keep whatever the deploy returned.
          }
          if (!cancelled) {
            deploy.restore(details);
            setSelectedTemplateId(details.template_id || null);
          }
        }
      } catch {
        // No prior deploy — Sections 2–4 stay hidden.
      }
    })();

    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [serverId]);

  // Reconnect banner state: shown while a deploy/benchmark is running and the
  // WebSocket drops. On reconnect the hook re-fetches status from the API.
  const connectionLost =
    deploy.wsStatus !== "connected" &&
    (deploy.phase === "deploying" || deploy.phase === "benchmarking");

  const selectedTemplate = useMemo(
    () => templates.find((t) => t.id === selectedTemplateId) || null,
    [templates, selectedTemplateId]
  );

  const modelLabel = selectedTemplate
    ? `${selectedTemplate.model.family} ${selectedTemplate.model.size} ${selectedTemplate.model.variant || ""}`.trim()
    : "";

  const handleDeploy = async (templateId: string) => {
    const ok = await deploy.deploy(templateId);
    if (ok && window !== undefined) {
      window.scrollTo({ top: 0, behavior: "smooth" });
    }
  };

  const handleRunBenchmark = async () => {
    const templateId = deploy.result?.template_id || selectedTemplateId;
    if (!templateId) return;
    await deploy.runBenchmark(templateId);
    if (window !== undefined) {
      window.scrollTo({ top: 0, behavior: "smooth" });
    }
  };

  const hardwareLabel =
    platform && (platform.cpu_microarchitecture || platform.cpu_cloud_provider_hint)
      ? [platform.cpu_microarchitecture, platform.cpu_cloud_provider_hint]
          .filter(Boolean)
          .join(" · ")
      : platform?.cpu_model_name || "this server";

  const benchmark = deploy.result?.benchmark_comparison;

  // Progressive visibility:
  //   Section 1 — always visible
  //   Section 2 — appears when deploy/benchmark is running or failed
  //   Section 3 — appears when deploy succeeds
  //   Section 4 — appears when the benchmark comparison is present
  const showProgress =
    deploy.phase === "deploying" || deploy.phase === "benchmarking" || deploy.phase === "failed";
  const showEndpoint = deploy.phase === "succeeded" && !!deploy.result;
  const showBenchmark = showEndpoint && !!benchmark;

  return (
    <FadeIn>
      <div className="mx-auto max-w-3xl space-y-6">
        {/* Reconnect banner — connection dropped mid-deploy */}
        {connectionLost && (
          <div className="flex items-start gap-3 rounded-[var(--radius-md)] border border-[var(--color-warning)]/40 bg-[var(--color-warning-soft)] px-4 py-3">
            <span className="mt-1.5 h-2 w-2 shrink-0 animate-pulse rounded-full bg-[var(--color-warning)]" />
            <div>
              <p className="text-sm font-semibold text-[var(--color-ink)]">
                Connection to your server was lost. The deploy is still running.
              </p>
              <p className="mt-0.5 text-sm text-[var(--color-muted)]">Reconnecting…</p>
            </div>
          </div>
        )}

        {/* Section 1 — Model Selection (always visible) */}
        <ModelSelection
          serverId={serverId}
          serverName={selectedServer?.name || null}
          serverConnected={selectedServer?.status === "connected"}
          wsStatus={deploy.wsStatus}
          serversCount={servers.length}
          templates={templates}
          templatesLoading={templatesLoading || serversLoading}
          templatesError={templatesError}
          platform={platform}
          platformLoading={!platform && !platformError}
          platformError={platformError}
          selectedTemplateId={selectedTemplateId}
          deploying={deploy.phase === "deploying"}
          onSelectTemplate={setSelectedTemplateId}
          onDeploy={handleDeploy}
          onPickServer={() => {
            if (servers.length === 0) {
              router.push("/onboarding/connect-server");
              return;
            }
            document
              .getElementById("infer-server-selector")
              ?.scrollIntoView({ behavior: "smooth", block: "center" });
          }}
        />

        {/* Section 2 — Deploy / Benchmark Progress */}
        {showProgress && (
          <DeployProgress
            modelName={
              selectedTemplate?.name ||
              deploy.result?.template_id ||
              "model"
            }
            state={deploy}
            onRetry={() => {
              void handleDeploy(deploy.result?.template_id || selectedTemplateId || "");
            }}
            onChangeConfig={() => {
              deploy.reset();
              setSelectedTemplateId(null);
              if (window !== undefined) {
                window.scrollTo({ top: 0, behavior: "smooth" });
              }
            }}
          />
        )}

        {/* Section 3 — Live Endpoint (appears when deploy succeeds) */}
        {showEndpoint && deploy.result && (
          <LiveEndpoint
            result={deploy.result}
            modelLabel={modelLabel || deploy.result.model_file?.replace(/\.gguf$/, "") || ""}
            deployedAt={deploy.result.benchmarked_at}
          />
        )}

        {/* Section 4 — Benchmark Card (appears when benchmark completes) */}
        {showBenchmark && benchmark && (
          <BenchmarkCard
            comparison={benchmark}
            hardwareLabel={hardwareLabel}
            modelLabel={modelLabel || deploy.result?.model_file || "LLM"}
            running={deploy.phase === "benchmarking"}
            onRunAgain={handleRunBenchmark}
          />
        )}

        {/* Server switch when multiple servers are connected */}
        {servers.length > 1 && (
          <div
            id="infer-server-selector"
            className="flex items-center justify-between border-t border-[var(--color-border)] pt-4"
          >
            <label className="text-sm font-medium text-[var(--color-muted)]">
              Deploy target server
            </label>
            <div className="relative">
              <select
                value={serverId || ""}
                onChange={(e) => {
                  setServerId(e.target.value || null);
                  restoredRef.current = false;
                  deploy.reset();
                }}
                className="appearance-none rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] py-2 pl-3.5 pr-9 text-sm font-medium text-[var(--color-ink)] shadow-sm transition-colors focus:border-[var(--color-accent)] focus:outline-none focus:ring-2 focus:ring-[var(--color-accent-soft)]"
              >
                {servers.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                    {s.status === "connected" ? " · connected" : " · offline"}
                  </option>
                ))}
              </select>
              <ChevronDown className="pointer-events-none absolute right-3 top-1/2 h-4 w-4 -translate-y-1/2 text-[var(--color-muted)]" />
            </div>
          </div>
        )}

        {serversLoading && servers.length === 0 && (
          <p className="text-center text-sm text-[var(--color-muted)]">
            Loading your servers…
          </p>
        )}

        {!serversLoading && servers.length === 0 && (
          <div className="text-center">
            <Button onClick={() => router.push("/onboarding/connect-server")}>
              Connect a server first
            </Button>
          </div>
        )}
      </div>
    </FadeIn>
  );
}
