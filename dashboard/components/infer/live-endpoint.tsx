"use client";

import { useEffect, useState } from "react";
import {
  Check,
  ChevronDown,
  Copy,
  ExternalLink,
  Eye,
  EyeOff,
  Globe,
  KeyRound,
  Loader2,
  Send,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import type { InferenceDeployResult } from "@/types";

function CopyButton({ text, label = "Copy" }: { text: string; label?: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      aria-label={label}
      onClick={() => {
        navigator.clipboard?.writeText(text).catch(() => {});
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
      }}
      className="ml-2 inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-sm)] text-[var(--color-muted)] transition-colors hover:bg-[var(--color-paper-2)] hover:text-[var(--color-ink)]"
    >
      {copied ? (
        <Check className="h-3.5 w-3.5 text-[var(--color-success)]" />
      ) : (
        <Copy className="h-3.5 w-3.5" />
      )}
    </button>
  );
}

interface TestResponse {
  content: string;
  latencyMs: number;
  error?: string;
}

interface LiveEndpointProps {
  result: InferenceDeployResult;
  modelLabel: string;
  deployedAt?: string;
}

export function LiveEndpoint({ result, modelLabel, deployedAt }: LiveEndpointProps) {
  const [revealKey, setRevealKey] = useState(false);
  const [showUsage, setShowUsage] = useState(false);
  const [message, setMessage] = useState("");
  const [sending, setSending] = useState(false);
  const [test, setTest] = useState<TestResponse | null>(null);

  const endpoint = result.endpoint_url || (result.domain ? `https://${result.domain}` : "");
  const apiKey = result.api_key || "";
  const masked = apiKey ? `sk-${'•'.repeat(Math.min(12, apiKey.length - 3))}` : "";
  const apiPath = result.api_path || "/v1/chat/completions";

  // "Deployed: X ago" from the result timestamp (falls back to "just now").
  const [nowTick, setNowTick] = useState(Date.now());
  useEffect(() => {
    const t = setInterval(() => setNowTick(Date.now()), 30_000);
    return () => clearInterval(t);
  }, []);
  const deployedLabel = (() => {
    const ts = deployedAt ? new Date(deployedAt).getTime() : Date.now();
    const s = Math.max(0, Math.floor((nowTick - ts) / 1000));
    if (s < 10) return "just now";
    if (s < 60) return `${s}s ago`;
    const m = Math.floor(s / 60);
    if (m < 60) return `${m}m ago`;
    const h = Math.floor(m / 60);
    if (h < 24) return `${h}h ago`;
    return new Date(ts).toLocaleDateString();
  })();

  const sendTest = async () => {
    if (!endpoint || !apiKey || !message.trim() || sending) return;
    setSending(true);
    setTest(null);
    const started = performance.now();
    try {
      const res = await fetch(`${endpoint}${apiPath}`, {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          Authorization: `Bearer ${apiKey}`,
        },
        body: JSON.stringify({
          messages: [{ role: "user", content: message }],
          max_tokens: 256,
        }),
      });
      if (!res.ok) {
        setTest({
          content: "",
          latencyMs: performance.now() - started,
          error: `Server responded with HTTP ${res.status}. The endpoint may still be warming up.`,
        });
        return;
      }
      const data = await res.json();
      const content =
        data?.choices?.[0]?.message?.content ||
        data?.choices?.[0]?.text ||
        (typeof data === "string" ? data : JSON.stringify(data));
      setTest({ content: String(content), latencyMs: performance.now() - started });
    } catch {
      setTest({
        content: "",
        latencyMs: performance.now() - started,
        error:
          "Could not reach the endpoint from this browser (likely CORS on the demo machine). " +
          "Use the curl example below — it works from any machine that can reach the server.",
      });
    } finally {
      setSending(false);
    }
  };

  const curlExample = `curl ${endpoint}${apiPath} \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer ${apiKey}" \\
  -d '{"messages":[{"role":"user","content":"Say hello in one word."}],"max_tokens":32}'`;

  const pythonExample = `import requests

resp = requests.post(
    "${endpoint}${apiPath}",
    headers={"Authorization": "Bearer ${apiKey}"},
    json={"messages": [{"role": "user", "content": "Say hello in one word."}]},
)
print(resp.json()["choices"][0]["message"]["content"])`;

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

        {/* Endpoint URL */}
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

        {/* API key */}
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
                className="ml-2 inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-[var(--radius-sm)] text-[var(--color-muted)] transition-colors hover:bg-[var(--color-paper-2)] hover:text-[var(--color-ink)]"
              >
                {revealKey ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
              </button>
              <CopyButton text={apiKey} label="Copy API key" />
            </div>
            <p className="mt-2 text-xs text-[var(--color-muted)]">
              Send it in the <code className="font-mono">Authorization: Bearer</code> header.
              Save this key now — you can always find it in your environment variables.
            </p>
          </div>
        )}

        {/* Deployment details */}
        <div className="grid gap-2 text-sm sm:grid-cols-3">
          <div className="rounded-[var(--radius-md)] bg-[var(--color-paper-2)] px-3 py-2.5">
            <p className="text-xs text-[var(--color-muted)]">Model deployed</p>
            <p className="mt-0.5 font-semibold text-[var(--color-ink)]">
              {modelLabel || result.model_file?.replace(/\.gguf$/, "") || result.quantization || "—"}
            </p>
          </div>
          <div className="rounded-[var(--radius-md)] bg-[var(--color-paper-2)] px-3 py-2.5">
            <p className="text-xs text-[var(--color-muted)]">Arm optimization applied</p>
            <p className="mt-0.5 font-semibold text-[var(--color-ink)]">
              {result.optimization || "Full (I8MM + SVE)"}
            </p>
          </div>
          <div className="rounded-[var(--radius-md)] bg-[var(--color-paper-2)] px-3 py-2.5">
            <p className="text-xs text-[var(--color-muted)]">Deployed</p>
            <p className="mt-0.5 font-semibold text-[var(--color-ink)]">{deployedLabel}</p>
          </div>
        </div>

        {/* Test interface */}
        <div className="space-y-2.5">
          <p className="text-sm font-semibold text-[var(--color-ink)]">Send a test message</p>
          <div className="flex flex-col gap-2 sm:flex-row">
            <textarea
              id="infer-test-message"
              name="infer-test-message"
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !e.shiftKey) {
                  e.preventDefault();
                  sendTest();
                }
              }}
              placeholder="Ask the model anything..."
              rows={2}
              className="w-full resize-none rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-surface)] px-3.5 py-2.5 text-sm text-[var(--color-ink)] shadow-sm transition-colors placeholder:text-[var(--color-muted)] focus:border-[var(--color-accent)] focus:outline-none focus:ring-2 focus:ring-[var(--color-accent-soft)]"
            />
            <Button
              onClick={sendTest}
              disabled={!endpoint || !apiKey || !message.trim() || sending}
              className="h-auto shrink-0 gap-1.5 sm:self-stretch"
            >
              {sending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
              {sending ? "Sending…" : "Send"}
            </Button>
          </div>

          {sending && (
            <div className="flex items-center gap-2 rounded-[var(--radius-md)] bg-[var(--color-paper-2)] px-3.5 py-3 text-sm text-[var(--color-muted)]">
              <span className="flex gap-1">
                <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-[var(--color-accent)]" />
                <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-[var(--color-accent)] [animation-delay:120ms]" />
                <span className="h-1.5 w-1.5 animate-bounce rounded-full bg-[var(--color-accent)] [animation-delay:240ms]" />
              </span>
              Model is thinking…
            </div>
          )}

          {test && !sending && (
            <div
              className={`rounded-[var(--radius-md)] border px-3.5 py-3 text-sm ${
                test.error
                  ? "border-[var(--color-warning)]/30 bg-[var(--color-warning-soft)]"
                  : "border-[var(--color-border)] bg-[var(--color-paper-2)]"
              }`}
            >
              {test.error ? (
                <>
                  <p className="font-semibold text-[var(--color-warning)]">Test message failed</p>
                  <p className="mt-1 text-[var(--color-muted)]">{test.error}</p>
                </>
              ) : (
                <>
                  <p className="whitespace-pre-wrap leading-relaxed text-[var(--color-ink)]">
                    {test.content}
                  </p>
                  <p className="mt-2 text-xs font-medium text-[var(--color-success)]">
                    Response received in {Math.round(test.latencyMs)}ms
                  </p>
                </>
              )}
            </div>
          )}
        </div>

        {/* Usage examples (collapsed by default) */}
        <div className="overflow-hidden rounded-[var(--radius-md)] border border-[var(--color-border)]">
          <button
            type="button"
            onClick={() => setShowUsage((v) => !v)}
            className="flex w-full items-center justify-between bg-[var(--color-paper-2)] px-4 py-2.5 text-sm font-semibold text-[var(--color-ink)] transition-colors hover:bg-[var(--color-paper-2)]/70"
          >
            <span>Usage example</span>
            <ChevronDown
              className={`h-4 w-4 text-[var(--color-muted)] transition-transform duration-[var(--dur-med)] ${
                showUsage ? "rotate-180" : ""
              }`}
            />
          </button>
          {showUsage && (
            <div className="space-y-3 bg-[#0b1020] px-4 py-3">
              <div>
                <p className="mb-1 font-mono text-[11px] uppercase tracking-wider text-white/40">
                  cURL
                </p>
                <pre className="overflow-x-auto whitespace-pre-wrap break-all font-mono text-xs leading-relaxed text-[#c9d4e3]">
                  {curlExample}
                </pre>
              </div>
              <div>
                <p className="mb-1 font-mono text-[11px] uppercase tracking-wider text-white/40">
                  Python
                </p>
                <pre className="overflow-x-auto whitespace-pre-wrap break-all font-mono text-xs leading-relaxed text-[#c9d4e3]">
                  {pythonExample}
                </pre>
              </div>
            </div>
          )}
        </div>
      </CardContent>
    </Card>
  );
}
