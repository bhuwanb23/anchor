import api from "./api";
import type {
  PlatformInfo,
  InferenceTemplate,
  DeployInferenceResponse,
  InferenceStatus,
  BenchmarkComparison,
} from "@/types";

export async function fetchInferTemplates(): Promise<InferenceTemplate[]> {
  const { data } = await api.get<InferenceTemplate[]>("/api/v1/infer/templates");
  return data;
}

export async function fetchServerPlatform(serverId: string): Promise<PlatformInfo | null> {
  try {
    const { data } = await api.get<PlatformInfo>(`/api/v1/servers/${serverId}/platform`);
    return data;
  } catch {
    return null;
  }
}

export async function detectServerPlatform(serverId: string): Promise<{ command_id: string; status: string }> {
  const { data } = await api.post<{ command_id: string; status: string }>(
    `/api/v1/servers/${serverId}/platform/detect`,
  );
  return data;
}

export async function deployInference(
  serverId: string,
  templateId: string,
  domain?: string,
): Promise<DeployInferenceResponse> {
  const body: Record<string, string> = { template_id: templateId };
  if (domain) body.domain = domain;
  const { data } = await api.post<DeployInferenceResponse>(
    `/api/v1/servers/${serverId}/infer/deploy`,
    body,
  );
  return data;
}

export async function fetchInferenceStatus(serverId: string): Promise<InferenceStatus> {
  const { data } = await api.get<InferenceStatus>(`/api/v1/servers/${serverId}/infer/status`);
  return data;
}

export async function fetchInferBenchmarks(serverId: string): Promise<{
  available: boolean;
  tokens_per_second_improvement_pct?: number;
  ttft_improvement_pct?: number;
  memory_difference_bytes?: number;
  optimized?: BenchmarkComparison["optimized"];
  generic?: BenchmarkComparison["generic"];
}> {
  const { data } = await api.get(`/api/v1/servers/${serverId}/infer/benchmarks`);
  return data;
}
