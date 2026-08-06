"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import api from "@/lib/api";
import { toast } from "sonner";
import { Rocket } from "lucide-react";

interface DeployButtonProps {
  serverId: string;
  appId: string;
  projectName: string;
}

export function DeployButton({ serverId, appId, projectName }: DeployButtonProps) {
  const [open, setOpen] = useState(false);
  const [image, setImage] = useState("");
  const [port, setPort] = useState(80);
  const [loading, setLoading] = useState(false);

  const handleDeploy = async (e: React.FormEvent) => {
    e.preventDefault();
    setLoading(true);
    try {
      await api.post(`/api/v1/servers/${serverId}/apps/${appId}/deploy`, { image, port });
      toast.success("Deploy started");
      setOpen(false);
    } catch (err: any) {
      toast.error(err.response?.data?.message || "Deploy failed");
    } finally {
      setLoading(false);
    }
  };

  return (
    <>
      <Button size="sm" onClick={() => setOpen(true)}>
        <Rocket className="mr-1 h-3 w-3" />
        Deploy
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Deploy {projectName}</DialogTitle>
          </DialogHeader>
          <form onSubmit={handleDeploy} className="space-y-4">
            <Input label="Docker image" value={image} onChange={(e) => setImage(e.target.value)} placeholder="nginx:latest" required />
            <Input label="Port" type="number" value={port} onChange={(e) => setPort(Number(e.target.value))} required />
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setOpen(false)}>Cancel</Button>
              <Button type="submit" disabled={loading}>{loading ? "Deploying..." : "Deploy"}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}
