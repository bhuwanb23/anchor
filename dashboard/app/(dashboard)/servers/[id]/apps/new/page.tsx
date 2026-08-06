"use client";

import { use, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ArrowLeft } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import api from "@/lib/api";
import { toast } from "sonner";

export default function DeployNewAppPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id: serverId } = use(params);
  const router = useRouter();
  const [projectName, setProjectName] = useState("");
  const [image, setImage] = useState("");
  const [port, setPort] = useState(3000);
  const [loading, setLoading] = useState(false);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    try {
      const create = await api.post<{ id: string }>(
        `/api/v1/servers/${serverId}/apps`,
        { project_name: projectName }
      );
      const appId = create.data.id;
      await api.post(`/api/v1/servers/${serverId}/apps/${appId}/deploy`, {
        image,
        port,
      });
      toast.success("Deploy started");
      router.push(`/servers/${serverId}`);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Deploy failed");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="mx-auto max-w-lg space-y-6">
      <Link
        href={`/servers/${serverId}`}
        className="inline-flex items-center gap-2 text-sm text-gray-500 hover:text-gray-700"
      >
        <ArrowLeft className="h-4 w-4" />
        Back to server
      </Link>

      <Card>
        <CardHeader>
          <CardTitle>Deploy New App</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={submit} className="space-y-4">
            <Input
              label="App name"
              value={projectName}
              onChange={(e) => setProjectName(e.target.value)}
              placeholder="myblog"
              required
            />
            <Input
              label="Docker image"
              value={image}
              onChange={(e) => setImage(e.target.value)}
              placeholder="nginx:latest"
              required
            />
            <Input
              label="Container port"
              type="number"
              value={port}
              onChange={(e) => setPort(Number(e.target.value))}
              required
            />
            <Button type="submit" disabled={loading} className="w-full">
              {loading ? "Deploying…" : "Deploy"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
