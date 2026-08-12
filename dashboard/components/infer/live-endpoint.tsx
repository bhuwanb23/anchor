"use client";

import { useState } from "react";
import { Check, Copy, ExternalLink, Eye, EyeOff, KeyRound, Globe } from "lucide-react";
import { Card, CardContent } from "@/components/ui/card";
import type { InferenceDeployResult } from "@/types";

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      aria-label="Copy"
      onClick={() => {
        navigator.clipboard?.writeText(text).catch(() => {});
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
      }}
      className="ml-2 inline-flex h-7 w-7 items-center justify-center rounded-[var(--radius-sm)] text-[var(--color-muted)] transition-colors hover:bg-[var(--color-paper-2)] hover:text-[var(--color-ink)]"
    >
      {copied ? (
        <Check className="h-3.5 w-3.5 text-[var(--color-success)]" />
      ) : (
        <Copy className="h-3.5 w-3.5" />
      )}
    </button>
  );
}

interface LiveEndpointProps {
  result: InferenceDeployResult;
}

export function LiveEndpoint({ result }: LiveEndpointProps) {
  const [revealKey, setRevealKey] = useState(false);
  const endpoint = result.endpoint_url || (result.domain ? `https://${result.domain}` : "");
  const apiKey = result.api_key || "";
  const masked = apiKey ? `sk-${"•".repeat(Math.min(12, apiKey.length - 3))}` : "";

  return (
    <Card className="border-[var(--color-success)]/40">
      <CardContent className="space-y-5">
        <div className="flex items-center gap-2">
          <span className="flex h-6 w-6 items-center justify-center rounded-full bg-[var(--color-success)] text-xs font-bold text-white">
            ✓
          </span>
          <h2 className="text-lg font-bold tracking-tight text-[var(--color-ink)]">
            Your AI endpoint is live
          </h2>
        </div>

        {endpoint && (
          <div className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-paper-2)] p-4">
            <p className="mb-1 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-[var(--color-muted)]">
              <Globe className="h-3.5 w-3.5" /> Endpoint URL
            </p>
            <div className="flex items-center">
              <a
                href={endpoint}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex min-w-0 items-center gap-1.5 truncate font-mono text-sm font-semibold text-[var(--color-accent)] hover:underline"
              >
                {endpoint}
                <ExternalLink className="h-3.5 w-3.5 shrink-0" />
              </a>
              <CopyButton text={endpoint} />
            </div>
          </div>
        )}

        {apiKey && (
          <div className="rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-paper-2)] p-4">
            <p className="mb-1 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wider text-[var(--color-muted)]">
              <KeyRound className="h-3.5 w-3.5" /> API key
            </p>
            <div className="flex items-center">
              <code className="truncate font-mono text-sm text-[var(--color-ink)]">
                {revealKey ? apiKey : masked}
              </code>
              <button
                type="button"
                aria-label={revealKey ? "Hide API key" : "Reveal API key"}
                onClick={() => setRevealKey((v) => !v)}
                className="ml-2 inline-flex h-7 w-7 items-center justify-center rounded-[var(--radius-sm)] text-[var(--color-muted)] transition-colors hover:bg-[var(--color-paper-2)] hover:text-[var(--color-ink)]"
              >
                {revealKey ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
              </button>
              <CopyButton text={apiKey} />
            </div>
            <p className="mt-2 text-xs text-[var(--color-muted)]">
              Send it in the <code className="font-mono">Authorization: Bearer</code> header.
              Save this key now — you can always find it in your environment variables.
            </p>
          </div>
        )}

        <div className="grid gap-2 text-sm sm:grid-cols-3">
          {result.optimization && (
            <div className="rounded-[var(--radius-md)] bg-[var(--color-paper-2)] px-3 py-2.5">
              <p className="text-xs text-[var(--color-muted)]">Arm optimization</p>
              <p className="mt-0.5 font-semibold text-[var(--color-ink)]">
                {result.optimization}
              </p>
            </div>
          )}
          {result.quantization && (
            <div className="rounded-[var(--radius-md)] bg-[var(--color-paper-2)] px-3 py-2.5">
              <p className="text-xs text-[var(--color-muted)]">Model</p>
              <p className="mt-0.5 font-semibold text-[var(--color-ink)]">
                {result.model_file?.replace(/\.gguf$/, "") || result.quantization}
              </p>
            </div>
          )}
          <div className="rounded-[var(--radius-md)] bg-[var(--color-paper-2)] px-3 py-2.5">
            <p className="text-xs text-[var(--color-muted)]">API path</p>
            <p className="mt-0.5 font-mono text-sm font-semibold text-[var(--color-ink)]">
              {result.api_path || "/v1/chat/completions"}
            </p>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
