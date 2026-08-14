"use client";

import { useEffect, useState, Suspense } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { toast } from "sonner";
import { useServers } from "@/hooks/use-server";
import { ServerCard } from "@/components/dashboard/server-card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent } from "@/components/ui/card";
import { PageError } from "@/components/ui/page-states";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogTrigger,
} from "@/components/ui/dialog";
import api from "@/lib/api";
import { Plus, Copy, Server, Terminal, CheckCircle2 } from "lucide-react";
import type { RegistrationTokenResponse } from "@/types";

const createServerSchema = z.object({
  name: z.string().min(1, "Server name is required"),
});

type CreateServerForm = z.infer<typeof createServerSchema>;

function ServersPageInner() {
  const { servers, isLoading, error, refetch } = useServers(10_000);
  const searchParams = useSearchParams();
  const router = useRouter();
  const [isCreating, setIsCreating] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [newServer, setNewServer] = useState<RegistrationTokenResponse | null>(null);

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<CreateServerForm>({
    resolver: zodResolver(createServerSchema),
    defaultValues: { name: "my-server" },
  });

  // Deep-link: /servers?connect=1 opens the connect dialog (replaces onboarding wizard)
  useEffect(() => {
    if (searchParams.get("connect") === "1") {
      queueMicrotask(() => setDialogOpen(true));
    }
  }, [searchParams]);

  async function onSubmit(data: CreateServerForm) {
    setIsCreating(true);
    try {
      const res = await api.post<RegistrationTokenResponse>(
        "/api/v1/servers/registration-token",
        data
      );
      setNewServer(res.data);
      toast.success("Install command ready — run it on your VPS");
      refetch();
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to create server");
    } finally {
      setIsCreating(false);
    }
  }

  function copyInstallCommand() {
    if (newServer?.install_command) {
      navigator.clipboard.writeText(newServer.install_command);
      toast.success("Copied to clipboard");
    }
  }

  function closeDialog() {
    setDialogOpen(false);
    setNewServer(null);
    reset({ name: "my-server" });
    if (searchParams.get("connect") === "1") {
      router.replace("/servers");
    }
  }

  return (
    <div className="mx-auto max-w-6xl space-y-8">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <h2 className="text-xl font-bold tracking-tight text-[var(--color-ink)] sm:text-2xl">
            Servers
          </h2>
          <p className="mt-1 max-w-xl text-sm text-[var(--color-muted)]">
            Every machine runs a small Anchor agent. Connect one Linux VPS to deploy apps or Infer.
          </p>
        </div>
        <Dialog
          open={dialogOpen}
          onOpenChange={(open) => {
            if (!open) closeDialog();
            else setDialogOpen(true);
          }}
        >
          <DialogTrigger asChild>
            <Button className="gap-1.5">
              <Plus className="h-4 w-4" />
              Add server
            </Button>
          </DialogTrigger>
          <DialogContent className="sm:max-w-lg">
            <DialogHeader>
              <DialogTitle>Connect a Linux server</DialogTitle>
              <DialogDescription>
                Name it, copy one install command, run it over SSH. The agent appears here when live.
              </DialogDescription>
            </DialogHeader>
            {newServer ? (
              <div className="mt-2 space-y-5">
                <div className="flex items-start gap-3 rounded-[var(--radius-md)] border border-[var(--color-accent)]/25 bg-[var(--color-accent-soft)]/40 p-3">
                  <CheckCircle2 className="mt-0.5 h-5 w-5 shrink-0 text-[var(--color-accent)]" />
                  <div className="text-sm text-[var(--color-ink)]">
                    <p className="font-semibold">Run this on the VPS</p>
                    <p className="mt-0.5 text-[var(--color-muted)]">
                      Needs Docker and curl. Arm64 recommended for Infer demos.
                    </p>
                  </div>
                </div>
                <div className="space-y-2">
                  <div className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-[var(--color-muted)]">
                    <Terminal className="h-3.5 w-3.5" />
                    Install command
                  </div>
                  <div className="flex items-start gap-2 rounded-[var(--radius-md)] border border-[var(--color-border)] bg-[var(--color-paper-2)] p-3 font-mono text-xs leading-relaxed text-[var(--color-ink)]">
                    <code className="flex-1 break-all">{newServer.install_command}</code>
                    <Button
                      type="button"
                      variant="secondary"
                      size="sm"
                      className="shrink-0"
                      onClick={copyInstallCommand}
                    >
                      <Copy className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
                <Button className="w-full" onClick={closeDialog}>
                  Done — waiting for agent
                </Button>
              </div>
            ) : (
              <form onSubmit={handleSubmit(onSubmit)} className="mt-2 space-y-4">
                <Input
                  label="Server name"
                  placeholder="arm-demo"
                  error={errors.name?.message}
                  {...register("name")}
                />
                <Button type="submit" className="w-full" disabled={isCreating}>
                  {isCreating ? "Generating…" : "Generate install command"}
                </Button>
              </form>
            )}
          </DialogContent>
        </Dialog>
      </div>

      {error && (
        <PageError
          message="We could not load your servers. Try again in a moment."
          onRetry={() => refetch()}
        />
      )}

      {isLoading ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {[1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-36 rounded-[var(--radius-lg)]" />
          ))}
        </div>
      ) : servers.length === 0 ? (
        <Card className="overflow-hidden border-[var(--color-border)]">
          <CardContent className="grid gap-0 p-0 md:grid-cols-2">
            <div className="space-y-5 p-8 sm:p-10">
              <div className="inline-flex h-12 w-12 items-center justify-center rounded-[var(--radius-md)] bg-[var(--color-accent-soft)] text-[var(--color-accent)]">
                <Server className="h-6 w-6" />
              </div>
              <div>
                <p className="text-xl font-bold tracking-tight text-[var(--color-ink)]">
                  No servers yet
                </p>
                <p className="mt-2 text-sm leading-relaxed text-[var(--color-muted)]">
                  This space lists every VPS with an Anchor agent. Connect one to deploy apps,
                  stream logs, or run Infer on Arm64.
                </p>
              </div>
              <Button onClick={() => setDialogOpen(true)} className="gap-2">
                <Plus className="h-4 w-4" />
                Connect your first server
              </Button>
            </div>
            <div className="border-t border-[var(--color-border)] bg-[var(--color-paper-2)] p-8 sm:p-10 md:border-l md:border-t-0">
              <p className="text-xs font-semibold uppercase tracking-wider text-[var(--color-muted)]">
                What you&apos;ll do
              </p>
              <ol className="mt-4 space-y-4 text-sm text-[var(--color-ink)]">
                <li className="flex gap-3">
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-[var(--color-surface)] text-xs font-bold text-[var(--color-accent)] shadow-sm">
                    1
                  </span>
                  <span>Generate a one-line install command</span>
                </li>
                <li className="flex gap-3">
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-[var(--color-surface)] text-xs font-bold text-[var(--color-accent)] shadow-sm">
                    2
                  </span>
                  <span>SSH into your Linux box and paste it</span>
                </li>
                <li className="flex gap-3">
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-[var(--color-surface)] text-xs font-bold text-[var(--color-accent)] shadow-sm">
                    3
                  </span>
                  <span>Watch the server flip to connected here</span>
                </li>
              </ol>
            </div>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {servers.map((server) => (
            <ServerCard key={server.id} server={server} />
          ))}
        </div>
      )}
    </div>
  );
}

export default function ServersPage() {
  return (
    <Suspense
      fallback={
        <div className="mx-auto max-w-6xl space-y-6">
          <Skeleton className="h-10 w-48" />
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {[1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-36 rounded-[var(--radius-lg)]" />
            ))}
          </div>
        </div>
      }
    >
      <ServersPageInner />
    </Suspense>
  );
}
