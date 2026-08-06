"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import api from "@/lib/api";
import { toast } from "sonner";
import { Trash2 } from "lucide-react";

interface EnvVar {
  key: string;
  value?: string;
}

interface EnvVarListProps {
  serverId: string;
  appId: string;
  envVars: EnvVar[];
  onChange: () => void;
}

export function EnvVarList({ serverId, appId, envVars, onChange }: EnvVarListProps) {
  const [newKey, setNewKey] = useState("");
  const [newValue, setNewValue] = useState("");
  const [loading, setLoading] = useState(false);

  const handleAdd = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newKey.trim()) return;
    setLoading(true);
    try {
      await api.put(`/api/v1/servers/${serverId}/apps/${appId}/env/${encodeURIComponent(newKey)}`, { value: newValue });
      toast.success(`Set ${newKey}`);
      setNewKey("");
      setNewValue("");
      onChange();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to set variable";
      toast.error(msg);
    } finally {
      setLoading(false);
    }
  };

  const handleDelete = async (key: string) => {
    try {
      await api.delete(`/api/v1/servers/${serverId}/apps/${appId}/env/${encodeURIComponent(key)}`);
      toast.success(`Deleted ${key}`);
      onChange();
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "Failed to delete";
      toast.error(msg);
    }
  };

  return (
    <div className="space-y-4">
      <div className="space-y-2">
        {envVars.map((ev) => (
          <div key={ev.key} className="flex items-center gap-2">
            <code className="flex-1 rounded bg-gray-100 px-2 py-1 text-sm dark:bg-gray-800">
              {ev.key}
            </code>
            <Button size="sm" variant="ghost" onClick={() => handleDelete(ev.key)}>
              <Trash2 className="h-3 w-3" />
            </Button>
          </div>
        ))}
      </div>
      <form onSubmit={handleAdd} className="flex gap-2">
        <Input placeholder="KEY" value={newKey} onChange={(e) => setNewKey(e.target.value)} className="flex-1" />
        <Input placeholder="value" value={newValue} onChange={(e) => setNewValue(e.target.value)} className="flex-1" />
        <Button type="submit" size="sm" disabled={loading || !newKey.trim()}>Set</Button>
      </form>
    </div>
  );
}
