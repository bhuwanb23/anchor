"use client";

import { useEffect, useMemo, useState } from "react";
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

export default function InferPage() {
  const router = useRouter();
  const { servers, isLoading: serversLoading } = useServers(15_000);
  const selectServer = useServerStore((s) => s.selectServer);
  const [serverId, setServerId] = useState<string | null>(null);

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

  const hardwareLabel =
    platform && (platform.cpu_microarchitecture || platform.cpu_cloud_provider_hint)
      ? [platform.cpu_microarchitecture, platform.cpu_cloud_provider_hint]
          .filter(Boolean)
          .join(" · ")
      : platform?.cpu_model_name || "this server";

  const benchmark = deploy.result?.benchmark_comparison;

  // Progressive visibility:
  //   Section 1 — always visible
  //   Section 2 — appears when deploy starts
  //   Section 3 — appears when deploy succeeds
  //   Section 4 — appears when the benchmark comparison is present
  const showProgress = deploy.phase === "deploying" || deploy.phase === "failed";
  const showEndpoint = deploy.phase === "succeeded" && !!deploy.result;
  const showBenchmark = showEndpoint && !!benchmark;

  return (
    <FadeIn>
      <div className="mx-auto max-w-3xl space-y-6">
        {/* Section 1 — Model Selection (always visible) */}
        <ModelSelection
          serverId={serverId}
          serverName={selectedServer?.name || null}
          serverConnected={selectedServer?.status === "connected"}
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

        {/* Section 2 — Deploy Progress (appears when deploy starts) */}
        {showProgress && selectedTemplate && (
          <DeployProgress
            modelName={`${selectedTemplate.name}${modelLabel ? ` (${modelLabel})` : ""}`}
            state={deploy}
          />
        )}

        {/* Section 3 — Live Endpoint (appears when deploy succeeds) */}
        {showEndpoint && deploy.result && (
          <LiveEndpoint result={deploy.result} />
        )}

        {/* Section 4 — Benchmark Card (appears when benchmark completes) */}
        {showBenchmark && benchmark && (
          <BenchmarkCard
            comparison={benchmark}
            hardwareLabel={hardwareLabel}
            modelLabel={modelLabel || deploy.result?.model_file || "LLM"}
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
                onChange={(e) => setServerId(e.target.value || null)}
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
