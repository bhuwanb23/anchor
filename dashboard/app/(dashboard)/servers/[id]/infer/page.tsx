"use client";

import { use, useCallback, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import {
  Check,
  Circle,
  Copy,
  Cpu,
  Eye,
  EyeOff,
  Loader2,
  Sparkles,
  XCircle,
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { FadeIn, PageError, ServerOverviewSkeleton } from "@/components/ui/page-states";
import { useServer } from "@/hooks/use-server";
import { getWSClient, type WSMessage } from "@/lib/ws";
import {
  deployInference,
  detectServerPlatform,
  fetchInferBenchmarks,
  fetchInferTemplates,
  fetchInferenceStatus,
  fetchServerPlatform,
} from "@/lib/infer";
import type {
  BenchmarkResult,
  InferenceStatus,
  InferenceTemplate,
  PlatformInfo,
} from "@/types";

const DEPLOY_STEPS = [
  { key: "checking", label: "Checking server capabilities" },
  { key: "pulling", label: "Pulling runtime images" },
  { key: "preparing", label: "Preparing model storage" },
  { key: "downloading", label: "Downloading model weights" },
  { key: "benchmark", label: "Benchmarking generic / optimized builds" },
  { key: "starting", label: "Starting optimized inference server" },
  { key: "routing", label: "Configuring HTTPS endpoint" },
  { key: "testing", label: "Testing endpoint" },
  { key: "complete", label: "Finishing up" },
];

function stepIndex(phase: string, message: string): number {
  const hay = `${phase} ${message}`.toLowerCase();
  for (let i = 0; i < DEPLOY_STEPS.length; i++) {
    if (hay.includes(DEPLOY_STEPS[i].key)) return i;
  }
  if (hay.includes("download")) return 3;
  if (hay.includes("bench")) return 4;
  if (hay.includes("health") || hay.includes("load")) return 5;
  if (hay.includes("https") || hay.includes("caddy") || hay.includes("rout")) return 6;
  if (hay.includes("test")) return 7;
  if (hay.includes("complete") || hay.includes("success")) return 8;
  return 0;
}

function copyText(label: string, value: string) {
  void navigator.clipboard.writeText(value);
  toast.success(`${label} copied`);
}

export default function InferPage({ params }: { params: Promise<{ id: string }> }) {
  const { id: serverId } = use(params);
  const { server, isLoading, error } = useServer(serverId);

  const [platform, setPlatform] = useState<PlatformInfo | null>(null);
  const [templates, setTemplates] = useState<InferenceTemplate[]>([]);
  const [selectedTemplate, setSelectedTemplate] = useState<string>("");
  const [status, setStatus] = useState<InferenceStatus | null>(null);
  const [phase, setPhase] = useState<"idle" | "progress" | "done" | "failed">("idle");
  const [percent, setPercent] = useState(0);
  const [activeStep, setActiveStep] = useState(0);
  const [completedSteps, setCompletedSteps] = useState<Set<number>>(new Set());
  const [logs, setLogs] = useState<string[]>([]);
  const [errorMsg, setErrorMsg] = useState("");
  const [revealKey, setRevealKey] = useState(false);
  const [endpointURL, setEndpointURL] = useState("");
  const [apiKey, setApiKey] = useState("");
  const [optimization, setOptimization] = useState("");
  const [benchmark, setBenchmark] = useState<{
    tpsImp?: number;
    ttftImp?: number;
    optimized?: BenchmarkResult;
    generic?: BenchmarkResult;
  } | null>(null);
  const [testPrompt, setTestPrompt] = useState("What is Anchor Infer in one sentence?");
  const [testReply, setTestReply] = useState("");
  const [testMs, setTestMs] = useState<number | null>(null);
  const [testing, setTesting] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [detecting, setDetecting] = useState(false);

  const commandId = useRef<string | null>(null);
  const detectCmdId = useRef<string | null>(null);
  const logEnd = useRef<HTMLDivElement>(null);

  const load = useCallback(async () => {
    const [plat, tpls, st, bench] = await Promise.all([
      fetchServerPlatform(serverId),
      fetchInferTemplates(),
      fetchInferenceStatus(serverId),
      fetchInferBenchmarks(serverId),
    ]);
    setPlatform(plat);
    setTemplates(tpls);
    if (tpls[0] && !selectedTemplate) setSelectedTemplate(tpls[0].id);
    setStatus(st);
    if (st.deployed && st.details) {
      setPhase("done");
      setEndpointURL((st.details as { endpoint_url?: string }).endpoint_url || "");
      setApiKey((st.details as { api_key?: string }).api_key || "");
      setOptimization((st.details as { optimization?: string }).optimization || "");
      const d = st.details as {
        endpoint_url?: string;
        api_key?: string;
        optimization?: string;
        benchmark_comparison?: {
          tokens_per_second_improvement_pct?: number;
          ttft_improvement_pct?: number;
          optimized?: BenchmarkResult;
          generic?: BenchmarkResult;
        };
      };
      if (d.endpoint_url) setEndpointURL(d.endpoint_url);
      if (d.api_key) setApiKey(d.api_key);
      if (d.optimization) setOptimization(d.optimization);
      if (d.benchmark_comparison) {
        setBenchmark({
          tpsImp: d.benchmark_comparison.tokens_per_second_improvement_pct,
          ttftImp: d.benchmark_comparison.ttft_improvement_pct,
          optimized: d.benchmark_comparison.optimized,
          generic: d.benchmark_comparison.generic,
        });
      }
    }
    if (bench.available) {
      setBenchmark({
        tpsImp: bench.tokens_per_second_improvement_pct,
        ttftImp: bench.ttft_improvement_pct,
        optimized: bench.optimized as BenchmarkResult | undefined,
        generic: bench.generic as BenchmarkResult | undefined,
      });
    }
  }, [serverId, selectedTemplate]);

  useEffect(() => {
    void load();
  }, [serverId]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    logEnd.current?.scrollIntoView({ behavior: "smooth" });
  }, [logs]);

  useEffect(() => {
    const client = getWSClient();
    client.subscribeServer(serverId);
    const onPlatform = () => {
      void fetchServerPlatform(serverId).then((plat) => {
        if (plat) setPlatform(plat);
      });
    };
    const unsub = client.on("platform_report", onPlatform);
    return () => unsub();
  }, [serverId]);

  useEffect(() => {
    if (phase !== "progress" || !commandId.current) return;
    const client = getWSClient();
    client.subscribeServer(serverId);

    const onProgress = (msg: WSMessage) => {
      const p = (msg.payload || {}) as {
        command_id?: string;
        phase?: string;
        message?: string;
        percent?: number;
      };
      if (p.command_id && commandId.current && p.command_id !== commandId.current) return;
      if (typeof p.percent === "number") setPercent(p.percent);
      const idx = stepIndex(p.phase || "", p.message || "");
      setActiveStep(idx);
      setCompletedSteps((prev) => {
        const next = new Set(prev);
        for (let i = 0; i < idx; i++) next.add(i);
        return next;
      });
      if (p.message) setLogs((L) => [...L.slice(-200), p.message!]);
    };

    const onResult = (msg: WSMessage) => {
      const p = (msg.payload || msg) as {
        command_id?: string;
        id?: string;
        status?: string;
        output?: string;
        error?: string;
        result?: string;
      };
      const cid = p.command_id || p.id;
      if (cid && commandId.current && cid !== commandId.current) return;
      const ok = p.status === "success" || p.status === "completed" || p.status === "ok";
      if (!ok) {
        setErrorMsg(p.error || p.output || p.result || "Deploy failed");
        setPhase("failed");
        return;
      }
      setPercent(100);
      setCompletedSteps(new Set(DEPLOY_STEPS.map((_, i) => i)));
      setPhase("done");
      try {
        const raw = p.output || p.result || "";
        const details = typeof raw === "string" && raw.startsWith("{") ? JSON.parse(raw) : null;
        if (details) {
          if (details.endpoint_url) setEndpointURL(details.endpoint_url);
          if (details.api_key) setApiKey(details.api_key);
          if (details.optimization) setOptimization(details.optimization);
          if (details.benchmark_comparison) {
            setBenchmark({
              tpsImp: details.benchmark_comparison.tokens_per_second_improvement_pct,
              ttftImp: details.benchmark_comparison.ttft_improvement_pct,
              optimized: details.benchmark_comparison.optimized,
              generic: details.benchmark_comparison.generic,
            });
          }
        }
      } catch {
        /* ignore parse */
      }
      void load();
      toast.success("Inference endpoint is live");
    };

    const u1 = client.on("command_progress", onProgress);
    const u2 = client.on("command_result", onResult);
    const u3 = client.on("result", onResult);
    return () => {
      u1();
      u2();
      u3();
    };
  }, [phase, serverId, load]);

  const startDetect = async () => {
    if (!server || server.status !== "connected") {
      toast.error("Server must be connected");
      return;
    }
    setDetecting(true);
    try {
      const res = await detectServerPlatform(serverId);
      detectCmdId.current = res.command_id;
      toast.message("Detecting hardware…");
      const client = getWSClient();
      client.subscribeServer(serverId);
      let unsubResult = () => {};
      let unsubAlt = () => {};
      const finish = (ok: boolean) => {
        setDetecting(false);
        detectCmdId.current = null;
        unsubResult();
        unsubAlt();
        void load();
        if (ok) toast.success("Platform readiness updated");
        else toast.error("Platform detection failed");
      };
      const onDone = (msg: WSMessage) => {
        const p = (msg.payload || msg) as { command_id?: string; id?: string; status?: string };
        const cid = p.command_id || p.id;
        if (cid && detectCmdId.current && cid !== detectCmdId.current) return;
        const ok = p.status === "success" || p.status === "completed" || p.status === "ok";
        finish(ok);
      };
      unsubResult = client.on("command_result", onDone);
      unsubAlt = client.on("result", onDone);
      window.setTimeout(() => {
        if (detectCmdId.current) finish(true);
      }, 60_000);
    } catch {
      setDetecting(false);
      toast.error("Could not start platform detection");
    }
  };

  const startDeploy = async () => {
    if (!selectedTemplate) return;
    setSubmitting(true);
    setErrorMsg("");
    setLogs([]);
    setPercent(0);
    setActiveStep(0);
    setCompletedSteps(new Set());
    try {
      const res = await deployInference(serverId, selectedTemplate);
      commandId.current = res.command_id;
      setPhase("progress");
      toast.message("Deploy queued — watching live progress");
    } catch (e: unknown) {
      const msg = e && typeof e === "object" && "response" in e
        ? String((e as { response?: { data?: { message?: string } } }).response?.data?.message || "Deploy failed")
        : "Deploy failed";
      setErrorMsg(msg);
      setPhase("failed");
    } finally {
      setSubmitting(false);
    }
  };

  const runTest = async () => {
    if (!endpointURL || !apiKey) {
      toast.error("Endpoint or API key missing");
      return;
    }
    setTesting(true);
    setTestReply("");
    setTestMs(null);
    const started = performance.now();
    try {
      const res = await fetch(`${endpointURL.replace(/\/$/, "")}/v1/chat/completions`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${apiKey}`,
        },
        body: JSON.stringify({
          messages: [{ role: "user", content: testPrompt }],
          max_tokens: 120,
        }),
      });
      const ms = Math.round(performance.now() - started);
      setTestMs(ms);
      const json = await res.json();
      const text =
        json?.choices?.[0]?.message?.content ||
        json?.content ||
        JSON.stringify(json).slice(0, 400);
      setTestReply(String(text));
    } catch (err) {
      setTestReply(err instanceof Error ? err.message : "Request failed");
    } finally {
      setTesting(false);
    }
  };

  const wowLine = useMemo(() => {
    if (benchmark?.tpsImp == null) return null;
    const n = Math.round(benchmark.tpsImp);
    return n >= 0 ? `${n}% faster on Arm` : `${Math.abs(n)}% slower (unexpected)`;
  }, [benchmark]);

  if (isLoading) return <ServerOverviewSkeleton />;
  if (error || !server) return <PageError message={error || "Server not found"} />;

  return (
    <FadeIn>
    <div className="mx-auto max-w-5xl space-y-8 px-4 py-8 sm:px-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-xs font-bold uppercase tracking-[0.14em] text-[var(--color-accent)]">
            Anchor Infer
          </p>
          <h1 className="mt-1 text-3xl font-extrabold tracking-tight text-[var(--color-ink)]">
            Deploy AI on Arm
          </h1>
          <p className="mt-2 max-w-2xl text-sm text-[var(--color-muted)]">
            Detect hardware, pick a model template, deploy an OpenAI-compatible endpoint, and
            compare generic vs KleidiAI-optimized builds — on{" "}
            <Link href={`/servers/${serverId}`} className="font-semibold text-[var(--color-accent)]">
              {server.name}
            </Link>
            .
          </p>
        </div>
        <div
          className={`inline-flex items-center gap-2 rounded-full px-3 py-1.5 text-xs font-semibold ${
            server.status === "connected"
              ? "bg-[var(--color-success-soft)] text-[var(--color-success)]"
              : "bg-[var(--color-paper-2)] text-[var(--color-muted)]"
          }`}
        >
          <span
            className={`h-2 w-2 rounded-full ${
              server.status === "connected" ? "bg-[var(--color-success)]" : "bg-[var(--color-muted)]"
            }`}
          />
          {server.status}
        </div>
      </div>

      {/* Section 1 — readiness + templates */}
      <section className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-5">
        <div className="mb-4 flex items-center gap-2">
          <Cpu className="h-4 w-4 text-[var(--color-accent)]" />
          <h2 className="text-sm font-bold uppercase tracking-wider text-[var(--color-muted)]">
            Server readiness
          </h2>
        </div>
        {platform ? (
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <Stat label="Architecture" value={platform.is_arm64 ? "Arm64" : "x86_64"} />
            <Stat
              label="CPU"
              value={platform.cpu_microarchitecture || platform.cpu_model_name || "Unknown"}
            />
            <Stat label="Optimization" value={platform.optimization_label || "—"} />
            <Stat
              label="Memory"
              value={`${platform.memory_available_gb?.toFixed?.(1) ?? "?"} GB free`}
            />
          </div>
        ) : (
          <p className="text-sm text-[var(--color-muted)]">
            No platform report yet. Connect an agent, then run detection.
          </p>
        )}
        <div className="mt-3">
          <Button
            variant="secondary"
            size="sm"
            onClick={startDetect}
            disabled={detecting || server.status !== "connected"}
          >
            {detecting ? (
              <>
                <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" /> Detecting…
              </>
            ) : platform ? (
              "Re-detect hardware"
            ) : (
              "Detect hardware"
            )}
          </Button>
        </div>
        {platform && !platform.can_run_inference && (
          <p className="mt-3 rounded-[var(--radius-md)] border border-[var(--color-danger)]/30 bg-[var(--color-danger-soft)] px-3 py-2 text-sm text-[var(--color-danger)]">
            {platform.block_reason || "This server cannot run inference right now."}
          </p>
        )}
        {platform && platform.is_arm64 === false && (
          <p className="mt-3 text-sm text-[var(--color-warning)]">
            For best results, use an Arm64 server. You can still deploy without Arm acceleration.
          </p>
        )}

        <h3 className="mb-3 mt-6 text-sm font-bold uppercase tracking-wider text-[var(--color-muted)]">
          Model templates
        </h3>
        <div className="grid gap-3 md:grid-cols-2">
          {templates.map((t) => {
            const active = selectedTemplate === t.id;
            return (
              <button
                key={t.id}
                type="button"
                onClick={() => setSelectedTemplate(t.id)}
                className={`rounded-[var(--radius-lg)] border p-4 text-left transition ${
                  active
                    ? "border-[var(--color-accent)] bg-[var(--color-accent-soft)]"
                    : "border-[var(--color-border)] hover:border-[var(--color-accent)]/40"
                }`}
              >
                <div className="flex items-center gap-2">
                  <Sparkles className="h-4 w-4 text-[var(--color-accent)]" />
                  <span className="font-bold text-[var(--color-ink)]">{t.name}</span>
                </div>
                <p className="mt-2 text-sm text-[var(--color-muted)]">{t.description}</p>
                <p className="mt-3 text-xs text-[var(--color-muted)]">
                  {t.model.family} {t.model.size} · {t.model.default_quant} · needs{" "}
                  {t.resources.min_ram_gb}GB RAM
                </p>
              </button>
            );
          })}
        </div>
        <div className="mt-5">
          <Button
            onClick={startDeploy}
            disabled={
              !selectedTemplate ||
              submitting ||
              phase === "progress" ||
              server.status !== "connected" ||
              (platform != null && platform.can_run_inference === false)
            }
          >
            {submitting || phase === "progress" ? (
              <>
                <Loader2 className="mr-2 h-4 w-4 animate-spin" /> Deploying…
              </>
            ) : (
              "Deploy to server →"
            )}
          </Button>
        </div>
      </section>

      {/* Section 2 — progress */}
      {(phase === "progress" || phase === "failed") && (
        <section className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-5">
          <h2 className="text-sm font-bold uppercase tracking-wider text-[var(--color-muted)]">
            Deploy progress
          </h2>
          <div className="mt-2 h-2 overflow-hidden rounded-full bg-[var(--color-paper-2)]">
            <div
              className="h-full bg-[var(--color-accent)] transition-all"
              style={{ width: `${percent}%` }}
            />
          </div>
          <ul className="mt-4 space-y-2">
            {DEPLOY_STEPS.map((s, i) => {
              const done = completedSteps.has(i);
              const active = phase === "progress" && i === activeStep;
              const failed = phase === "failed" && i === activeStep;
              return (
                <li key={s.key} className="flex items-center gap-2 text-sm">
                  {done ? (
                    <Check className="h-4 w-4 text-[var(--color-success)]" />
                  ) : failed ? (
                    <XCircle className="h-4 w-4 text-[var(--color-danger)]" />
                  ) : active ? (
                    <Loader2 className="h-4 w-4 animate-spin text-[var(--color-accent)]" />
                  ) : (
                    <Circle className="h-4 w-4 text-[var(--color-muted)]" />
                  )}
                  <span className={done || active ? "text-[var(--color-ink)]" : "text-[var(--color-muted)]"}>
                    {s.label}
                  </span>
                </li>
              );
            })}
          </ul>
          {errorMsg && (
            <p className="mt-4 rounded-[var(--radius-md)] border border-[var(--color-danger)]/30 bg-[var(--color-danger-soft)] px-3 py-2 text-sm text-[var(--color-danger)]">
              {errorMsg}
            </p>
          )}
          <div className="mt-4 max-h-48 overflow-y-auto rounded-[var(--radius-md)] bg-[#0b1b2b] p-3 font-mono text-xs text-[#cfe2ff]">
            {logs.length === 0 ? (
              <span className="opacity-60">Waiting for agent progress…</span>
            ) : (
              logs.map((line, i) => <div key={i}>{line}</div>)
            )}
            <div ref={logEnd} />
          </div>
        </section>
      )}

      {/* Section 3 — live endpoint */}
      {(phase === "done" || status?.deployed) && endpointURL && (
        <section className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-5">
          <h2 className="text-lg font-bold text-[var(--color-ink)]">✓ Your AI endpoint is live</h2>
          <div className="mt-4 flex flex-wrap items-center gap-2 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-paper)] px-3 py-3">
            <code className="flex-1 break-all text-sm font-semibold text-[var(--color-ink)]">
              {endpointURL}
            </code>
            <Button variant="secondary" size="sm" onClick={() => copyText("URL", endpointURL)}>
              <Copy className="h-3.5 w-3.5" />
            </Button>
          </div>
          {apiKey && (
            <div className="mt-3 flex flex-wrap items-center gap-2 rounded-[var(--radius-md)] border border-[var(--color-border)] px-3 py-3">
              <span className="text-xs font-bold uppercase text-[var(--color-muted)]">API key</span>
              <code className="flex-1 font-mono text-sm">
                {revealKey ? apiKey : "sk-••••••••••••••••••"}
              </code>
              <Button variant="ghost" size="sm" onClick={() => setRevealKey((v) => !v)}>
                {revealKey ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
              </Button>
              <Button variant="secondary" size="sm" onClick={() => copyText("API key", apiKey)}>
                <Copy className="h-3.5 w-3.5" />
              </Button>
            </div>
          )}
          {optimization && (
            <p className="mt-3 text-sm text-[var(--color-muted)]">
              Arm optimization: <strong className="text-[var(--color-ink)]">{optimization}</strong>
            </p>
          )}

          <div className="mt-6">
            <label className="text-sm font-semibold text-[var(--color-ink)]">Send a test message</label>
            <textarea
              className="mt-2 w-full rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-paper)] p-3 text-sm"
              rows={3}
              value={testPrompt}
              onChange={(e) => setTestPrompt(e.target.value)}
            />
            <div className="mt-2 flex items-center gap-3">
              <Button onClick={runTest} disabled={testing}>
                {testing ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
                Send
              </Button>
              {testMs != null && (
                <span className="text-xs text-[var(--color-muted)]">Response in {testMs}ms</span>
              )}
            </div>
            {testReply && (
              <pre className="mt-3 whitespace-pre-wrap rounded-[var(--radius-md)] bg-[var(--color-paper-2)] p-3 text-sm text-[var(--color-ink)]">
                {testReply}
              </pre>
            )}
          </div>
        </section>
      )}

      {/* Section 4 — benchmarks */}
      {benchmark?.optimized && benchmark?.generic && (
        <section className="rounded-[var(--radius-lg)] border border-[var(--color-border)] bg-[var(--color-surface)] p-5">
          <h2 className="text-sm font-bold uppercase tracking-wider text-[var(--color-muted)]">
            Benchmark results
          </h2>
          {wowLine && (
            <p className="mt-3 text-4xl font-extrabold tracking-tight text-[var(--color-accent)]">
              {wowLine}
            </p>
          )}
          <div className="mt-5 overflow-x-auto">
            <table className="w-full min-w-[32rem] text-left text-sm">
              <thead>
                <tr className="border-b border-[var(--color-border)] text-[var(--color-muted)]">
                  <th className="py-2 font-semibold">Metric</th>
                  <th className="py-2 font-semibold">Generic</th>
                  <th className="py-2 font-semibold text-[var(--color-accent)]">Arm optimized</th>
                  <th className="py-2 font-semibold">Improvement</th>
                </tr>
              </thead>
              <tbody>
                <tr className="border-b border-[var(--color-border)]/60">
                  <td className="py-3">Generation speed</td>
                  <td>{benchmark.generic.median_tokens_per_second?.toFixed?.(1)} tok/s</td>
                  <td className="font-semibold text-[var(--color-accent)]">
                    {benchmark.optimized.median_tokens_per_second?.toFixed?.(1)} tok/s
                  </td>
                  <td>
                    {benchmark.tpsImp != null ? `${Math.round(benchmark.tpsImp)}%` : "—"}
                  </td>
                </tr>
                <tr>
                  <td className="py-3">Time to 1st token</td>
                  <td>{benchmark.generic.median_ttft_ms ?? benchmark.generic.median_time_to_first_token_ms} ms</td>
                  <td className="font-semibold text-[var(--color-accent)]">
                    {benchmark.optimized.median_ttft_ms ?? benchmark.optimized.median_time_to_first_token_ms} ms
                  </td>
                  <td>
                    {benchmark.ttftImp != null ? `${Math.round(benchmark.ttftImp)}% lower` : "—"}
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </section>
      )}
    </div>
    </FadeIn>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-paper)] px-3 py-2">
      <p className="text-[10px] font-bold uppercase tracking-wider text-[var(--color-muted)]">{label}</p>
      <p className="mt-1 text-sm font-semibold text-[var(--color-ink)]">{value}</p>
    </div>
  );
}
