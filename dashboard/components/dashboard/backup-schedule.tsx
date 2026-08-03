"use client";

import { useState } from "react";
import { Button } from "@/components/ui/button";
import type { BackupSchedule } from "@/types";

interface BackupScheduleProps {
  schedule: BackupSchedule | null;
  onUpdate: (hourUtc: number) => Promise<boolean>;
  isLoading?: boolean;
}

const hours = Array.from({ length: 24 }, (_, i) => i);

export function BackupScheduleComponent({
  schedule,
  onUpdate,
  isLoading,
}: BackupScheduleProps) {
  const [selectedHour, setSelectedHour] = useState<number>(
    schedule?.hour_utc ?? 2
  );
  const [isSaving, setIsSaving] = useState(false);

  const handleSave = async () => {
    setIsSaving(true);
    const success = await onUpdate(selectedHour);
    setIsSaving(false);
  };

  if (isLoading) {
    return (
      <div className="text-sm text-muted-foreground">Loading schedule...</div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-4">
        <div className="text-sm">
          Daily at{" "}
          <span className="font-medium">
            {String(selectedHour).padStart(2, "0")}:00 UTC
          </span>
        </div>
        <select
          value={selectedHour}
          onChange={(e) => setSelectedHour(Number(e.target.value))}
          className="border rounded px-2 py-1 text-sm"
          disabled={isSaving}
        >
          {hours.map((h) => (
            <option key={h} value={h}>
              {String(h).padStart(2, "0")}:00
            </option>
          ))}
        </select>
        <Button
          variant="secondary"
          size="sm"
          onClick={handleSave}
          disabled={isSaving || selectedHour === (schedule?.hour_utc ?? 2)}
        >
          {isSaving ? "Saving..." : "Save"}
        </Button>
      </div>
      {schedule?.last_backup_at && (
        <div className="text-xs text-muted-foreground">
          Last backup: {new Date(schedule.last_backup_at).toLocaleString()}
        </div>
      )}
    </div>
  );
}
