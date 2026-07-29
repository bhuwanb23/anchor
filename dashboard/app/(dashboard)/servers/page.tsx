"use client";

import { useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { toast } from "sonner";
import { useServers } from "@/hooks/use-server";
import { ServerCard } from "@/components/dashboard/server-card";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Dialog, DialogContent, DialogTrigger } from "@radix-ui/react-dialog";
import api from "@/lib/api";
import { Server, Plus, Copy } from "lucide-react";
import type { CreateServerResponse } from "@/types";

const createServerSchema = z.object({
  name: z.string().min(1, "Server name is required"),
});

type CreateServerForm = z.infer<typeof createServerSchema>;

export default function ServersPage() {
  const { servers, isLoading, error, refetch } = useServers();
  const [isCreating, setIsCreating] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [newServer, setNewServer] = useState<CreateServerResponse | null>(null);

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<CreateServerForm>({
    resolver: zodResolver(createServerSchema),
  });

  async function onSubmit(data: CreateServerForm) {
    setIsCreating(true);
    try {
      const res = await api.post<CreateServerResponse>("/api/v1/servers", data);
      setNewServer(res.data);
      toast.success("Server created");
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
      toast.success("Install command copied to clipboard");
    }
  }

  return (
    <div className="space-y-8">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Servers</h1>
          <p className="text-gray-500 dark:text-gray-400">Manage your connected servers.</p>
        </div>
        <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              Add Server
            </Button>
          </DialogTrigger>
          <DialogContent className="fixed left-1/2 top-1/2 z-50 w-full max-w-md -translate-x-1/2 -translate-y-1/2 rounded-xl border border-gray-200 bg-white p-6 shadow-lg dark:border-gray-700 dark:bg-gray-900">
            <h2 className="text-lg font-semibold">Add New Server</h2>
            {newServer ? (
              <div className="space-y-4">
                <p className="text-sm text-gray-600 dark:text-gray-400">
                  Run this command on your server to connect it:
                </p>
                <div className="flex items-center gap-2 rounded-lg bg-gray-100 p-3 font-mono text-xs dark:bg-gray-800">
                  <code className="flex-1 break-all">{newServer.install_command}</code>
                  <Button variant="ghost" size="sm" onClick={copyInstallCommand}>
                    <Copy className="h-4 w-4" />
                  </Button>
                </div>
                <Button className="w-full" onClick={() => { setNewServer(null); setDialogOpen(false); reset(); }}>
                  Done
                </Button>
              </div>
            ) : (
              <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
                <Input
                  label="Server Name"
                  placeholder="my-server"
                  error={errors.name?.message}
                  {...register("name")}
                />
                <Button type="submit" className="w-full" disabled={isCreating}>
                  {isCreating ? "Creating..." : "Create Server"}
                </Button>
              </form>
            )}
          </DialogContent>
        </Dialog>
      </div>

      {error && <p className="text-sm text-red-500">{error}</p>}
      {isLoading ? (
        <div className="flex items-center gap-2 text-gray-500">
          <Server className="h-4 w-4 animate-pulse" />
          Loading servers...
        </div>
      ) : servers.length === 0 ? (
        <Card>
          <CardContent className="py-12 text-center text-gray-500">
            No servers yet. Click &quot;Add Server&quot; to get started.
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
