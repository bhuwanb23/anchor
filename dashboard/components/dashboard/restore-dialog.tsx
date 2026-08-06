"use client";

import { useEffect, useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { getWSClient, type WSMessage } from "@/lib/ws";
import type { BackupJob, BackupProjectResult } from "@/types";

const STEPS = [
  "Downloading backup",
  "Restoring database",
  "Restoring files",
  "Restarting app",
] as const;

function formatBytes(bytes?: number): string {
  if (!bytes) return "—";
  const mb = bytes / (1024 * 1024);
  return mb >= 1 ? `${mb.toFixed(0)}MB` : `${(bytes / 1024).toFixed(0)}KB`;
}

function parseProjects(job: BackupJob): BackupProjectResult[] {
  if (!job.project_results) return [];
  try {
    return JSON.parse(job.project_results);
  } catch {
    return [];
  }
}

interface RestoreDialogProps {
  job: BackupJob;
  isOpen: boolean;
  onClose: () => void;
  onRestore: (snapshotId: string, projectName: string) => Promise<string | null>;
}

type Phase = "confirm" | "progress" | "success" | "failure";

export function RestoreDialog({ job, isOpen, onClose, onRestore }: RestoreDialogProps) {
  const projects = useMemo(() => parseProjects(job), [job]);
  const projectNames = projects.map((p) => p.name).filter(Boolean);
  const [confirmText, setConfirmText] = useState("");
  const [selectedProject, setSelectedProject] = useState(
    projectNames.length === 1 ? projectNames[0] : ""
  );
  const [phase, setPhase] = useState<Phase>("confirm");
  const [stepIdx, setStepIdx] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [commandId, setCommandId] = useState<string | null>(null);

  useEffect(() => {
    if (!isOpen) {
      setConfirmText("");
      setPhase("confirm");
      setStepIdx(0);
      setError(null);
      setCommandId(null);
      setSelectedProject(projectNames.length === 1 ? projectNames[0] : "");
    }
  }, [isOpen, projectNames]);

  useEffect(() => {
    if (phase !== "progress" || !commandId) return;
    const client = getWSClient();
    const unsubP = client.on("command_progress", (msg: WSMessage) => {
      const p = msg.payload as { command_id?: string; message?: string; status?: string };
      if (p?.command_id && p.command_id !== commandId) return;
      const m = (p?.message || "").toLowerCase();
      if (m.includes("download")) setStepIdx(0);
      else if (m.includes("database") || m.includes("postgres")) setStepIdx(1);
      else if (m.includes("file") || m.includes("volume")) setStepIdx(2);
      else if (m.includes("restart")) setStepIdx(3);
      else setStepIdx((s) => Math.min(s + 1, STEPS.length - 1));
    });
    const unsubR = client.on("command_result", (msg: WSMessage) => {
      const p = msg.payload as { command_id?: string; status?: string; error?: string };
      if (p?.command_id && p.command_id !== commandId) return;
      if (p?.status === "success") {
        setStepIdx(STEPS.length - 1);
        setPhase("success");
      } else {
        setError(
          p?.error ||
            "Restore failed partway through. Your previous data may still be intact — check the app and try again, or contact support."
        );
        setPhase("failure");
      }
    });
    return () => {
      unsubP();
      unsubR();
    };
  }, [phase, commandId]);

  const handleRestore = async () => {
    if (confirmText !== "RESTORE" || !selectedProject || !job.snapshot_id) return;
    setPhase("progress");
    setStepIdx(0);
    setError(null);
    const jobId = await onRestore(job.snapshot_id, selectedProject);
    if (!jobId) {
      setError(
        "We could not start the restore. Check that the server is connected and try again."
      );
      setPhase("failure");
      return;
    }
    setCommandId(jobId);
  };

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="absolute inset-0 bg-black/50" onClick={phase === "progress" ? undefined : onClose} />
      <div className="relative w-full max-w-md rounded-lg bg-white p-6 shadow-xl dark:bg-gray-900">
        {phase === "confirm" && (
          <>
            <h2 className="mb-4 text-lg font-semibold text-gray-900 dark:text-white">
              Restore from Backup
            </h2>
            <div className="mb-4 rounded-md bg-gray-50 p-3 text-sm dark:bg-gray-800">
              <p>
                <span className="font-medium">When:</span>{" "}
                {new Date(job.started_at || job.created_at).toLocaleString()}
              </p>
              <p>
                <span className="font-medium">Size:</span> {formatBytes(job.size_new_bytes || job.size_total_bytes)}
              </p>
              <p className="mt-1">
                <span className="font-medium">Projects:</span>{" "}
                {projectNames.length ? projectNames.join(", ") : "—"}
              </p>
            </div>
            <div className="mb-4 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-100">
              This will replace your current database and files with the versions from this backup.
              Data created after this backup will be lost. This cannot be undone.
            </div>
            {projectNames.length > 1 && (
              <div className="mb-4">
                <label className="mb-1 block text-sm font-medium">Project to restore</label>
                <select
                  value={selectedProject}
                  onChange={(e) => setSelectedProject(e.target.value)}
                  className="w-full rounded-md border px-3 py-2 text-sm dark:border-gray-700 dark:bg-gray-950"
                >
                  <option value="">Choose a project…</option>
                  {projectNames.map((p) => (
                    <option key={p} value={p}>
                      {p}
                    </option>
                  ))}
                </select>
              </div>
            )}
            <div className="mb-4">
              <label className="mb-1 block text-sm font-medium">
                Type <span className="font-mono">RESTORE</span> to confirm
              </label>
              <Input
                value={confirmText}
                onChange={(e) => setConfirmText(e.target.value)}
                placeholder="RESTORE"
              />
            </div>
            <div className="flex justify-end gap-3">
              <Button variant="secondary" onClick={onClose}>
                Cancel
              </Button>
              <Button
                variant="danger"
                disabled={confirmText !== "RESTORE" || !selectedProject}
                onClick={handleRestore}
              >
                Restore
              </Button>
            </div>
          </>
        )}

        {phase === "progress" && (
          <>
            <h2 className="mb-2 text-lg font-semibold">Restoring…</h2>
            <div className="mb-4 rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800 dark:border-red-900 dark:bg-red-950/40 dark:text-red-200">
              Do not close this window during restore
            </div>
            <ol className="space-y-2">
              {STEPS.map((label, i) => (
                <li
                  key={label}
                  className={`flex items-center gap-2 text-sm ${
                    i < stepIdx
                      ? "text-green-600"
                      : i === stepIdx
                      ? "font-medium text-blue-600"
                      : "text-gray-400"
                  }`}
                >
                  <span className="w-4">{i < stepIdx ? "✓" : i === stepIdx ? "●" : "○"}</span>
                  {label}
                </li>
              ))}
            </ol>
            <p className="mt-4 text-xs text-gray-500">
              If you close the browser, restore continues on the server.
            </p>
          </>
        )}

        {phase === "success" && (
          <>
            <h2 className="mb-2 text-lg font-semibold text-green-700 dark:text-green-400">
              Restore complete
            </h2>
            <p className="mb-4 text-sm text-gray-600 dark:text-gray-300">
              Your app has been restarted.
            </p>
            <Button onClick={onClose}>Done</Button>
          </>
        )}

        {phase === "failure" && (
          <>
            <h2 className="mb-2 text-lg font-semibold text-red-700 dark:text-red-400">
              Restore failed
            </h2>
            <p className="mb-4 text-sm text-gray-700 dark:text-gray-300">
              {error || "Something went wrong during restore."}
            </p>
            <div className="flex gap-3">
              <Button variant="secondary" onClick={onClose}>
                Close
              </Button>
              <Button
                onClick={() => {
                  setPhase("confirm");
                  setError(null);
                }}
              >
                Try again
              </Button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
