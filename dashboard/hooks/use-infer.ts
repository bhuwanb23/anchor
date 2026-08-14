"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import api from "@/lib/api";
import { getWSClient, type WSMessage } from "@/lib/ws";
import type {
  InferenceTemplate,
  InferenceDeployResult,
  PlatformInfo,
  BenchmarkComparison,
} from "@/types";

// ---------------------------------------------------------------------------
// Deploy steps — Section 2 (Deploy Progress) step list.
// Mapped from the `phase` + `percent` fields the agent sends in
// command_progress messages. The agent emits `benchmarking` twice (generic at
// ~47%, optimized at ~88%), so percent disambiguates which benchmark step is
// active.
// ---------------------------------------------------------------------------

export const INFER_DEPLOY_STEPS = [
  { key: "validating", label: "Checking server capabilities" },
  { key: "pulling", label: "Pulling runtime images" },
  { key: "volume", label: "Preparing model storage" },
  { key: "downloading", label: "Downloading model weights" },
  { key: "benchmarking", label: "Benchmarking generic build" },
  { key: "starting", label: "Starting optimized inference server" },
  { key: "routing", label: "Configuring HTTPS endpoint" },
  { key: "benchmarking", label: "Benchmarking optimized build" },
] as const;

export const INFER_BENCH_STEPS = [
  { key: "benchmarking", label: "Benchmarking generic build" },
  { key: "benchmarking", label: "Benchmarking optimized build" },
] as const;

export type InferDeployPhase = "idle" | "deploying" | "benchmarking" | "succeeded" | "failed";
export type InferDeployMode = "deploy" | "benchmark";

export interface InferProgress {
  command_id: string;
  phase: string;
  message: string;
  percent: number;
}

export interface InferLogLine {
  at: number;
  text: string;
}

export interface InferStepTiming {
  started: number;
  done?: number;
}

export interface InferDeployState {
  phase: InferDeployPhase;
  mode: InferDeployMode;
  commandId: string | null;
  progress: InferProgress | null;
  result: InferenceDeployResult | null;
  error: string | null;
  startedAt: number | null;
  log: InferLogLine[];
  stepTimings: Record<number, InferStepTiming>;
  stepIndex: number;
}

// Map an agent (phase, percent) pair to the 0-based index of the active step.
// Returns steps.length for the fully-complete state.
export function stepIndexFor(
  mode: InferDeployMode,
  phase: string,
  percent: number
): number {
  const steps = mode === "benchmark" ? INFER_BENCH_STEPS : INFER_DEPLOY_STEPS;
  if (phase === "complete") return steps.length;
  switch (phase) {
    case "validating":
      return 0;
    case "pulling":
      return 1;
    case "volume":
      return 2;
    case "downloading":
    case "credentials":
      return 3;
    case "benchmarking":
      // Generic benchmark runs at ~47%, optimized at ~88%.
      return percent < 70
        ? mode === "benchmark"
          ? 0
          : 4
        : mode === "benchmark"
          ? 1
          : 7;
    case "starting":
    case "loading":
      return mode === "benchmark" ? 1 : 5;
    case "routing":
    case "testing":
      return mode === "benchmark" ? 1 : 6;
    default:
      return 0;
  }
}

// ---------------------------------------------------------------------------
// Templates — static list served by the control plane
// ---------------------------------------------------------------------------

