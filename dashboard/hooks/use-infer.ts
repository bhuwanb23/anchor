"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import api from "@/lib/api";
import { getWSClient, type WSMessage } from "@/lib/ws";
import type {
  InferenceTemplate,
  InferenceDeployResult,
  PlatformInfo,
} from "@/types";

// ---------------------------------------------------------------------------
// Deploy phases — the step list shown in Section 2 (Deploy Progress).
// Keyed by the `phase` field the agent sends in command_progress messages.
// ---------------------------------------------------------------------------

export const INFER_DEPLOY_STEPS = [
  { key: "validating", label: "Checking server capabilities" },
  { key: "pulling", label: "Pulling runtime images" },
  { key: "volume", label: "Preparing model storage" },
  { key: "downloading", label: "Downloading model weights" },
  { key: "credentials", label: "Generating API credentials" },
  { key: "benchmarking", label: "Benchmarking generic build" },
  { key: "starting", label: "Starting optimized inference server" },
  { key: "loading", label: "Loading model into memory" },
  { key: "routing", label: "Configuring HTTPS endpoint" },
  { key: "testing", label: "Testing endpoint" },
  { key: "complete", label: "Deployment complete" },
] as const;

export type InferDeployPhase = "idle" | "deploying" | "succeeded" | "failed";

export interface InferProgress {
  command_id: string;
  phase: string;
  message: string;
  percent: number;
}

export interface InferDeployState {
  phase: InferDeployPhase;
  commandId: string | null;
  progress: InferProgress | null;
  result: InferenceDeployResult | null;
  error: string | null;
  startedAt: number | null;
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

  useEffect(() => {
    mountedRef.current = true;
    if (serverId) fetchPlatform();
    return () => {
      mountedRef.current = false;
    };
  }, [serverId, fetchPlatform]);

  return { platform, isLoading, error, refetch: fetchPlatform };
}

// ---------------------------------------------------------------------------
// Deploy — POST the deploy command, then track command_progress /
// command_result over the shared WebSocket.
// ---------------------------------------------------------------------------

const initialState: InferDeployState = {
  phase: "idle",
  commandId: null,
  progress: null,
  result: null,
  error: null,
  startedAt: null,
};

export function useInferDeploy(serverId: string | null) {
  const [state, setState] = useState<InferDeployState>(initialState);
  const unsubRef = useRef<(() => void)[]>([]);
  const mountedRef = useRef(true);

  // Parse the deploy result output — the agent returns a JSON string with
  // the endpoint URL, API key, and benchmark comparison.
  const parseResult = useCallback((output: string): InferenceDeployResult => {
    try {
      const parsed = JSON.parse(output);
      return parsed as InferenceDeployResult;
    } catch {
      return { error: output };
    }
  }, []);

  const deploy = useCallback(
    async (templateId: string): Promise<boolean> => {
      if (!serverId) return false;
      try {
        const res = await api.post<{ command_id: string; status: string }>(
          `/api/v1/servers/${serverId}/infer/deploy`,
          { template_id: templateId }
        );
        const commandId = res.data.command_id;
        setState({
          phase: "deploying",
          commandId,
          progress: { command_id: commandId, phase: "validating", message: "Starting deployment…", percent: 2 },
          result: null,
          error: null,
          startedAt: Date.now(),
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

  const reset = useCallback(() => {
    setState(initialState);
  }, []);

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
        return {
          ...prev,
          phase: "deploying",
          commandId: p.command_id || null,
          progress: {
            command_id: p.command_id || prev.commandId || "",
            phase: p.phase || "",
            message: p.message || "",
            percent: typeof p.percent === "number" ? p.percent : prev.progress?.percent ?? 0,
          },
        };
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
          const result = p.output ? parseResult(p.output) : null;
          return {
            ...prev,
            phase: "succeeded",
            progress: prev.progress
              ? { ...prev.progress, percent: 100, phase: "complete" }
              : prev.progress,
            result,
            error: null,
          };
        }
        return {
          ...prev,
          phase: "failed",
          error: p.error || p.output || "Deploy failed",
        };
      });
    };

    // Command results arrive as `result` from the agent; the hub may also
    // emit `command_result`. Listen to both (existing dialogs do the same).
    unsubRef.current.forEach((fn) => fn());
    unsubRef.current = [
      client.on("command_progress", onProgress),
      client.on("command_result", onResult),
      client.on("result", onResult),
    ];
    client.connect();

    return () => {
      mountedRef.current = false;
      unsubRef.current.forEach((fn) => fn());
      unsubRef.current = [];
    };
  }, [parseResult]);

  return { ...state, deploy, reset };
}
