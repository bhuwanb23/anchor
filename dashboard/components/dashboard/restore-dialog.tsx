"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import type { BackupJob } from "@/types";

interface RestoreDialogProps {
  job: BackupJob;
  isOpen: boolean;
  onClose: () => void;
  onRestore: (snapshotId: string, projectName: string) => Promise<string | null>;
}

export function RestoreDialog({ job, isOpen, onClose, onRestore }: RestoreDialogProps) {
  const [confirmText, setConfirmText] = useState("");
  const [selectedProject, setSelectedProject] = useState("");
  const [isRestoring, setIsRestoring] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Parse project results from the backup job
  let projects: string[] = [];
  if (job.project_results) {
    try {
      const parsed = JSON.parse(job.project_results);
      projects = parsed.map((p: { name: string }) => p.name);
    } catch {
      // If parsing fails, we can't determine projects
    }
  }

  const handleRestore = async () => {
    if (confirmText !== "restore") return;
    if (!selectedProject || !job.snapshot_id) return;

    setIsRestoring(true);
    setError(null);

    try {
      const jobId = await onRestore(job.snapshot_id, selectedProject);
      if (jobId) {
        onClose();
        setConfirmText("");
        setSelectedProject("");
      } else {
        setError("Failed to trigger restore");
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to trigger restore");
    } finally {
      setIsRestoring(false);
    }
  };

  if (!isOpen) return null;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/50"
        onClick={onClose}
      />

      {/* Dialog */}
      <div className="relative bg-white rounded-lg shadow-xl max-w-md w-full mx-4 p-6">
        <h2 className="text-lg font-semibold text-gray-900 mb-4">
          Restore from Backup
        </h2>

        {/* Backup info */}
        <div className="mb-4 p-3 bg-gray-50 rounded-md">
          <div className="text-sm text-gray-600">
            <p><span className="font-medium">Snapshot:</span> {job.snapshot_id?.slice(0, 12)}...</p>
            <p><span className="font-medium">Date:</span> {new Date(job.created_at).toLocaleString()}</p>
            {job.size_new_bytes && (
              <p><span className="font-medium">Size:</span> {(job.size_new_bytes / 1024 / 1024).toFixed(1)} MB</p>
            )}
          </div>
        </div>

        {/* Warning */}
        <div className="mb-4 p-3 bg-amber-50 border border-amber-200 rounded-md">
          <p className="text-sm text-amber-800">
            This will overwrite the current project data with the backup. This action cannot be undone.
          </p>
        </div>

        {/* Project selector */}
        {projects.length > 1 && (
          <div className="mb-4">
            <label className="block text-sm font-medium text-gray-700 mb-1">
              Select project to restore
            </label>
            <select
              value={selectedProject}
              onChange={(e) => setSelectedProject(e.target.value)}
              className="w-full border border-gray-300 rounded-md px-3 py-2 text-sm"
            >
              <option value="">Choose a project...</option>
              {projects.map((p) => (
                <option key={p} value={p}>{p}</option>
              ))}
            </select>
          </div>
        )}

        {/* Confirm input */}
        <div className="mb-4">
          <label className="block text-sm font-medium text-gray-700 mb-1">
            Type <span className="font-mono bg-gray-100 px-1">restore</span> to confirm
          </label>
          <Input
            type="text"
            value={confirmText}
            onChange={(e) => setConfirmText(e.target.value)}
            placeholder='Type "restore"'
            className="w-full"
          />
        </div>

        {/* Error */}
        {error && (
          <div className="mb-4 p-3 bg-red-50 border border-red-200 rounded-md">
            <p className="text-sm text-red-800">{error}</p>
          </div>
        )}

        {/* Actions */}
        <div className="flex justify-end gap-3">
          <Button
            variant="secondary"
            onClick={onClose}
            disabled={isRestoring}
          >
            Cancel
          </Button>
          <Button
            onClick={handleRestore}
            disabled={confirmText !== "restore" || !selectedProject || isRestoring}
            className="bg-red-600 hover:bg-red-700 text-white"
          >
            {isRestoring ? "Restoring..." : "Restore"}
          </Button>
        </div>
      </div>
    </div>
  );
}
