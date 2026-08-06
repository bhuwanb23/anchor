import { cn } from "@/lib/utils";

interface ConnectionStatusProps {
  connected: boolean;
  lastSeen?: string;
}

export function ConnectionStatus({ connected, lastSeen }: ConnectionStatusProps) {
  return (
    <div className="flex items-center gap-2 text-sm">
      <span
        className={cn(
          "h-2 w-2 rounded-full",
          connected ? "bg-green-500" : "bg-gray-400"
        )}
      />
      <span className={connected ? "text-green-700 dark:text-green-400" : "text-gray-500"}>
        {connected ? "Connected" : "Disconnected"}
      </span>
      {lastSeen && !connected && (
        <span className="text-xs text-gray-400">Last seen {new Date(lastSeen).toLocaleString()}</span>
      )}
    </div>
  );
}