export function useInferTemplates() {
  const [templates, setTemplates] = useState<InferenceTemplate[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const mountedRef = useRef(true);

  const fetchTemplates = useCallback(async () => {
    try {
      const res = await api.get<InferenceTemplate[]>("/api/v1/infer/templates");
      if (mountedRef.current) {
        setTemplates(Array.isArray(res.data) ? res.data : []);
        setError(null);
      }
    } catch (e) {
      if (mountedRef.current) {
        setError(e instanceof Error ? e.message : "Failed to load templates");
      }
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    fetchTemplates();
    return () => {
      mountedRef.current = false;
    };
  }, [fetchTemplates]);

  return { templates, isLoading, error, refetch: fetchTemplates };
}

// ---------------------------------------------------------------------------
// Platform readiness — the server's pre-computed Infer capabilities
// ---------------------------------------------------------------------------

export function usePlatformInfo(serverId: string | null) {
  const [platform, setPlatform] = useState<PlatformInfo | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const mountedRef = useRef(true);

  const fetchPlatform = useCallback(async () => {
    if (!serverId) return;
    try {
      setIsLoading(true);
      const res = await api.get<PlatformInfo>(
        `/api/v1/servers/${serverId}/platform`
      );
      if (mountedRef.current) {
        setPlatform(res.data);
        setError(null);
      }
    } catch (e) {
      if (mountedRef.current) {
        setPlatform(null);
        setError(
          (e as { response?: { status?: number } })?.response?.status === 404
            ? "Agent has not reported server capabilities yet."
            : e instanceof Error
              ? e.message
              : "Failed to load server capabilities"
        );
      }
    } finally {
      if (mountedRef.current) setIsLoading(false);
    }
  }, [serverId]);

  const [detecting, setDetecting] = useState(false);

  const detectPlatform = useCallback(async () => {
    if (!serverId) return;
    setDetecting(true);
    try {
      await api.post(`/api/v1/servers/${serverId}/platform/detect`);
      // Agent usually finishes detect_platform in a few seconds.
      for (let i = 0; i < 15; i++) {
        await new Promise((r) => setTimeout(r, 1000));
        try {
          const res = await api.get<PlatformInfo>(
            `/api/v1/servers/${serverId}/platform`
          );
          if (mountedRef.current && res.data) {
            setPlatform(res.data);
            setError(null);
            break;
          }
        } catch {
          /* keep polling */
        }
      }
    } catch (e) {
      if (mountedRef.current) {
        setError(e instanceof Error ? e.message : "Platform detection failed");
      }
    } finally {
      if (mountedRef.current) setDetecting(false);
    }
  }, [serverId]);

  useEffect(() => {
    mountedRef.current = true;
    if (serverId) fetchPlatform();
    return () => {
      mountedRef.current = false;
    };
  }, [serverId, fetchPlatform]);

  return { platform, isLoading, error, detecting, refetch: fetchPlatform, detectPlatform };
}

// ---------------------------------------------------------------------------
// Deploy / benchmark — POST the command, then track command_progress and
// command_result over the shared WebSocket.
// ---------------------------------------------------------------------------

const initialState: InferDeployState = {
  phase: "idle",
  mode: "deploy",
  commandId: null,
  progress: null,
  result: null,
  error: null,
  startedAt: null,
  log: [],
  stepTimings: {},
  stepIndex: 0,
};

const MAX_LOG_LINES = 300;

export type WsConnectionStatus = "connected" | "connecting" | "disconnected";

// Extract a readable text fragment from an agent broadcast payload
// (log_line / log_lines / pull_progress / docker_status / state_update).
function extractLogText(payload: unknown): string | null {
  if (!payload || typeof payload !== "object") return null;
  const p = payload as Record<string, unknown>;
  if (typeof p.message === "string" && p.message) return p.message;
  if (typeof p.line === "string" && p.line) return p.line;
  if (typeof p.status === "string" && p.status) return p.status;
  if (typeof p.error === "string" && p.error) return p.error;
  return null;
}

export function useInferDeploy(serverId: string | null) {
  const [state, setState] = useState<InferDeployState>(initialState);
  const [wsStatus, setWsStatus] = useState<WsConnectionStatus>("connecting");
  const unsubRef = useRef<(() => void)[]>([]);
  const mountedRef = useRef(true);
  const lastTemplateRef = useRef<string | null>(null);
  const hadCommandRef = useRef(false);

  // Parse the command output — the agent returns a JSON string with the
  // endpoint URL, API key, and (for benchmark runs) a fresh comparison.
  const parseResult = useCallback((output: string): InferenceDeployResult => {
    try {
      return JSON.parse(output) as InferenceDeployResult;
    } catch {
      return { error: output };
    }
  }, []);

  const appendLog = useCallback(
    (prev: InferDeployState, text: string): InferDeployState => {
      const next: InferLogLine[] = [...prev.log, { at: Date.now(), text }];
      if (next.length > MAX_LOG_LINES) next.splice(0, next.length - MAX_LOG_LINES);
      return { ...prev, log: next };
    },
    []
  );

  const advanceStep = useCallback(
    (prev: InferDeployState, idx: number): InferDeployState => {
      if (idx === prev.stepIndex) return prev;
      const timings = { ...prev.stepTimings };
      const now = Date.now();
      // Mark the previous active step as done.
      const prevTiming = timings[prev.stepIndex];
      if (prevTiming && !prevTiming.done) {
        timings[prev.stepIndex] = { ...prevTiming, done: now };
      }
      // Start timing the newly active step.
      const nextTiming = timings[idx];
      if (!nextTiming) {
        timings[idx] = { started: now };
      }
      return { ...prev, stepTimings: timings, stepIndex: idx };
    },
    []
  );

  const applyProgress = useCallback(
    (prev: InferDeployState, phase: string, message: string, percent: number): InferDeployState => {
      const idx = stepIndexFor(prev.mode, phase, percent);
      let next: InferDeployState = {
        ...prev,
        progress: { command_id: prev.commandId || "", phase, message, percent },
      };
      next = advanceStep(next, idx);
      return appendLog(next, message || phase);
    },
    [advanceStep, appendLog]
  );

  const deploy = useCallback(
    async (templateId: string): Promise<boolean> => {
      if (!serverId) return false;
      lastTemplateRef.current = templateId;
      hadCommandRef.current = true;
      try {
        const res = await api.post<{ command_id: string; status: string }>(
          `/api/v1/servers/${serverId}/infer/deploy`,
          { template_id: templateId }
        );
        const commandId = res.data.command_id;
        const now = Date.now();
        setState({
          ...initialState,
          phase: "deploying",
          mode: "deploy",
          commandId,
          startedAt: now,
          stepTimings: { 0: { started: now } },
          log: [{ at: now, text: `Deploying ${templateId}…` }],
          progress: {
            command_id: commandId,
            phase: "validating",
            message: "Starting deployment…",
            percent: 2,
          },
        });
        return true;
      } catch (e) {
        setState({
          ...initialState,
          phase: "failed",
          error:
            (e as { response?: { data?: { error?: string } } })?.response?.data
              ?.error ||
            (e instanceof Error ? e.message : "Deploy failed to start"),
        });
        return false;
      }
    },
    [serverId]
  );

  // Re-run the benchmark pipeline against the existing deployment. The
  // previously deployed endpoint stays up; only the comparison is refreshed.
  const runBenchmark = useCallback(
    async (templateId: string): Promise<boolean> => {
      if (!serverId) return false;
      lastTemplateRef.current = templateId;
      hadCommandRef.current = true;
      try {
        const res = await api.post<{ command_id: string; status: string }>(
          `/api/v1/servers/${serverId}/infer/benchmark`,
          { template_id: templateId }
        );
        const commandId = res.data.command_id;
        const now = Date.now();
        setState((prev) => ({
          ...prev,
          phase: "benchmarking",
          mode: "benchmark",
          commandId,
          error: null,
          startedAt: now,
          stepTimings: { 0: { started: now } },
          stepIndex: 0,
          log: [...prev.log, { at: now, text: "Re-running benchmark…" }],
          progress: {
            command_id: commandId,
            phase: "benchmarking",
            message: "Running baseline benchmark…",
            percent: 10,
          },
        }));
        return true;
      } catch (e) {
        setState((prev) => ({
          ...prev,
          phase: "failed",
          error:
            (e as { response?: { data?: { error?: string } } })?.response?.data
              ?.error ||
            (e instanceof Error ? e.message : "Benchmark failed to start"),
        }));
        return false;
      }
    },
    [serverId]
  );

  // Try Again — re-deploy the last template from scratch.
  const retry = useCallback((): Promise<boolean> => {
    if (!lastTemplateRef.current) return Promise.resolve(false);
    return deploy(lastTemplateRef.current);
  }, [deploy]);

  const reset = useCallback(() => {
    setState(initialState);
  }, []);

  // Restore a pre-computed deploy result (e.g. from GET /infer/status on
  // page load) so Sections 3 and 4 populate immediately.
  const restore = useCallback((result: InferenceDeployResult) => {
    const now = Date.now();
    setState({
      ...initialState,
      phase: "succeeded",
      mode: "deploy",
      result,
      startedAt: now,
      log: [{ at: now, text: "Deployment restored from saved state." }],
      progress: {
        command_id: "",
        phase: "complete",
        message: "Deployment complete",
        percent: 100,
      },
      stepIndex: INFER_DEPLOY_STEPS.length,
    });
  }, []);

  // Re-fetch the current deploy state from the control plane. Used on page
  // load and after a WebSocket reconnect so the UI reflects what happened
  // while disconnected.
  const refreshFromAPI = useCallback(async () => {
    if (!serverId) return;
    try {
      const statusRes = await api.get<{
        deployed: boolean;
        status?: string;
        details?: InferenceDeployResult;
      }>(`/api/v1/servers/${serverId}/infer/status`);
      if (!mountedRef.current) return;
      if (!statusRes.data?.deployed || !statusRes.data.details) {
        // No completed deploy yet. If a command is in flight, leave the UI
        // alone so the live progress keeps showing; otherwise stay idle.
        return;
      }
      const details = { ...statusRes.data.details };
      // Fold in the persisted benchmark comparison if available.
      try {
        const benchRes = await api.get<BenchmarkComparison>(
          `/api/v1/servers/${serverId}/infer/benchmark`
        );
        if (mountedRef.current && benchRes.data?.optimized) {
          details.benchmark_comparison = benchRes.data;
          details.benchmarked_at = benchRes.data.benchmarked_at || details.benchmarked_at;
        }
      } catch {
        // No stored benchmark yet.
      }
      if (mountedRef.current) {
        restore(details);
        if (details.template_id) lastTemplateRef.current = details.template_id;
      }
    } catch {
      // Status endpoint unavailable — leave the current UI state alone.
    }
  }, [serverId, restore]);

  useEffect(() => {
    mountedRef.current = true;
    const client = getWSClient();

    const onProgress = (msg: WSMessage) => {
      const p = (msg.payload || {}) as {
        command_id?: string;
        phase?: string;
        message?: string;
        percent?: number;
      };
      if (!p.command_id) return;
      setState((prev) => {
        if (prev.commandId && p.command_id !== prev.commandId) return prev;
        return applyProgress(prev, p.phase || "", p.message || "", typeof p.percent === "number" ? p.percent : prev.progress?.percent ?? 0);
      });
    };

    const onResult = (msg: WSMessage) => {
      const p = (msg.payload || msg) as {
        command_id?: string;
        status?: string;
        output?: string;
        error?: string;
      };
      if (!p.command_id) return;
      setState((prev) => {
        if (prev.commandId && p.command_id !== prev.commandId) return prev;
        if (p.status === "success") {
          const parsed = p.output ? parseResult(p.output) : null;
          const now = Date.now();
          if (prev.mode === "benchmark" && prev.result) {
            // Merge the fresh comparison into the existing endpoint result.
            const merged: InferenceDeployResult = {
              ...prev.result,
              benchmark_comparison:
                (parsed?.benchmark_comparison as BenchmarkComparison) ||
                prev.result.benchmark_comparison,
              benchmarked_at:
                parsed?.benchmarked_at || prev.result.benchmarked_at || new Date(now).toISOString(),
            };
            return {
              ...prev,
              phase: "succeeded",
              error: null,
              result: merged,
              stepTimings: {
                ...prev.stepTimings,
                [prev.stepIndex]: { ...prev.stepTimings[prev.stepIndex], done: now },
              },
              stepIndex: INFER_BENCH_STEPS.length,
              log: [...prev.log, { at: now, text: "Benchmark complete." }],
            };
          }
          return {
            ...prev,
            phase: "succeeded",
            error: null,
            result: parsed
              ? {
                  ...parsed,
                  benchmarked_at: parsed.benchmarked_at || new Date(now).toISOString(),
                }
              : prev.result,
            stepTimings: {
              ...prev.stepTimings,
              [prev.stepIndex]: { ...prev.stepTimings[prev.stepIndex], done: now },
            },
            stepIndex: INFER_DEPLOY_STEPS.length,
            log: [...prev.log, { at: now, text: parsed?.endpoint_url ? "Deploy complete." : "Benchmark complete." }],
          };
        }
        return {
          ...prev,
          phase: "failed",
          error: p.error || p.output || "Command failed",
        };
      });
    };

    // Raw agent output for this server: docker pull progress, log lines, and
    // state updates all land in the Section 2 terminal log. Only surfaced
    // while a deploy/benchmark command is active to avoid noise.
    const onBroadcast = (msg: WSMessage) => {
      setState((prev) => {
        if (prev.phase !== "deploying" && prev.phase !== "benchmarking") return prev;
        const text = extractLogText(msg.payload);
        if (!text) return prev;
        return appendLog(prev, text);
      });
    };

    // Connection lifecycle: track status and re-sync from the API after a
    // reconnect if we had issued a command.
    const onConnect = () => {
      setWsStatus("connected");
      if (serverId) client.subscribeServer(serverId);
      if (hadCommandRef.current) {
        void refreshFromAPI();
      }
    };
    const onDisconnect = () => setWsStatus("disconnected");
    const onReconnecting = () => setWsStatus("connecting");

    if (serverId) client.subscribeServer(serverId);

    // Command results arrive as `result` from the agent; the hub may also
    // emit `command_result`. Listen to both (existing dialogs do the same).
    unsubRef.current.forEach((fn) => fn());
    unsubRef.current = [
      client.on("command_progress", onProgress),
      client.on("command_result", onResult),
      client.on("result", onResult),
      client.on("log_line", onBroadcast),
      client.on("log_lines", onBroadcast),
      client.on("pull_progress", onBroadcast),
      client.on("state_update", onBroadcast),
      client.on("docker_status", onBroadcast),
      client.onConnect(onConnect),
      client.onDisconnect(onDisconnect),
      client.onReconnecting(onReconnecting),
    ];
    client.connect();

    return () => {
      mountedRef.current = false;
      if (serverId) client.unsubscribeServer();
      unsubRef.current.forEach((fn) => fn());
      unsubRef.current = [];
    };
  }, [applyProgress, parseResult, appendLog, serverId, refreshFromAPI]);

  return { ...state, wsStatus, deploy, runBenchmark, retry, reset, restore, refreshFromAPI };
}
