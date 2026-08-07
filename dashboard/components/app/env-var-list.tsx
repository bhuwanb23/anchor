"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import api from "@/lib/api";
import { toast } from "sonner";
import { Pencil, Plus, Trash2 } from "lucide-react";

export interface EnvKey {
  id?: string;
  key_name: string;
  is_auto?: boolean;
}

interface EnvVarListProps {
  serverId: string;
  appId: string;
  keys: EnvKey[];
  onChange: () => void;
  onRestart?: () => void;
}

export function EnvVarList({ serverId, appId, keys, onChange, onRestart }: EnvVarListProps) {
  const [editKey, setEditKey] = useState<string | null>(null);
  const [editValue, setEditValue] = useState("");
  const [adding, setAdding] = useState(false);
  const [newKey, setNewKey] = useState("");
  const [newValue, setNewValue] = useState("");
  const [loading, setLoading] = useState(false);
  const [needsRestart, setNeedsRestart] = useState(false);

  const save = async (key: string, value: string, close: () => void) => {
    setLoading(true);
    try {
      await api.put(`/api/v1/servers/${serverId}/apps/${appId}/env/${encodeURIComponent(key)}`, {
        value,
        restart_after: false,
      });
      toast.success("Variable updated. Restart your app to apply.");
      setNeedsRestart(true);
      close();
      onChange();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to update");
    } finally {
      setLoading(false);
    }
  };

  const remove = async (key: string) => {
    try {
      await api.delete(`/api/v1/servers/${serverId}/apps/${appId}/env/${encodeURIComponent(key)}`);
      toast.success(`Removed ${key}`);
      setNeedsRestart(true);
      onChange();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Failed to delete");
    }
  };

  return (
    <div className="space-y-3">
      <div className="divide-y rounded-lg border border-gray-200 dark:divide-gray-800 dark:border-gray-800">
        {keys.length === 0 && (
          <p className="px-4 py-3 text-sm text-gray-500">No environment variables yet.</p>
        )}
        {keys.map((ev) => (
          <div key={ev.key_name} className="flex items-center gap-3 px-4 py-2.5">
            <code className="flex-1 text-sm font-medium text-[var(--color-ink)]">
              {ev.key_name}
              {ev.is_auto && (
                <span className="ml-2 text-xs font-normal text-gray-400">(auto)</span>
              )}
            </code>
            <span className="font-mono text-sm text-gray-400">••••••</span>
            {!ev.is_auto && (
              <>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    setEditKey(ev.key_name);
                    setEditValue("");
                  }}
                >
                  <Pencil className="h-3.5 w-3.5" />
                </Button>
                <Button size="sm" variant="ghost" onClick={() => remove(ev.key_name)}>
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </>
            )}
          </div>
        ))}
      </div>

      <Button size="sm" variant="secondary" onClick={() => setAdding(true)}>
        <Plus className="mr-1 h-3.5 w-3.5" />
        Add Variable
      </Button>

      {needsRestart && (
        <div className="flex flex-wrap items-center gap-3 rounded-lg bg-amber-50 px-4 py-3 text-sm dark:bg-amber-950/40">
          <span className="text-amber-900 dark:text-amber-200">
            Restart your app to apply changes
          </span>
          <Button
            size="sm"
            onClick={() => {
              onRestart?.();
              setNeedsRestart(false);
            }}
          >
            Restart Now
          </Button>
        </div>
      )}

      <Dialog open={!!editKey} onOpenChange={(o) => !o && setEditKey(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit {editKey}</DialogTitle>
          </DialogHeader>
          <Input
            label="New value"
            type="password"
            value={editValue}
            onChange={(e) => setEditValue(e.target.value)}
            autoFocus
          />
          <DialogFooter>
            <Button variant="secondary" onClick={() => setEditKey(null)}>
              Cancel
            </Button>
            <Button
              disabled={loading || !editValue}
              onClick={() => editKey && save(editKey, editValue, () => setEditKey(null))}
            >
              Save
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={adding} onOpenChange={setAdding}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add Variable</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <Input label="Key" value={newKey} onChange={(e) => setNewKey(e.target.value)} />
            <Input
              label="Value"
              type="password"
              value={newValue}
              onChange={(e) => setNewValue(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button variant="secondary" onClick={() => setAdding(false)}>
              Cancel
            </Button>
            <Button
              disabled={loading || !newKey.trim()}
              onClick={() =>
                save(newKey.trim(), newValue, () => {
                  setAdding(false);
                  setNewKey("");
                  setNewValue("");
                })
              }
            >
              Add
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
