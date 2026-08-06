"use client";

import { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Button } from "@/components/ui/button";
import api from "@/lib/api";

const templates = [
  { name: "Nginx", image: "nginx:latest", port: 80 },
  { name: "Node.js", image: "node:18-alpine", port: 3000 },
  { name: "Python", image: "python:3.12-slim", port: 8000 },
  { name: "PostgreSQL", image: "postgres:16-alpine", port: 5432 },
];

export default function FirstDeployPage() {
  const router = useRouter();
  const [servers, setServers] = useState<{ id: string; name: string }[]>([]);
  const [selectedServer, setSelectedServer] = useState("");
  const [image, setImage] = useState("");
  const [port, setPort] = useState(80);
  const [appName, setAppName] = useState("");
  const [deploying, setDeploying] = useState(false);

  useEffect(() => {
    api.get("/api/v1/servers").then((res) => {
      const list = res.data.map((s: { id: string; name: string }) => ({ id: s.id, name: s.name }));
      setServers(list);
      if (list.length > 0) setSelectedServer(list[0].id);
    });
  }, []);

  const handleDeploy = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedServer || !image) return;
    setDeploying(true);
    try {
      const appRes = await api.post(`/api/v1/servers/${selectedServer}/apps`, { project_name: appName || "my-app" });
      await api.post(`/api/v1/servers/${selectedServer}/apps/${appRes.data.id}/deploy`, { image, port });
      router.push("/overview");
    } catch {
      setDeploying(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>Deploy your first app</CardTitle>
      </CardHeader>
      <CardContent>
        <form onSubmit={handleDeploy} className="space-y-4">
          {servers.length > 0 ? (
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              {templates.map((t) => (
                <button
                  key={t.name}
                  type="button"
                  className={`rounded-lg border p-3 text-left text-sm transition hover:border-blue-500 ${
                    image === t.image ? "border-blue-500 bg-blue-50 dark:bg-blue-950" : "border-gray-200 dark:border-gray-700"
                  }`}
                  onClick={() => { setImage(t.image); setPort(t.port); setAppName(t.name.toLowerCase()); }}
                >
                  <div className="font-medium">{t.name}</div>
                  <div className="text-xs text-gray-500">:{t.port}</div>
                </button>
              ))}
            </div>
          ) : (
            <p className="text-sm text-amber-600">No servers connected yet. Go back and connect a server first.</p>
          )}
          <Input label="App name" value={appName} onChange={(e) => setAppName(e.target.value)} placeholder="my-app" />
          <Input label="Docker image" value={image} onChange={(e) => setImage(e.target.value)} placeholder="nginx:latest" />
          <Input label="Port" type="number" value={port} onChange={(e) => setPort(Number(e.target.value))} />
          <div className="flex justify-end gap-3">
            <Button variant="ghost" type="button" onClick={() => router.push("/overview")}>
              Skip
            </Button>
            <Button type="submit" disabled={deploying || !image || !selectedServer}>
              {deploying ? "Deploying..." : "Deploy"}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}
